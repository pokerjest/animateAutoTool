package api

import (
	"net/http"
	"strings"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/bootstrap"
)

func isAPIRequestPath(path string) bool {
	return strings.HasPrefix(path, "/api")
}

func setupEnforcementExempt(path string) bool {
	switch path {
	case "/setup":
		return true
	}

	return path == "/logout" || strings.HasPrefix(path, "/api/setup/")
}

func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		userID := session.Get("user_id")
		path := c.Request.URL.Path

		if userID == nil {
			if isAPIRequestPath(path) {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
				return
			}

			c.Redirect(http.StatusFound, "/login")
			c.Abort()
			return
		}

		if bootstrap.BootstrapSetupPending() && !setupEnforcementExempt(path) {
			if isAPIRequestPath(path) {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
					"error":    "setup required",
					"redirect": "/setup",
				})
				return
			}

			c.Redirect(http.StatusFound, "/setup")
			c.Abort()
			return
		}

		c.Next()
	}
}
