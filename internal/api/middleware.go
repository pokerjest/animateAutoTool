package api

import (
	"net/http"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		userID := session.Get("user_id")

		if userID == nil {
			// Check if it's an API request or page load
			// For API requests, return 401
			// For page loads, redirect to login
			path := c.Request.URL.Path
			// Simple check for API or Static
			// Ideally we check Accept header too, but path is often enough
			if len(path) >= 4 && path[:4] == "/api" {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
				return
			}

			c.Redirect(http.StatusFound, "/login")
			c.Abort()
			return
		}
		c.Next()
	}
}
