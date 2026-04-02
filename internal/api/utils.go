package api

import (
	"fmt"
	"net"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/config"
	"github.com/pokerjest/animateAutoTool/internal/qbutil"
)

// IsHTMX checks if the request is from HTMX
func IsHTMX(c *gin.Context) bool {
	return c.GetHeader("HX-Request") == "true"
}

// FetchQBConfig reliably fetches QB config without GORM scope issues
func FetchQBConfig() (string, string, string) {
	cfg := qbutil.LoadConfig()
	return cfg.URL, cfg.Username, cfg.Password
}

func firstHeaderValue(raw string) string {
	if raw == "" {
		return ""
	}

	parts := strings.Split(raw, ",")
	return strings.TrimSpace(parts[0])
}

func requestFromTrustedProxy(c *gin.Context) bool {
	if c == nil || c.Request == nil || config.AppConfig == nil || len(config.AppConfig.Server.TrustedProxies) == 0 {
		return false
	}

	remoteHost := strings.TrimSpace(c.Request.RemoteAddr)
	if host, _, err := net.SplitHostPort(remoteHost); err == nil {
		remoteHost = host
	}

	remoteIP := net.ParseIP(remoteHost)
	if remoteIP == nil {
		return false
	}

	for _, candidate := range config.AppConfig.Server.TrustedProxies {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if _, cidr, err := net.ParseCIDR(candidate); err == nil && cidr.Contains(remoteIP) {
			return true
		}
		if ip := net.ParseIP(candidate); ip != nil && ip.Equal(remoteIP) {
			return true
		}
	}

	return false
}

func getServerBaseURL(c *gin.Context) string {
	if config.AppConfig != nil && config.AppConfig.Server.PublicURL != "" {
		return strings.TrimRight(config.AppConfig.Server.PublicURL, "/")
	}

	if c != nil && c.Request != nil {
		scheme := "http"
		if c.Request.TLS != nil {
			scheme = "https"
		}
		host := c.Request.Host

		if requestFromTrustedProxy(c) {
			if forwardedScheme := firstHeaderValue(c.Request.Header.Get("X-Forwarded-Proto")); forwardedScheme != "" {
				scheme = forwardedScheme
			}
			if forwardedHost := firstHeaderValue(c.Request.Header.Get("X-Forwarded-Host")); forwardedHost != "" {
				host = forwardedHost
			}
		}
		if host != "" {
			return fmt.Sprintf("%s://%s", scheme, host)
		}
	}

	port := 8306
	if config.AppConfig != nil && config.AppConfig.Server.Port != 0 {
		port = config.AppConfig.Server.Port
	}

	return fmt.Sprintf("http://localhost:%d", port)
}

func getBangumiRedirectURI(c *gin.Context) string {
	return getServerBaseURL(c) + "/api/bangumi/callback"
}
