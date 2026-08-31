package controllers

import (
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/config"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/models"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/services"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// The interview is five questions. Everything an answer submission carries is
// bounded, because every one of these fields is forwarded to a paid LLM call:
// without a cap, one authenticated user can post a thousand-item list of
// 50 KB answers and bill us for it, on repeat.
const (
	maxQAItems          = 5
	maxAnswerChars      = 6000
	maxQuestionChars    = 1200
	maxInterviewsPerDay = 20
)

type SubmitInterviewReq struct {
	RepoFullName string            `json:"repo_full_name" binding:"required"`
	QAList       []services.QAItem `json:"qa_list" binding:"required"`
	Mode         string            `json:"mode"`
}

func HandleSubmitInterview(c *gin.Context) {
	userID := c.MustGet("user_id").(string)

	var reqBody SubmitInterviewReq
	if err := c.ShouldBindJSON(&reqBody); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
		return
	}
	if !services.ValidRepoFullName(reqBody.RepoFullName) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "repo_full_name must be owner/name"})
		return
	}
	if len(reqBody.QAList) == 0 || len(reqBody.QAList) > maxQAItems {
		c.JSON(http.StatusBadRequest, gin.H{"error": "An interview is between 1 and 5 questions."})
		return
	}

	// The analysis has to exist and belong to this user. Without this check
	// the evaluation endpoint is a general-purpose LLM proxy that anyone with
	// an account can point at any text.
	var profile models.GithubProfile
	if err := config.DB.Where("user_id = ? AND repo_full_name = ?", userID, reqBody.RepoFullName).
		First(&profile).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Analyze this repository before submitting an interview."})
		return
	}
	if profile.StrategyUsed != "full_scan" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "This repository's analysis is not finished."})
		return
	}

	// Every submitted question must be one we actually issued for this repo.
	issued, err := issuedQuestionSet(reqBody.RepoFullName)
	if err != nil || len(issued) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Start the interview from the repository page so we know which questions to score."})
		return
	}
	for i := range reqBody.QAList {
		q := strings.TrimSpace(reqBody.QAList[i].Question)
		if len(q) > maxQuestionChars || !issued[normalizeQuestion(q)] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "One of the questions doesn't match this interview. Reload the page and try again."})
			return
		}
		answer := strings.TrimSpace(reqBody.QAList[i].Answer)
		if len(answer) > maxAnswerChars {
			answer = answer[:maxAnswerChars]
		}
		if answer == "" {
			answer = "Skipped"
		}
		reqBody.QAList[i].Question = q
		reqBody.QAList[i].Answer = answer
	}

	// A per-day ceiling on scored interviews. The per-IP rate limiter does
	// not cover this: one account running the loop at a request an hour costs
	// real money and never trips it.
	var todayCount int64
	config.DB.Model(&models.InterviewSession{}).
		Where("user_id = ? AND created_at > ?", userID, time.Now().Add(-24*time.Hour)).
		Count(&todayCount)
	if todayCount >= maxInterviewsPerDay {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "Daily interview limit reached. Try again tomorrow."})
		return
	}

	payload := services.EvaluatePayload{
		RepoFullName: reqBody.RepoFullName,
		QAList:       reqBody.QAList,
	}

	// 1. Call the Python AI worker to evaluate the answers.
	feedbackJSON, score, err := services.CallPythonEvaluationWorker(payload)
	if err != nil {
		// Logged in full, reported generically: this error can carry the
		// worker's raw response body.
		log.Printf("submit: evaluation failed for user=%s repo=%s: %v", userID, reqBody.RepoFullName, err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "Could not score this interview right now. Your answers were not lost — try submitting again."})
		return
	}

	qaJSON, _ := json.Marshal(reqBody.QAList)

	mode := reqBody.Mode
	if mode != "voice" {
		mode = "text"
	}

	// 2. Save the session report.
	session := models.InterviewSession{
		UserID:        userID,
		RepoFullName:  reqBody.RepoFullName,
		QuestionsJSON: string(qaJSON), // saving the combined QA
		AnswersJSON:   string(qaJSON),
		OverallScore:  score,
		FeedbackJSON:  feedbackJSON,
		InterviewType: "code_interview",
		Mode:          mode,
	}

	if err := config.DB.Create(&session).Error; err != nil {
		log.Printf("submit: failed to save session for user=%s: %v", userID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save session to DB"})
		return
	}

	// Return the ID of the new session so the frontend can redirect.
	c.JSON(http.StatusOK, gin.H{"session_id": session.ID})
}

// issuedQuestionSet is every question we have generated for a repository,
// normalized for comparison.
func issuedQuestionSet(repoFullName string) (map[string]bool, error) {
	var questions []models.Question
	if err := config.DB.Select("question_text").
		Where("language = ?", repoFullName).Find(&questions).Error; err != nil {
		return nil, err
	}
	set := make(map[string]bool, len(questions))
	for _, q := range questions {
		set[normalizeQuestion(q.QuestionText)] = true
	}
	return set, nil
}

// normalizeQuestion collapses the whitespace and quoting differences a round
// trip through the browser can introduce, without being loose enough to match
// a different question.
func normalizeQuestion(text string) string {
	lower := strings.ToLower(strings.TrimSpace(text))
	lower = strings.NewReplacer("‘", "'", "’", "'", "“", "\"", "”", "\"").Replace(lower)
	return strings.Join(strings.Fields(lower), " ")
}

func HandleGetReportByID(c *gin.Context) {
	userID := c.MustGet("user_id").(string)

	var session models.InterviewSession
	if err := config.DB.Where("id = ? AND user_id = ?", c.Param("id"), userID).First(&session).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Report not found"})
		return
	}

	c.JSON(http.StatusOK, session)
}

func HandleGetReports(c *gin.Context) {
	userID := c.MustGet("user_id").(string)

	var sessions []models.InterviewSession
	// Newest first.
	if err := config.DB.Where("user_id = ?", userID).Order("created_at desc").Limit(200).Find(&sessions).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch reports"})
		return
	}

	c.JSON(http.StatusOK, sessions)
}

// HandleShareReport turns public sharing on or off for one report.
//
// A finished report is the only artifact this product creates that a
// candidate actually wants to send to somebody. Until it has a public URL it
// dies behind the login, and so does every chance of the product spreading.
func HandleShareReport(c *gin.Context) {
	userID := c.MustGet("user_id").(string)

	var body struct {
		Public bool `json:"public"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	var session models.InterviewSession
	if err := config.DB.Where("id = ? AND user_id = ?", c.Param("id"), userID).First(&session).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Report not found"})
		return
	}

	if !body.Public {
		// Clearing both fields makes the old link 404 immediately — an
		// unshare that leaves the row readable is not an unshare.
		if err := config.DB.Model(&session).
			Updates(map[string]interface{}{"share_slug": nil, "shared_at": nil}).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not update sharing"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"public": false})
		return
	}

	if session.ShareSlug == nil || *session.ShareSlug == "" {
		slug, err := newShareSlug()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not create a share link"})
			return
		}
		now := time.Now()
		if err := config.DB.Model(&session).
			Updates(map[string]interface{}{"share_slug": slug, "shared_at": now}).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not create a share link"})
			return
		}
		session.ShareSlug = &slug
	}

	c.JSON(http.StatusOK, gin.H{"public": true, "slug": *session.ShareSlug})
}

// HandleGetPublicReport serves a shared report to anyone holding the link.
//
// It deliberately returns a narrowed shape rather than the session row: the
// candidate shared a score and its reasoning, not their raw answers.
func HandleGetPublicReport(c *gin.Context) {
	slug := c.Param("slug")
	if len(slug) < 8 || len(slug) > 64 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Report not found"})
		return
	}

	var session models.InterviewSession
	err := config.DB.Where("share_slug = ?", slug).First(&session).Error
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			log.Printf("public report: lookup failed for slug=%s: %v", slug, err)
		}
		c.JSON(http.StatusNotFound, gin.H{"error": "Report not found"})
		return
	}

	var owner models.User
	// Non-fatal: a report whose owner row has gone missing still has a
	// score and feedback worth showing, so the page renders without the
	// byline rather than 404ing.
	if err := config.DB.Select("github_username", "avatar_url").
		Where("id = ?", session.UserID).First(&owner).Error; err != nil {
		log.Printf("public report: owner lookup failed for session=%s: %v", session.ID, err)
	}

	// Only the question, the score, and the assessment travel. The
	// candidate's own answers stay private.
	type publicFeedback struct {
		Question            string  `json:"question"`
		Score               float64 `json:"score"`
		Strengths           string  `json:"strengths"`
		AreasForImprovement string  `json:"areas_for_improvement"`
	}
	var parsed struct {
		OverallScore     float64          `json:"overall_score"`
		OverallFeedback  string           `json:"overall_feedback"`
		DetailedFeedback []publicFeedback `json:"detailed_feedback"`
	}
	_ = json.Unmarshal([]byte(session.FeedbackJSON), &parsed)

	c.JSON(http.StatusOK, gin.H{
		"repo_full_name":    session.RepoFullName,
		"overall_score":     session.OverallScore,
		"overall_feedback":  parsed.OverallFeedback,
		"detailed_feedback": parsed.DetailedFeedback,
		"mode":              session.Mode,
		"created_at":        session.CreatedAt,
		"candidate": gin.H{
			"github_username": owner.GithubUsername,
			"avatar_url":      owner.AvatarURL,
		},
	})
}

// newShareSlug returns an unguessable, URL-safe identifier. The link itself is
// the only credential a public report has, so it must not be enumerable.
func newShareSlug() (string, error) {
	buf := make([]byte, 10)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf)), nil
}
