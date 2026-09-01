package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/config"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/models"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/services"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
)

var GithubOAuthConfig *oauth2.Config

// InitOAuth sets up the OAuth configuration using our .env variables.
func InitOAuth() {
	GithubOAuthConfig = &oauth2.Config{
		ClientID:     os.Getenv("GITHUB_CLIENT_ID"),
		ClientSecret: os.Getenv("GITHUB_CLIENT_SECRET"),
		RedirectURL:  os.Getenv("OAUTH_REDIRECT_URL"),
		Scopes:       []string{"repo", "user:email"},
		Endpoint:     github.Endpoint,
	}
}

// generateOAuthState returns a cryptographically random, URL-safe string used
// as the OAuth CSRF `state` token.
func generateOAuthState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// HandleGithubLogin redirects the user to GitHub's consent page.
func HandleGithubLogin(c *gin.Context) {
	state, err := generateOAuthState()
	if err != nil {
		log.Println("[oauth] FAIL: could not generate state:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start login"})
		return
	}

	// Stash the state in the session so the callback can verify the response
	// came back to the same browser that started the flow, preventing login-CSRF.
	session := sessions.Default(c)
	session.Set("oauth_state", state)
	if err := session.Save(); err != nil {
		log.Println("[oauth] FAIL: could not save state to session:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start login"})
		return
	}

	url := GithubOAuthConfig.AuthCodeURL(state)
	c.Redirect(http.StatusTemporaryRedirect, url)
}

// HandleGithubCallback is called by GitHub after the user logs in.
func HandleGithubCallback(c *gin.Context) {
	log.Println("[oauth] callback hit:", c.Request.URL.String())

	session := sessions.Default(c)
	expectedState, _ := session.Get("oauth_state").(string)
	state := c.Query("state")
	if expectedState == "" || state != expectedState {
		log.Println("[oauth] FAIL: bad state:", state)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid state"})
		return
	}
	// One-time use: clear it immediately so it cannot be replayed.
	session.Delete("oauth_state")

	code := c.Query("code")
	if code == "" {
		log.Println("[oauth] FAIL: no code in query, error param:", c.Query("error"), c.Query("error_description"))
		c.JSON(http.StatusBadRequest, gin.H{"error": "Code not found"})
		return
	}

	token, err := GithubOAuthConfig.Exchange(context.Background(), code)
	if err != nil {
		log.Println("[oauth] FAIL: token exchange:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to exchange token"})
		return
	}

	client := GithubOAuthConfig.Client(context.Background(), token)
	resp, err := client.Get("https://api.github.com/user")
	if err != nil {
		log.Println("[oauth] FAIL: fetch github user:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch GitHub user"})
		return
	}
	defer resp.Body.Close()

	var githubUser struct {
		ID        int64  `json:"id"`
		Login     string `json:"login"`
		Email     string `json:"email"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&githubUser); err != nil {
		log.Println("[oauth] FAIL: decode github user:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse GitHub user data"})
		return
	}

	// GitHub omits the email on /user when the user keeps it private. Fall back
	// to the primary verified address from /user/emails (needs user:email).
	if githubUser.Email == "" {
		if emailResp, err := client.Get("https://api.github.com/user/emails"); err == nil {
			defer emailResp.Body.Close()
			var emails []struct {
				Email    string `json:"email"`
				Primary  bool   `json:"primary"`
				Verified bool   `json:"verified"`
			}
			if err := json.NewDecoder(emailResp.Body).Decode(&emails); err == nil {
				for _, e := range emails {
					if e.Primary && e.Verified {
						githubUser.Email = e.Email
						break
					}
				}
			}
		}
	}

	var user models.User
	result := config.DB.Where("github_id = ?", githubUser.ID).First(&user)

	if result.Error != nil {
		ghID := githubUser.ID
		user = models.User{
			GithubID:        &ghID,
			GithubUsername:  githubUser.Login,
			FullName:        githubUser.Login,
			Email:           githubUser.Email,
			AvatarURL:       githubUser.AvatarURL,
			GithubConnected: true,
			IsOnboarded:     false,
			LastLoginAt:     time.Now(),
		}
		if err := config.DB.Create(&user).Error; err != nil {
			log.Println("[oauth] FAIL: create user:", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create user"})
			return
		}
	} else {
		user.LastLoginAt = time.Now()
		if user.Email == "" && githubUser.Email != "" {
			user.Email = githubUser.Email
		}
		if err := config.DB.Save(&user).Error; err != nil {
			// Non-fatal: a failed last_login bump must not block a valid login.
			log.Println("[oauth] WARN: failed to update last_login_at:", err)
		}
	}

	session.Set("user_id", user.ID)
	session.Set("github_token", token.AccessToken)

	if err := session.Save(); err != nil {
		log.Println("[oauth] FAIL: session save:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save session"})
		return
	}

	frontendURL := os.Getenv("FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = "http://localhost:5173"
	}
	
	targetPath := "/dashboard"
	if !user.IsOnboarded {
		targetPath = "/onboarding"
	}
	c.Redirect(http.StatusTemporaryRedirect, frontendURL+targetPath)
}

// HandleAuthMe checks the session cookie and returns the current user.
func HandleAuthMe(c *gin.Context) {
	session := sessions.Default(c)
	userID := session.Get("user_id")

	if userID == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Not authenticated"})
		return
	}

	var user models.User
	if err := config.DB.Where("id = ?", userID).First(&user).Error; err != nil {
		// Session points at a user row that is gone — clear it so the browser
		// stops presenting a cookie we will never accept.
		session.Clear()
		if err := session.Save(); err != nil {
			log.Println("[auth/me] WARN: failed to clear a session for a missing user:", err)
		}
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"user": user,
	})
}

// HandleLogout clears the session cookie and drops anything we were holding
// in memory for this user.
func HandleLogout(c *gin.Context) {
	session := sessions.Default(c)
	if userID, ok := session.Get("user_id").(string); ok && userID != "" {
		services.InvalidateRepoCache(userID)
	}
	session.Clear()
	session.Options(sessions.Options{Path: "/", MaxAge: -1})
	if err := session.Save(); err != nil {
		log.Println("[oauth] WARN: failed to clear session on logout:", err)
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
