package api

import (
	"net/http"
	"strings"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/bootstrap"
)

const securityHeadersCSP = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob: http: https:; font-src 'self' data:; connect-src 'self' http: https: ws: wss:; media-src 'self' blob: data: http: https:; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'"

func isAPIRequestPath(path string) bool {
	return strings.HasPrefix(path, "/api")
}

func isV1APIRequestPath(path string) bool {
	return path == "/api/v1" || strings.HasPrefix(path, "/api/v1/")
}

func setupEnforcementExempt(path string) bool {
	switch path {
	case "/setup", "/recover":
		return true
	}

	return path == "/logout" ||
		strings.HasPrefix(path, "/api/setup/") ||
		strings.HasPrefix(path, "/api/v1/setup/") ||
		path == "/api/recovery/reset-admin" ||
		path == "/api/system/pick-directory" ||
		path == "/api/v1/system/pick-directory"
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
			if isV1APIRequestPath(c.Request.URL.Path) {
				v1Error(c, http.StatusForbidden, "local_only", "这个接口仅允许在本机通过 localhost 直接访问。")
				return
			}
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "这个接口仅允许在本机通过 localhost 直接访问。",
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
			if isV1APIRequestPath(c.Request.URL.Path) {
				v1Error(c, http.StatusForbidden, "bootstrap_local_only", "首次初始化仅允许在本机通过 localhost 直接访问。")
				return
			}
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "首次初始化仅允许在本机通过 localhost 直接访问。",
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
			if isV1APIRequestPath(c.Request.URL.Path) {
				v1Error(c, http.StatusForbidden, "cross_origin_write", "不允许跨站发起写操作请求。")
				return
			}
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": "不允许跨站发起写操作请求。",
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
				if isV1APIRequestPath(path) {
					v1Error(c, http.StatusUnauthorized, "unauthorized", "请先登录")
					return
				}
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "请先登录"})
				return
			}

			c.Redirect(http.StatusFound, "/login")
			c.Abort()
			return
		}

		if bootstrap.BootstrapSetupPending() && !setupEnforcementExempt(path) {
			if isAPIRequestPath(path) {
				if isV1APIRequestPath(path) {
					v1Error(c, http.StatusForbidden, "setup_required", "需要先完成初始化设置")
					return
				}
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
					"error":    "需要先完成初始化设置",
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
