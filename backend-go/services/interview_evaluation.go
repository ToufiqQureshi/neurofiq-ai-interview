package services

import (
	"encoding/json"
	"fmt"
)

type QAItem struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

type EvaluatePayload struct {
	RepoFullName string   `json:"repo_full_name"`
	QAList       []QAItem `json:"qa_list"`
}

// CallPythonEvaluationWorker scores a completed interview. It returns the raw
// feedback JSON (stored verbatim so the report page can render whatever shape
// the worker produced) alongside the overall score, which we pull out
// separately because the reports list sorts and averages on it.
func CallPythonEvaluationWorker(payload EvaluatePayload) (string, float64, error) {
	body, err := postToWorker(workerClient, "/internal/evaluate-answer", payload)
	if err != nil {
		return "", 0, err
	}

	var result struct {
		OverallScore float64 `json:"overall_score"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", 0, fmt.Errorf("failed to parse AI evaluation JSON: %w", err)
	}
	return string(body), result.OverallScore, nil
}
