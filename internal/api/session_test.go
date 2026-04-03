package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testProxyRemoteAddr = "127.0.0.1:12345"
	testHTTPRemoteAddr  = "127.0.0.1:34567"
	testLoginHost       = "localhost:8306"
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
	req.RemoteAddr = testProxyRemoteAddr
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
	req.RemoteAddr = testHTTPRemoteAddr

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	setCookie := w.Header().Get("Set-Cookie")
	require.NotEmpty(t, setCookie)
	assert.NotContains(t, setCookie, "Secure")
	assert.Contains(t, setCookie, "HttpOnly")
	assert.Contains(t, setCookie, "SameSite=Lax")
}

func TestLoginRateLimitAppliesBackoffAfterRepeatedFailures(t *testing.T) {
	withSessionTestConfig(t, "", []string{"127.0.0.1", "::1"})
	resetLoginThrottleState()

	now := time.Unix(1_700_000_000, 0)
	setLoginThrottleTimeNow(func() time.Time { return now })
	t.Cleanup(func() {
		resetLoginThrottleState()
	})

	r := setupRouter()

	for i := 0; i < loginBackoffThreshold; i++ {
		body := []byte(`{"username":"admin","password":"wrong-pass"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = testHTTPRemoteAddr
		req.Host = testLoginHost

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusUnauthorized, w.Code)
	}

	body := []byte(`{"username":"admin","password":"wrong-pass"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = testHTTPRemoteAddr
	req.Host = testLoginHost

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.Equal(t, "2", w.Header().Get("Retry-After"))
	state := currentLoginThrottleState("127.0.0.1")
	assert.Equal(t, loginBackoffThreshold, state.Failures)
	assert.True(t, state.LockedUntil.After(now))
}

func TestSuccessfulLoginClearsPreviousFailureBackoff(t *testing.T) {
	withSessionTestConfig(t, "", []string{"127.0.0.1", "::1"})
	resetLoginThrottleState()
	t.Cleanup(func() {
		resetLoginThrottleState()
	})

	r := setupRouter()
	ip := "127.0.0.1"
	registerFailedLoginAttempt(ip)
	registerFailedLoginAttempt(ip)

	body := []byte(`{"username":"admin","password":"admin","remember_me":false}`)
	req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = ip + ":45678"
	req.Host = testLoginHost

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	state := currentLoginThrottleState(ip)
	assert.Equal(t, 0, state.Failures)
	assert.True(t, state.LockedUntil.IsZero())
}
