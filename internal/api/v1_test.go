package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestV1SessionUsesDataEnvelope(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	markLocalRequest(req)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var payload struct {
		Data struct {
			Authenticated bool   `json:"authenticated"`
			Version       string `json:"version"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
	assert.False(t, payload.Data.Authenticated)
	assert.NotEmpty(t, payload.Data.Version)
}

func TestV1ProtectedEndpointsUseStructuredErrors(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard", nil)
	markLocalRequest(req)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
	var payload v1ErrorEnvelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
	assert.Equal(t, "unauthorized", payload.Error.Code)
}

func TestV1SettingsNeverReturnSecretValues(t *testing.T) {
	resetAuthFixtures(t)
	require.NoError(t, db.DB.Create(&model.GlobalConfig{Key: model.ConfigKeyAIApiKey, Value: "top-secret"}).Error)
	r := setupRouter()
	cookie, _ := loginCookie(t, r, "admin")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil)
	req.Header.Set("Cookie", cookie)
	markLocalRequest(req)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	assert.NotContains(t, w.Body.String(), "top-secret")
	assert.Contains(t, w.Body.String(), `"ai_api_key":true`)
}

func TestV1WriteRejectsCrossOriginRequests(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()
	cookie, _ := loginCookie(t, r, "admin")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/settings", bytes.NewBufferString(`{"values":{}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", cookie)
	markRemoteRequest(req)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusForbidden, w.Code)
	var payload v1ErrorEnvelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
	assert.Equal(t, "cross_origin_write", payload.Error.Code)
}

func TestV1LoginRejectsCrossOriginRequests(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/session/login", bytes.NewBufferString(`{"username":"admin","password":"admin"}`))
	req.Header.Set("Content-Type", "application/json")
	markRemoteRequest(req)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusForbidden, w.Code)
	var payload v1ErrorEnvelope
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
	assert.Equal(t, "cross_origin_write", payload.Error.Code)
}

func TestV1PaginationUsesStandardMeta(t *testing.T) {
	resetAuthFixtures(t)
	require.NoError(t, db.DB.Create(&model.Subscription{Title: "First", RSSUrl: "https://example.test/one", IsActive: true}).Error)
	require.NoError(t, db.DB.Create(&model.Subscription{Title: "Second", RSSUrl: "https://example.test/two", IsActive: true}).Error)
	r := setupRouter()
	cookie, _ := loginCookie(t, r, "admin")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/subscriptions?page=2&page_size=1", nil)
	req.Header.Set("Cookie", cookie)
	markLocalRequest(req)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var payload struct {
		Data struct {
			Items []model.Subscription `json:"items"`
		} `json:"data"`
		Meta struct {
			Page     int   `json:"page"`
			PageSize int   `json:"page_size"`
			Total    int64 `json:"total"`
		} `json:"meta"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
	assert.Len(t, payload.Data.Items, 1)
	assert.Equal(t, 2, payload.Meta.Page)
	assert.Equal(t, 1, payload.Meta.PageSize)
	assert.GreaterOrEqual(t, payload.Meta.Total, int64(2))
}

func TestProductionRouterDoesNotExposeLegacyAPI(t *testing.T) {
	resetAuthFixtures(t)
	previous := gin.Mode()
	gin.SetMode(gin.ReleaseMode)
	t.Cleanup(func() { gin.SetMode(previous) })
	r := gin.New()
	InitRoutes(r)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewBufferString(`{"username":"admin","password":"admin"}`))
	markLocalRequest(req)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "not_found")
}
