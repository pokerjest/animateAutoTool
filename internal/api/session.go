package api

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/config"
)

func secureCookiesFromConfig() bool {
	if config.AppConfig == nil {
		return false
	}

	publicURL := strings.TrimSpace(config.AppConfig.Server.PublicURL)
	if publicURL == "" {
		return false
	}

	parsed, err := url.Parse(publicURL)
	if err != nil {
		return strings.HasPrefix(strings.ToLower(publicURL), "https://")
	}

	return strings.EqualFold(parsed.Scheme, "https")
}

func requestUsesSecureCookies(c *gin.Context) bool {
	if secureCookiesFromConfig() {
		return true
	}
	if c == nil || c.Request == nil {
		return false
	}
	if c.Request.TLS != nil {
		return true
	}
	if requestFromTrustedProxy(c) {
		return strings.EqualFold(firstHeaderValue(c.Request.Header.Get("X-Forwarded-Proto")), "https")
	}
	return false
}

func sessionCookieOptions(c *gin.Context, maxAge int) sessions.Options {
	return sessions.Options{
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestUsesSecureCookies(c),
	}
}
