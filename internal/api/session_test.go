package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withSessionTestConfig(t *testing.T, publicURL string, trustedProxies []string) {
	t.Helper()

	prevPublicURL := config.AppConfig.Server.PublicURL
	prevTrustedProxies := append([]string(nil), config.AppConfig.Server.TrustedProxies...)

	config.AppConfig.Server.PublicURL = publicURL
	config.AppConfig.Server.TrustedProxies = append([]string(nil), trustedProxies...)

	t.Cleanup(func() {
		config.AppConfig.Server.PublicURL = prevPublicURL
		config.AppConfig.Server.TrustedProxies = prevTrustedProxies
	})
}

func TestRequestUsesSecureCookiesFromTrustedProxy(t *testing.T) {
	withSessionTestConfig(t, "", []string{"127.0.0.1", "::1"})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodGet, "http://example.test/login", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("X-Forwarded-Proto", "https")
	c.Request = req

	assert.True(t, requestUsesSecureCookies(c))
}

func TestLoginRememberMePreservesSecureCookieAttributes(t *testing.T) {
	withSessionTestConfig(t, "", []string{"127.0.0.1", "::1"})

	r := setupRouter()

	body := []byte(`{"username":"admin","password":"admin","remember_me":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "127.0.0.1:23456"
	req.Header.Set("X-Forwarded-Proto", "https")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	setCookie := w.Header().Get("Set-Cookie")
	require.NotEmpty(t, setCookie)
	assert.Contains(t, setCookie, "Secure")
	assert.Contains(t, setCookie, "HttpOnly")
	assert.Contains(t, setCookie, "SameSite=Lax")
	assert.Contains(t, setCookie, "Max-Age=2592000")
}

func TestLoginCookieStaysInsecureOnPlainHTTP(t *testing.T) {
	withSessionTestConfig(t, "", []string{"127.0.0.1", "::1"})

	r := setupRouter()

	body := []byte(`{"username":"admin","password":"admin","remember_me":false}`)
	req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "127.0.0.1:34567"

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	setCookie := w.Header().Get("Set-Cookie")
	require.NotEmpty(t, setCookie)
	assert.NotContains(t, setCookie, "Secure")
	assert.Contains(t, setCookie, "HttpOnly")
	assert.Contains(t, setCookie, "SameSite=Lax")
}
