package api

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/config"
)

const (
	testLoopbackHost = "127.0.0.1:8306"
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
	req.Host = testLoopbackHost
	req.RemoteAddr = testLocalRemoteAddr
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
	req.Host = testLoopbackHost
	req.RemoteAddr = "203.0.113.10:12345"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "anime.example.com")
	ctx.Request = req

	if got := getBangumiRedirectURI(ctx); got != "http://127.0.0.1:8306/api/bangumi/callback" {
		t.Fatalf("unexpected redirect URI for untrusted proxy: %q", got)
	}
}

func TestRequestSameOriginAcceptsTrustedProxyOrigin(t *testing.T) {
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
	req := httptest.NewRequest("POST", "/api/settings", nil)
	req.Host = testLoopbackHost
	req.RemoteAddr = testLocalRemoteAddr
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "anime.example.com")
	req.Header.Set("Origin", "https://anime.example.com")
	ctx.Request = req

	if !requestSameOrigin(ctx) {
		t.Fatal("expected same-origin check to accept trusted proxy origin")
	}
}

func TestRequestSameOriginRejectsCrossSiteOrigin(t *testing.T) {
	prev := config.AppConfig
	t.Cleanup(func() {
		config.AppConfig = prev
	})

	config.AppConfig = &config.Config{
		Server: config.ServerConfig{
			Port: 8306,
		},
		Auth: config.AuthConfig{SecretKey: "test"},
	}

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	req := httptest.NewRequest("POST", "/api/settings", nil)
	req.Host = "localhost:8306"
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Origin", "https://evil.example.net")
	ctx.Request = req

	if requestSameOrigin(ctx) {
		t.Fatal("expected same-origin check to reject mismatched origin")
	}
}

func TestRequestSameOriginAcceptsRequestHostOriginWhenPublicURLDiffers(t *testing.T) {
	prev := config.AppConfig
	t.Cleanup(func() {
		config.AppConfig = prev
	})

	config.AppConfig = &config.Config{
		Server: config.ServerConfig{
			Port:      8306,
			PublicURL: "https://anime.example.com",
		},
		Auth: config.AuthConfig{SecretKey: "test"},
	}

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	req := httptest.NewRequest("POST", "/api/subscriptions", nil)
	req.Host = "localhost:8306"
	req.RemoteAddr = testLocalRemoteAddr
	req.Header.Set("Origin", "http://localhost:8306")
	ctx.Request = req

	if !requestSameOrigin(ctx) {
		t.Fatal("expected same-origin check to accept request-host origin when public_url differs")
	}
}
