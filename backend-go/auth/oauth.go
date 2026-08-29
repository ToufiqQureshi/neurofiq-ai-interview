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

func InitOAuth() {
	GithubOAuthConfig = &oauth2.Config{
		ClientID:     os.Getenv("GITHUB_CLIENT_ID"),
		ClientSecret: os.Getenv("GITHUB_CLIENT_SECRET"),
		RedirectURL:  os.Getenv("OAUTH_REDIRECT_URL"),
		Scopes:       []string{"repo", "user:email"},
		Endpoint:     github.Endpoint,
	}
}

func generateOAuthState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

func HandleGithubLogin(c *gin.Context) {
	state, err := generateOAuthState()
	if err != nil {
		log.Println("[oauth] FAIL: could not generate state:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start login"})
		return
	}

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
	} else {
		user.LastLoginAt = time.Now()
		if user.Email == "" && githubUser.Email != "" {
			user.Email = githubUser.Email
		}
		_ = config.DB.Save(&user).Error
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
	c.Redirect(http.StatusTemporaryRedirect, frontendURL+"/dashboard")
}

func HandleAuthMe(c *gin.Context) {
	session := sessions.Default(c)
	userID := session.Get("user_id")

	if userID == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Not authenticated"})
		return
	}

	var user models.User
	if err := config.DB.Where("id = ?", userID).First(&user).Error; err != nil {
		session.Clear()
		session.Save()
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"user": user,
	})
}

func HandleLogout(c *gin.Context) {
	session := sessions.Default(c)
	session.Clear()
	_ = session.Save()
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
