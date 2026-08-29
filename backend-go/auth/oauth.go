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

	"github.com/gin-gonic/gin"
	"github.com/gin-contrib/sessions"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/config"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/models"
)

var GithubOAuthConfig *oauth2.Config

// InitOAuth sets up the OAuth configuration using our .env variables
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

// HandleGithubLogin redirects the user to GitHub's consent page
func HandleGithubLogin(c *gin.Context) {
	state, err := generateOAuthState()
	if err != nil {
		log.Println("[oauth] FAIL: could not generate state:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start login"})
		return
	}

	// Stash the state in the session so the callback can verify it came from
	// this same browser, preventing login-CSRF.
	session := sessions.Default(c)
	session.Set("oauth_state", state)
	if err := session.Save(); err != nil {
		log.Println("[oauth] FAIL: could not save state to session:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start login"})
		return
	}

	url := GithubOAuthConfig.AuthCodeURL(state)

	// c.Redirect is a Gin function that sends an HTTP 302 redirect response
	c.Redirect(http.StatusTemporaryRedirect, url)
}

// HandleGithubCallback is called by GitHub after the user logs in
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
	// One-time use: clear it immediately so it can't be replayed.
	session.Delete("oauth_state")

	code := c.Query("code")
	if code == "" {
		log.Println("[oauth] FAIL: no code in query, error param:", c.Query("error"), c.Query("error_description"))
		c.JSON(http.StatusBadRequest, gin.H{"error": "Code not found"})
		return
	}

	// Exchange the code for a token
	token, err := GithubOAuthConfig.Exchange(context.Background(), code)
	if err != nil {
		log.Println("[oauth] FAIL: token exchange:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to exchange token"})
		return
	}
	log.Println("[oauth] token exchange OK")

	// Fetch user details from GitHub
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

	// GitHub omits the email on /user when the user has it set to private.
	// Fall back to the primary verified address from /user/emails (needs the user:email scope).
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
	log.Printf("[oauth] github user OK: id=%d login=%s email=%q\n", githubUser.ID, githubUser.Login, githubUser.Email)

	// Lookup or create user in DB
	var user models.User
	result := config.DB.Where("github_id = ?", githubUser.ID).First(&user)

	if result.Error != nil {
		log.Println("[oauth] no existing user row (lookup error:", result.Error, ") -- creating new one")
		// User not found, create new
		user = models.User{
			GithubID:        githubUser.ID,
			GithubUsername:  githubUser.Login,
			Email:           githubUser.Email,
			AvatarURL:       githubUser.AvatarURL,
			GithubConnected: true,
			LastLoginAt:     time.Now(),
		}
		if err := config.DB.Create(&user).Error; err != nil {
			log.Println("[oauth] FAIL: create user:", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create user"})
			return
		}
		log.Println("[oauth] created user id:", user.ID)
	} else {
		// User exists, update last login (and backfill email if we can now resolve one)
		user.LastLoginAt = time.Now()
		if user.Email == "" && githubUser.Email != "" {
			user.Email = githubUser.Email
		}
		if err := config.DB.Save(&user).Error; err != nil {
			log.Println("[oauth] WARN: failed to update last_login_at:", err)
		}
		log.Println("[oauth] existing user id:", user.ID)
	}

	// Save the token and user ID in the session cookie (state was already
	// cleared above; reuse the same session handle)
	session.Set("user_id", user.ID)
	session.Set("github_token", token.AccessToken)

	if err := session.Save(); err != nil {
		log.Println("[oauth] FAIL: session save:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save session"})
		return
	}
	log.Println("[oauth] session saved for user_id:", user.ID, "-- redirecting to dashboard")

	// Redirect back to the frontend dashboard
	frontendURL := os.Getenv("FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = "http://localhost:5173"
	}
	c.Redirect(http.StatusTemporaryRedirect, frontendURL+"/dashboard")
}

// HandleAuthMe checks the session cookie and returns the current logged-in user
func HandleAuthMe(c *gin.Context) {
	session := sessions.Default(c)
	userID := session.Get("user_id")

	if userID == nil {
		log.Println("[auth/me] no user_id in session (cookie missing or empty)")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Not authenticated"})
		return
	}
	log.Printf("[auth/me] session user_id = %v (%T)\n", userID, userID)

	var user models.User
	if err := config.DB.Where("id = ?", userID).First(&user).Error; err != nil {
		log.Println("[auth/me] FAIL: db lookup for user_id", userID, ":", err)
		// Session exists but user deleted from DB? Clear session
		session.Clear()
		session.Save()
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not found"})
		return
	}
	log.Println("[auth/me] OK, resolved user:", user.GithubUsername)

	c.JSON(http.StatusOK, gin.H{
		"user": user,
	})
}
