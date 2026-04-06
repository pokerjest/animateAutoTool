package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/stretchr/testify/assert"
)

func TestUpdateSettingsMediaScopeSavesJellyfinAPIKey(t *testing.T) {
	resetAuthFixtures(t)

	r := setupRouter()
	cookie, _ := loginCookie(t, r, "admin")

	form := url.Values{
		"settings_scope":                {"media"},
		model.ConfigKeyJellyfinUrl:      {""},
		model.ConfigKeyJellyfinApiKey:   {"test-api-key-123"},
		model.ConfigKeyJellyfinUsername: {""},
		model.ConfigKeyJellyfinPassword: {""},
	}

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", cookie)
	markLocalRequest(req)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var apiKeyCfg model.GlobalConfig
	err := db.DB.Where("key = ?", model.ConfigKeyJellyfinApiKey).First(&apiKeyCfg).Error
	if err != nil {
		t.Fatalf("expected jellyfin api key to be persisted, got error: %v", err)
	}
	assert.Equal(t, "test-api-key-123", apiKeyCfg.Value)
}

func TestRenderSettingsTemplateIncludesJellyfinAPIKeyInputID(t *testing.T) {
	html, err := renderTemplateToString("settings.html", map[string]interface{}{
		"SkipLayout":       true,
		"Config":           map[string]string{},
		"JellyfinServerID": "",
		"Stats":            BackupStats{},
	})
	if err != nil {
		t.Fatalf("expected settings template to render, got error: %v", err)
	}

	assert.Contains(t, html, `id="jellyfin_api_key_input"`)
}
