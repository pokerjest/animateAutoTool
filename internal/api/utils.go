package api

import (
	"fmt"
	"net"
	"net/url"
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

func remoteRequestIP(c *gin.Context) net.IP {
	if c == nil || c.Request == nil {
		return nil
	}

	remoteHost := strings.TrimSpace(c.Request.RemoteAddr)
	if host, _, err := net.SplitHostPort(remoteHost); err == nil {
		remoteHost = host
	}

	if remoteHost == "" {
		return nil
	}

	return net.ParseIP(remoteHost)
}

func requestFromLoopback(c *gin.Context) bool {
	remoteIP := remoteRequestIP(c)
	return remoteIP != nil && remoteIP.IsLoopback()
}

func requestUsesForwardedHeaders(c *gin.Context) bool {
	if c == nil || c.Request == nil {
		return false
	}

	for _, key := range []string{"Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto"} {
		if strings.TrimSpace(c.Request.Header.Get(key)) != "" {
			return true
		}
	}

	return false
}

func requestTargetsLoopbackHost(c *gin.Context) bool {
	if c == nil || c.Request == nil {
		return false
	}

	host := strings.TrimSpace(c.Request.Host)
	if host == "" {
		return false
	}
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
	}

	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}

	hostIP := net.ParseIP(host)
	return hostIP != nil && hostIP.IsLoopback()
}

func requestFromTrustedProxy(c *gin.Context) bool {
	if c == nil || c.Request == nil || config.AppConfig == nil || len(config.AppConfig.Server.TrustedProxies) == 0 {
		return false
	}

	remoteIP := remoteRequestIP(c)
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

func requestClientIP(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return ""
	}

	if requestFromTrustedProxy(c) {
		if forwardedFor := firstHeaderValue(c.Request.Header.Get("X-Forwarded-For")); forwardedFor != "" {
			forwardedFor = strings.TrimSpace(strings.Trim(forwardedFor, "[]"))
			if host, _, err := net.SplitHostPort(forwardedFor); err == nil {
				forwardedFor = host
			}
			if ip := net.ParseIP(forwardedFor); ip != nil {
				return ip.String()
			}
		}
	}

	if remoteIP := remoteRequestIP(c); remoteIP != nil {
		return remoteIP.String()
	}

	return ""
}

func normalizeRequestOrigin(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}

	return strings.ToLower(parsed.Scheme) + "://" + strings.ToLower(parsed.Host)
}

func requestSameOrigin(c *gin.Context) bool {
	if c == nil || c.Request == nil {
		return false
	}

	expected := normalizeRequestOrigin(getServerBaseURL(c))
	if expected == "" {
		return false
	}

	if origin := normalizeRequestOrigin(c.Request.Header.Get("Origin")); origin != "" {
		return origin == expected
	}

	if referer := normalizeRequestOrigin(c.Request.Header.Get("Referer")); referer != "" {
		return referer == expected
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
