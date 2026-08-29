package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
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
	workerURL := os.Getenv("PYTHON_WORKER_URL")
	if workerURL == "" {
		workerURL = "http://localhost:8001"
	}

	jsonData, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", workerURL+"/internal/evaluate-answer", bytes.NewBuffer(jsonData))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Secret", os.Getenv("INTERNAL_SECRET"))

	resp, err := http.DefaultClient.Do(req)
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
	// We extract the score to save it in DB directly, but we also save the full raw JSON.
	if err := json.Unmarshal(body, &result); err != nil {
		return "", 0, fmt.Errorf("failed to parse AI evaluation JSON: %w", err)
	}

	return string(body), result.OverallScore, nil
}
