package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/bootstrap"
	"github.com/pokerjest/animateAutoTool/internal/config"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withAuthIPAllowlistSettings(t *testing.T, enabled, allowlist string) {
	t.Helper()

	previousSettings := map[string]string{}
	if config.AppConfig != nil {
		for key, value := range config.AppConfig.SystemSettings {
			previousSettings[key] = value
		}
	}
	require.NoError(t, store.NewConfigStore(db.DB).SetMany(map[string]string{
		model.ConfigKeyAuthIPAllowlistEnabled: enabled,
		model.ConfigKeyAuthIPAllowlist:        allowlist,
	}))
	t.Cleanup(func() {
		_ = db.DB.Where("key IN ?", []string{
			model.ConfigKeyAuthIPAllowlistEnabled,
			model.ConfigKeyAuthIPAllowlist,
		}).Delete(&model.GlobalConfig{}).Error
		_ = config.ReplaceSystemSettings(previousSettings)
	})
}

func TestNormalizeAuthIPAllowlist(t *testing.T) {
	normalized, err := normalizeAuthIPAllowlist("192.168.1.20, 100.64.3.8/10\n192.168.1.20\t2001:db8::1")
	require.NoError(t, err)
	assert.Equal(t, "100.64.0.0/10\n192.168.1.20\n2001:db8::1", normalized)

	for _, invalid := range []string{"not-an-ip", "0.0.0.0/0", "::/0", "224.0.0.1"} {
		_, err := normalizeAuthIPAllowlist(invalid)
		assert.Error(t, err, invalid)
	}
}

func TestAuthIPAllowlistAuthenticatesMatchingAddressWithoutCookie(t *testing.T) {
	resetAuthFixtures(t)
	withAuthIPAllowlistSettings(t, model.ConfigValueTrue, "203.0.113.25\n100.64.0.0/10")
	r := setupRouter()

	sessionRecorder := httptest.NewRecorder()
	sessionRequest := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	markRemoteRequest(sessionRequest)
	r.ServeHTTP(sessionRecorder, sessionRequest)
	require.Equal(t, http.StatusOK, sessionRecorder.Code, sessionRecorder.Body.String())

	var payload struct {
		Data struct {
			Authenticated bool   `json:"authenticated"`
			Username      string `json:"username"`
			AuthMode      string `json:"auth_mode"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(sessionRecorder.Body.Bytes(), &payload))
	assert.True(t, payload.Data.Authenticated)
	assert.Equal(t, "admin", payload.Data.Username)
	assert.Equal(t, "ip_allowlist", payload.Data.AuthMode)

	protectedRecorder := httptest.NewRecorder()
	protectedRequest := httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil)
	markRemoteRequest(protectedRequest)
	r.ServeHTTP(protectedRecorder, protectedRequest)
	assert.Equal(t, http.StatusOK, protectedRecorder.Code, protectedRecorder.Body.String())
}

func TestAuthIPAllowlistRejectsUnmatchedAndSpoofedAddresses(t *testing.T) {
	resetAuthFixtures(t)
	withAuthIPAllowlistSettings(t, model.ConfigValueTrue, "198.51.100.8")
	r := setupRouter()

	for _, configure := range []func(*http.Request){
		func(req *http.Request) {
			req.RemoteAddr = testRemoteAddr
		},
		func(req *http.Request) {
			req.RemoteAddr = testRemoteAddr
			req.Header.Set("X-Forwarded-For", "198.51.100.8")
		},
	} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil)
		configure(request)
		r.ServeHTTP(recorder, request)
		assert.Equal(t, http.StatusUnauthorized, recorder.Code, recorder.Body.String())
	}
}

func TestAuthIPAllowlistUsesForwardedAddressOnlyFromTrustedProxy(t *testing.T) {
	resetAuthFixtures(t)
	withAuthIPAllowlistSettings(t, model.ConfigValueTrue, "198.51.100.8")
	previousTrustedProxies := append([]string(nil), config.AppConfig.Server.TrustedProxies...)
	config.AppConfig.Server.TrustedProxies = []string{"127.0.0.1"}
	t.Cleanup(func() { config.AppConfig.Server.TrustedProxies = previousTrustedProxies })
	r := setupRouter()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil)
	request.RemoteAddr = "127.0.0.1:45678"
	request.Header.Set("X-Forwarded-For", "198.51.100.8")
	r.ServeHTTP(recorder, request)
	assert.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
}

func TestAuthIPAllowlistDoesNotBypassFirstRunSetup(t *testing.T) {
	resetAuthFixtures(t)
	withAuthIPAllowlistSettings(t, model.ConfigValueTrue, "127.0.0.1")
	require.NoError(t, bootstrap.SaveAdminBootstrapInfo(bootstrap.AdminBootstrapInfo{
		Username:  "admin",
		Password:  "admin",
		CreatedAt: time.Now(),
	}))
	r := setupRouter()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	markLocalRequest(request)
	r.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.Contains(t, recorder.Body.String(), `"authenticated":false`)
	assert.Contains(t, recorder.Body.String(), `"setup_pending":true`)
}

func TestV1SettingsNormalizesAndPersistsAuthIPAllowlist(t *testing.T) {
	resetAuthFixtures(t)
	withAuthIPAllowlistSettings(t, "false", "")
	r := setupRouter()
	cookie, _ := loginCookie(t, r, "admin")

	recorder := httptest.NewRecorder()
	body := bytes.NewBufferString(`{"values":{"auth_ip_allowlist_enabled":"true","auth_ip_allowlist":"192.168.1.20, 100.64.3.8/10"}}`)
	request := httptest.NewRequest(http.MethodPut, "/api/v1/settings", body)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Cookie", cookie)
	markLocalRequest(request)
	r.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	settings, err := store.NewConfigStore(db.DB).ListMap()
	require.NoError(t, err)
	assert.Equal(t, model.ConfigValueTrue, settings[model.ConfigKeyAuthIPAllowlistEnabled])
	assert.Equal(t, "100.64.0.0/10\n192.168.1.20", settings[model.ConfigKeyAuthIPAllowlist])
	assert.Equal(t, settings[model.ConfigKeyAuthIPAllowlist], config.SystemSetting(model.ConfigKeyAuthIPAllowlist))

	configData, err := os.ReadFile(config.ConfigFilePath())
	require.NoError(t, err)
	assert.Contains(t, string(configData), model.ConfigKeyAuthIPAllowlist)
}

func TestV1SettingsRejectsUnsafeAuthIPAllowlist(t *testing.T) {
	resetAuthFixtures(t)
	withAuthIPAllowlistSettings(t, "false", "")
	r := setupRouter()
	cookie, _ := loginCookie(t, r, "admin")

	for _, body := range []string{
		`{"values":{"auth_ip_allowlist_enabled":"true","auth_ip_allowlist":""}}`,
		`{"values":{"auth_ip_allowlist_enabled":"true","auth_ip_allowlist":"0.0.0.0/0"}}`,
		`{"values":{"auth_ip_allowlist_enabled":"true","auth_ip_allowlist":"not-an-ip"}}`,
	} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPut, "/api/v1/settings", bytes.NewBufferString(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Cookie", cookie)
		markLocalRequest(request)
		r.ServeHTTP(recorder, request)
		assert.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
		assert.Contains(t, recorder.Body.String(), `"code":"invalid_auth_ip_allowlist"`)
	}
}
