package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type QAItem struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

type EvaluatePayload struct {
	RepoFullName string   `json:"repo_full_name"`
	QAList       []QAItem `json:"qa_list"`
}

func CallPythonEvaluationWorker(payload EvaluatePayload) (string, float64, error) {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", 0, err
	}
	req, err := http.NewRequest("POST", workerURL()+"/internal/evaluate-answer", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Secret", internalSecret())

	resp, err := workerHTTPClient().Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", 0, fmt.Errorf("python worker error: %s", string(body))
	}

	var result struct {
		OverallScore float64 `json:"overall_score"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", 0, fmt.Errorf("failed to parse AI evaluation JSON: %w", err)
	}

	return string(body), result.OverallScore, nil
}
