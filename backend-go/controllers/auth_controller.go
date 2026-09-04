package controllers

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/config"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/models"
)

type RegisterReq struct {
	FullName string `json:"full_name" binding:"required"`
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=8"`
}

type LoginReq struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

type OnboardingReq struct {
	FullName         string `json:"full_name"`
	ExperienceLevel  string `json:"experience_level" binding:"required"`
	TargetRole       string `json:"target_role" binding:"required"`
	TechStack        string `json:"tech_stack"`
	LinkedInURL      string `json:"linkedin_url"`
	CollegeOrCompany string `json:"college_or_company"`
	InterviewGoal    string `json:"interview_goal"`
}

// HandleRegister creates a new user account with email and hashed password.
func HandleRegister(c *gin.Context) {
	var req RegisterReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Please provide a valid full name, email, and password (min 8 characters)"})
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))

	// Check if user already exists
	var existing models.User
	if err := config.DB.Where("email = ?", email).First(&existing).Error; err == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "An account with this email already exists"})
		return
	}

	// Hash password using bcrypt
	hashedBytes, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to secure password"})
		return
	}

	user := models.User{
		FullName:     strings.TrimSpace(req.FullName),
		Email:        email,
		PasswordHash: string(hashedBytes),
		PlanType:     "free",
		Role:         "candidate",
		IsOnboarded:  false,
		LastLoginAt:  time.Now(),
	}

	if err := config.DB.Create(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create user account"})
		return
	}

	// Set session cookie
	session := sessions.Default(c)
	session.Set("user_id", user.ID)
	if err := session.Save(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save session"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "Account created successfully",
		"user":    user,
	})
}

// HandleLogin authenticates an existing user using email and password.
func HandleLogin(c *gin.Context) {
	var req LoginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid email or password format"})
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))

	var user models.User
	if err := config.DB.Where("email = ?", email).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid email or password"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}

	if user.PasswordHash == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "This account was registered using GitHub. Please sign in with GitHub."})
		return
	}

	// Compare bcrypt hash
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid email or password"})
		return
	}

	// Update last login
	user.LastLoginAt = time.Now()
	config.DB.Model(&user).Update("last_login_at", user.LastLoginAt)

	// Set session cookie
	session := sessions.Default(c)
	session.Set("user_id", user.ID)
	if err := session.Save(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save session"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Login successful",
		"user":    user,
	})
}

// HandleOnboarding saves the candidate's profile checklist and marks them as onboarded.
func HandleOnboarding(c *gin.Context) {
	userID := c.MustGet("user_id").(string)

	var req OnboardingReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Please provide your experience level and target role"})
		return
	}

	// tech_stack is what the interviewer tailors questions to — the form
	// asks for at least 2, but that was a client-only check an empty value
	// sailed straight past.
	techCount := 0
	for _, t := range strings.Split(req.TechStack, ",") {
		if strings.TrimSpace(t) != "" {
			techCount++
		}
	}
	if techCount < 2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Select at least 2 technologies"})
		return
	}

	var user models.User
	if err := config.DB.Where("id = ?", userID).First(&user).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	user.IsOnboarded = true
	user.ExperienceLevel = strings.TrimSpace(req.ExperienceLevel)
	user.TargetRole = strings.TrimSpace(req.TargetRole)
	user.TechStack = strings.TrimSpace(req.TechStack)
	user.LinkedInURL = strings.TrimSpace(req.LinkedInURL)
	user.CollegeOrCompany = strings.TrimSpace(req.CollegeOrCompany)
	user.InterviewGoal = strings.TrimSpace(req.InterviewGoal)

	if req.FullName != "" && (user.FullName == "" || user.FullName == user.GithubUsername) {
		user.FullName = strings.TrimSpace(req.FullName)
	}

	if err := config.DB.Save(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save onboarding details: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Onboarding completed successfully",
		"user":    user,
	})
}
