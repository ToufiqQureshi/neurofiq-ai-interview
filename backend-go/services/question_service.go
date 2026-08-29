package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/config"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/models"
)

type GenerateQuestionsPayload struct {
	RepoFullName string `json:"repo_full_name"`
	AnalysisData string `json:"analysis_data"`
}

func GetOrGenerateQuestions(userID string, repoFullName string) ([]models.Question, error) {
	if !ValidRepoFullName(repoFullName) {
		return nil, fmt.Errorf("invalid repository name")
	}

	var profile models.GithubProfile
	if err := config.DB.Where("user_id = ? AND repo_full_name = ?", userID, repoFullName).First(&profile).Error; err != nil {
		return nil, fmt.Errorf("repo analysis not found for user")
	}
	if profile.StrategyUsed == "pending" {
		return nil, fmt.Errorf("analysis is still running")
	}
	if profile.StrategyUsed == "failed" || profile.AnalysisJSON == "" || profile.AnalysisJSON == "null" {
		return nil, fmt.Errorf("analysis is not ready — retry analyzing this repository")
	}

	payload := GenerateQuestionsPayload{
		RepoFullName: repoFullName,
		AnalysisData: profile.AnalysisJSON,
	}

	newQuestions, err := callPythonQuestionGenerator(payload)
	if err != nil {
		return nil, err
	}

	for i := range newQuestions {
		newQuestions[i].Reusable = true
		newQuestions[i].Language = repoFullName
		config.DB.Create(&newQuestions[i])
	}

	return newQuestions, nil
}

func callPythonQuestionGenerator(payload GenerateQuestionsPayload) ([]models.Question, error) {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest("POST", workerURL()+"/internal/generate-questions", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Secret", internalSecret())

	resp, err := workerHTTPClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("python worker error: %s", string(body))
	}

	var newQuestions []models.Question
	if err := json.Unmarshal(body, &newQuestions); err != nil {
		return nil, fmt.Errorf("failed to parse AI questions JSON: %w", err)
	}

	return newQuestions, nil
}
