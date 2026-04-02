package api

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/config"
)

func TestGetServerBaseURLUsesPublicURLWhenConfigured(t *testing.T) {
	prev := config.AppConfig
	t.Cleanup(func() {
		config.AppConfig = prev
	})

	config.AppConfig = &config.Config{
		Server: config.ServerConfig{
			Port:      8306,
			PublicURL: "https://anime.example.com/",
		},
		Auth: config.AuthConfig{SecretKey: "test"},
	}

	if got := getServerBaseURL(nil); got != "https://anime.example.com" {
		t.Fatalf("expected configured public URL, got %q", got)
	}
}

func TestGetServerBaseURLUsesRequestHost(t *testing.T) {
	prev := config.AppConfig
	t.Cleanup(func() {
		config.AppConfig = prev
	})

	config.AppConfig = &config.Config{
		Server: config.ServerConfig{
			Port:           8306,
			TrustedProxies: []string{"127.0.0.1"},
		},
		Auth: config.AuthConfig{SecretKey: "test"},
	}

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	req := httptest.NewRequest("GET", "/api/bangumi/login", nil)
	req.Host = "127.0.0.1:8306"
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "anime.example.com")
	ctx.Request = req

	if got := getBangumiRedirectURI(ctx); got != "https://anime.example.com/api/bangumi/callback" {
		t.Fatalf("unexpected redirect URI: %q", got)
	}
}

func TestGetServerBaseURLIgnoresForwardedHeadersFromUntrustedProxy(t *testing.T) {
	prev := config.AppConfig
	t.Cleanup(func() {
		config.AppConfig = prev
	})

	config.AppConfig = &config.Config{
		Server: config.ServerConfig{
			Port:           8306,
			TrustedProxies: []string{"127.0.0.1"},
		},
		Auth: config.AuthConfig{SecretKey: "test"},
	}

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	req := httptest.NewRequest("GET", "/api/bangumi/login", nil)
	req.Host = "127.0.0.1:8306"
	req.RemoteAddr = "203.0.113.10:12345"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "anime.example.com")
	ctx.Request = req

	if got := getBangumiRedirectURI(ctx); got != "http://127.0.0.1:8306/api/bangumi/callback" {
		t.Fatalf("unexpected redirect URI for untrusted proxy: %q", got)
	}
}
