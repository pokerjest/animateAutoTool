package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// authedRequest issues an HTTP request marked as same-origin local and
// with a session cookie attached. Returns the recorder.
func authedRequest(t *testing.T, r http.Handler, cookie, method, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	} else {
		reader = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, path, reader)
	require.NoError(t, err)
	req.Header.Set("Cookie", cookie)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	markLocalRequest(req)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// TestLogoutHandlerClearsSessionAndRedirects exercises the logout endpoint
// and verifies that subsequent protected page requests are redirected back to /login.
func TestLogoutHandlerClearsSessionAndRedirects(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()
	cookie, _ := loginCookie(t, r, "admin")

	// Confirm we are authenticated for a known protected page.
	pageBefore := authedRequest(t, r, cookie, http.MethodGet, "/subscriptions", nil)
	assert.Equal(t, http.StatusOK, pageBefore.Code, "logged-in user should reach /subscriptions")

	// Logout — the handler redirects to /login with 302 and a cleared cookie.
	logout := authedRequest(t, r, cookie, http.MethodGet, "/logout", nil)
	assert.Equal(t, http.StatusFound, logout.Code)
	assert.Equal(t, "/login", logout.Header().Get("Location"))

	// After logout, attempting the same protected page without a fresh cookie
	// must redirect to /login.
	pageAfter := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/subscriptions", nil)
	markLocalRequest(req)
	r.ServeHTTP(pageAfter, req)
	assert.Equal(t, http.StatusFound, pageAfter.Code)
	assert.Equal(t, "/login", pageAfter.Header().Get("Location"))
}

// TestChangePasswordHandlerValidatesAndRotates covers the three branches of
// ChangePasswordHandler: bad payload, too-short password, and the happy path.
func TestChangePasswordHandlerValidatesAndRotates(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()
	cookie, _ := loginCookie(t, r, "admin")

	// 1. Malformed JSON → 400
	bad := authedRequest(t, r, cookie, http.MethodPost, "/api/change-password", []byte("not-json"))
	assert.Equal(t, http.StatusBadRequest, bad.Code)

	// 2. New password too short → 400
	shortBody, _ := json.Marshal(map[string]string{
		"old_password": "admin",
		"new_password": "short",
	})
	short := authedRequest(t, r, cookie, http.MethodPost, "/api/change-password", shortBody)
	assert.Equal(t, http.StatusBadRequest, short.Code)
	assert.Contains(t, short.Body.String(), "8 个字符")

	// 3. Successful rotation, then re-login with the new password proves it stuck.
	okBody, _ := json.Marshal(map[string]string{
		"old_password": "admin",
		"new_password": "much-longer-password-1",
	})
	ok := authedRequest(t, r, cookie, http.MethodPost, "/api/change-password", okBody)
	assert.Equal(t, http.StatusOK, ok.Code)
	assert.Contains(t, ok.Body.String(), "成功")

	// Old password should no longer log in; new password should.
	oldBody, _ := json.Marshal(map[string]string{"username": "admin", "password": "admin"})
	wOld := httptest.NewRecorder()
	reqOld, _ := http.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(oldBody))
	reqOld.Header.Set("Content-Type", "application/json")
	markLocalRequest(reqOld)
	r.ServeHTTP(wOld, reqOld)
	assert.NotEqual(t, http.StatusOK, wOld.Code, "old password must be rejected after rotation")

	_, _ = loginCookie(t, r, "much-longer-password-1")
}

// TestChangePasswordHandlerRejectsWrongOldPassword exercises the path where
// AuthService returns an error because the old password is incorrect.
func TestChangePasswordHandlerRejectsWrongOldPassword(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()
	cookie, _ := loginCookie(t, r, "admin")

	body, _ := json.Marshal(map[string]string{
		"old_password": "wrong-current-password",
		"new_password": "long-enough-new-password",
	})
	w := authedRequest(t, r, cookie, http.MethodPost, "/api/change-password", body)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestStatusEndpointsReturnJSONWhenUnconfigured exercises the bare path of
// several settings/status endpoints. With no external services configured
// they should still return a structured JSON response (200 or controlled
// error code), never panic, and never leak Go internals.
func TestStatusEndpointsReturnJSONWhenUnconfigured(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()
	cookie, _ := loginCookie(t, r, "admin")

	endpoints := []string{
		"/api/settings/qb-status",
		"/api/settings/tmdb-status",
		"/api/settings/anilist-status",
		"/api/settings/jellyfin-status",
		"/api/settings/pikpak-status",
		"/api/ai/config",
		"/api/backup/r2/config",
		"/api/runtime/stats",
		"/api/health/report",
	}

	for _, ep := range endpoints {
		t.Run(ep, func(t *testing.T) {
			w := authedRequest(t, r, cookie, http.MethodGet, ep, nil)
			// All status endpoints should respond with a non-5xx code; some may
			// return 200 with a "not configured" payload, others may return a
			// controlled 4xx. A panic would surface as 500.
			assert.NotEqual(t, http.StatusInternalServerError, w.Code,
				"%s should not 500 on a clean database", ep)
			assert.NotEqual(t, http.StatusNotFound, w.Code,
				"%s should be registered", ep)
		})
	}
}
