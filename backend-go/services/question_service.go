package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/config"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/models"
)

type GenerateQuestionsPayload struct {
	RepoFullName string `json:"repo_full_name"`
	AnalysisData string `json:"analysis_data"`
}

func GetOrGenerateQuestions(userID string, repoFullName string) ([]models.Question, error) {
	// 1. Fetch analysis from DB
	var profile models.GithubProfile
	if err := config.DB.Where("user_id = ? AND repo_full_name = ?", userID, repoFullName).First(&profile).Error; err != nil {
		return nil, fmt.Errorf("repo analysis not found for user: %v", err)
	}

	// 2. Try to find cached/reusable questions in the DB
	// For MVP, we'll try to find any 5 reusable questions for this specific repo name,
	// or just generic reusable ones if we want to simulate the tech-stack matching.
	var cachedQuestions []models.Question
	config.DB.Where("reusable = ? AND language = ?", true, repoFullName).Limit(5).Find(&cachedQuestions)

	if len(cachedQuestions) >= 5 {
		// YAGNI / Cost-Saving: Use cached questions!
		return cachedQuestions, nil
	}

	// 3. Fallback: Call Python AI worker to generate new questions
	payload := GenerateQuestionsPayload{
		RepoFullName: repoFullName,
		AnalysisData: profile.AnalysisJSON,
	}

	newQuestions, err := callPythonQuestionGenerator(payload)
	if err != nil {
		return nil, err
	}

	// 4. Save newly generated questions to DB for future reuse
	for i := range newQuestions {
		newQuestions[i].Reusable = true
		newQuestions[i].Language = repoFullName // Using repoFullName as the 'tech stack' group for MVP
		config.DB.Create(&newQuestions[i])
	}

	return newQuestions, nil
}

func callPythonQuestionGenerator(payload GenerateQuestionsPayload) ([]models.Question, error) {
	workerURL := os.Getenv("PYTHON_WORKER_URL")
	if workerURL == "" {
		workerURL = "http://localhost:8001"
	}

	jsonData, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", workerURL+"/internal/generate-questions", bytes.NewBuffer(jsonData))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Secret", os.Getenv("INTERNAL_SECRET"))

	resp, err := http.DefaultClient.Do(req)
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
