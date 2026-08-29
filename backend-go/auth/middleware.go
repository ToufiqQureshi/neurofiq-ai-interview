package auth

import (
	"net/http"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

// AuthMiddleware ensures the user is authenticated via session cookie
func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		userID := session.Get("user_id")
		githubToken := session.Get("github_token")

		if userID == nil || githubToken == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized: Session missing"})
			return
		}

		// Store in context for controllers to access
		c.Set("user_id", userID)
		c.Set("github_token", githubToken)

		c.Next()
	}
}
