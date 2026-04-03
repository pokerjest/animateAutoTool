package api

import (
	"net/http"
	"strings"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/bootstrap"
)

const securityHeadersCSP = "default-src 'self'; script-src 'self' 'unsafe-inline' 'unsafe-eval' https://unpkg.com; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob: https:; font-src 'self' data: https:; connect-src 'self' http: https: ws: wss:; media-src 'self' blob: data: http: https:; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'"

func isAPIRequestPath(path string) bool {
	return strings.HasPrefix(path, "/api")
}

func setupEnforcementExempt(path string) bool {
	switch path {
	case "/setup", "/recover":
		return true
	}

	return path == "/logout" || strings.HasPrefix(path, "/api/setup/") || path == "/api/recovery/reset-admin"
}

func requestIsDirectLoopback(c *gin.Context) bool {
	return requestFromLoopback(c) && !requestUsesForwardedHeaders(c) && requestTargetsLoopbackHost(c)
}

func DirectLocalOnlyMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if requestIsDirectLoopback(c) {
			c.Next()
			return
		}

		if isAPIRequestPath(c.Request.URL.Path) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "this endpoint is only available from a direct localhost connection",
			})
			return
		}

		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"error": "此页面仅允许在本机通过 localhost 直接访问。",
		})
	}
}

func BootstrapLocalOnlyMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !bootstrap.BootstrapSetupPending() {
			c.Next()
			return
		}

		if requestIsDirectLoopback(c) {
			c.Next()
			return
		}

		if isAPIRequestPath(c.Request.URL.Path) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "initial setup is only available from a direct localhost connection",
			})
			return
		}

		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"error": "初始化尚未完成，请在本机通过 localhost 直接访问并完成首次改密。",
		})
	}
}

func requestRequiresSameOrigin(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return false
	default:
		return true
	}
}

func SameOriginMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requestRequiresSameOrigin(c.Request.Method) {
			c.Next()
			return
		}

		if requestSameOrigin(c) {
			c.Next()
			return
		}

		if isAPIRequestPath(c.Request.URL.Path) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "cross-site write requests are not allowed",
			})
			return
		}

		c.AbortWithStatus(http.StatusForbidden)
	}
}

func SecurityHeadersMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		headers := c.Writer.Header()
		headers.Set("Content-Security-Policy", securityHeadersCSP)
		headers.Set("X-Frame-Options", "DENY")
		headers.Set("X-Content-Type-Options", "nosniff")
		headers.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		headers.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=(), payment=()")
		headers.Set("Cross-Origin-Opener-Policy", "same-origin")
		c.Next()
	}
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
