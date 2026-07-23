package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/bootstrap"
	"github.com/pokerjest/animateAutoTool/internal/config"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/parser"
	"github.com/pokerjest/animateAutoTool/internal/service"
	"github.com/pokerjest/animateAutoTool/internal/store"
	"github.com/pokerjest/animateAutoTool/internal/taskstate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeV1MikanClient struct {
	searchItems    []parser.SearchResult
	dashboard      *parser.MikanDashboard
	subgroups      []parser.Subgroup
	episodes       []parser.Episode
	lastSearch     string
	lastYear       string
	lastSeason     string
	lastSubgroupID string
	lastRSSURL     string
}

func (f *fakeV1MikanClient) ParseContext(_ context.Context, rssURL string) ([]parser.Episode, error) {
	f.lastRSSURL = rssURL
	return f.episodes, nil
}

func (f *fakeV1MikanClient) SearchContext(_ context.Context, keyword string) ([]parser.SearchResult, error) {
	f.lastSearch = keyword
	return f.searchItems, nil
}

func (f *fakeV1MikanClient) GetSubgroupsContext(_ context.Context, mikanID string) ([]parser.Subgroup, error) {
	f.lastSubgroupID = mikanID
	return f.subgroups, nil
}

func (f *fakeV1MikanClient) GetDashboardContext(_ context.Context, year, season string) (*parser.MikanDashboard, error) {
	f.lastYear = year
	f.lastSeason = season
	return f.dashboard, nil
}

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
			Authenticated          bool   `json:"authenticated"`
			LocalRecoveryAvailable bool   `json:"local_recovery_available"`
			Version                string `json:"version"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
	assert.False(t, payload.Data.Authenticated)
	assert.True(t, payload.Data.LocalRecoveryAvailable)
	assert.NotEmpty(t, payload.Data.Version)
}

func TestV1SessionDisablesLocalRecoveryForRemoteRequests(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	markRemoteRequest(req)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var payload struct {
		Data struct {
			LocalRecoveryAvailable bool `json:"local_recovery_available"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
	assert.False(t, payload.Data.LocalRecoveryAvailable)
}

func TestV1LocalMetadataRefreshResolvesPreviousIssue(t *testing.T) {
	taskstate.Global.Reset()
	t.Cleanup(taskstate.Global.Reset)
	if err := db.DB.Exec("DELETE FROM library_issues").Error; err != nil {
		t.Fatalf("clear library issues: %v", err)
	}
	anime := model.LocalAnime{Title: "Locked Show", Path: "/library/Locked Show"}
	if err := db.DB.Create(&anime).Error; err != nil {
		t.Fatalf("create local anime: %v", err)
	}
	updateLocalMetadataIssue(&anime, errors.New("database is locked (5) (SQLITE_BUSY)"))

	previousEnricher := enrichV1LocalAnime
	enrichV1LocalAnime = func(*model.LocalAnime) error { return nil }
	t.Cleanup(func() { enrichV1LocalAnime = previousEnricher })

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(uint64(anime.ID), 10)}}
	V1RefreshLocalMetadataHandler(c)
	require.Equal(t, http.StatusAccepted, w.Code)
	require.Eventually(t, func() bool {
		task, ok := taskstate.Global.Get("local-metadata-" + strconv.FormatUint(uint64(anime.ID), 10))
		return ok && task.Status == taskstate.StatusCompleted
	}, time.Second, 10*time.Millisecond)

	issues, err := service.ListOpenLibraryIssues(10)
	require.NoError(t, err)
	assert.Empty(t, issues)
}

func TestV1SessionAdvertisesLocalSetupOnlyForDirectLoopback(t *testing.T) {
	resetAuthFixtures(t)
	require.NoError(t, bootstrap.SaveAdminBootstrapInfo(bootstrap.AdminBootstrapInfo{
		Username:  "admin",
		Password:  "admin",
		CreatedAt: time.Now(),
	}))
	r := setupRouter()

	readSession := func(mark func(*http.Request)) struct {
		SetupPending        bool `json:"setup_pending"`
		LocalSetupAvailable bool `json:"local_setup_available"`
	} {
		t.Helper()
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
		mark(req)
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var payload struct {
			Data struct {
				SetupPending        bool `json:"setup_pending"`
				LocalSetupAvailable bool `json:"local_setup_available"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
		return payload.Data
	}

	local := readSession(markLocalRequest)
	assert.True(t, local.SetupPending)
	assert.True(t, local.LocalSetupAvailable)

	remote := httptest.NewRecorder()
	remoteRequest := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	markRemoteRequest(remoteRequest)
	r.ServeHTTP(remote, remoteRequest)
	require.Equal(t, http.StatusForbidden, remote.Code)
	assert.Contains(t, remote.Body.String(), `"code":"bootstrap_local_only"`)
}

func TestV1LocalBootstrapSessionAuthenticatesPendingAdmin(t *testing.T) {
	resetAuthFixtures(t)
	require.NoError(t, bootstrap.SaveAdminBootstrapInfo(bootstrap.AdminBootstrapInfo{
		Username:  "admin",
		Password:  "admin",
		CreatedAt: time.Now(),
	}))
	r := setupRouter()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/session/bootstrap", nil)
	markLocalRequest(req)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	cookie := strings.SplitN(w.Header().Get("Set-Cookie"), ";", 2)[0]
	require.NotEmpty(t, cookie)
	sessionResponse := httptest.NewRecorder()
	sessionRequest := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	sessionRequest.Header.Set("Cookie", cookie)
	markLocalRequest(sessionRequest)
	r.ServeHTTP(sessionResponse, sessionRequest)
	require.Equal(t, http.StatusOK, sessionResponse.Code, sessionResponse.Body.String())
	assert.Contains(t, sessionResponse.Body.String(), `"authenticated":true`)
	assert.Contains(t, sessionResponse.Body.String(), `"setup_pending":true`)

	setupResponse := httptest.NewRecorder()
	setupRequest := httptest.NewRequest(http.MethodPost, "/api/v1/setup/bootstrap", bytes.NewBufferString(`{"new_password":"local-first-run-123","confirm_password":"local-first-run-123","qb_mode":"managed","base_download_dir":""}`))
	setupRequest.Header.Set("Content-Type", "application/json")
	setupRequest.Header.Set("Cookie", cookie)
	markLocalRequest(setupRequest)
	r.ServeHTTP(setupResponse, setupRequest)
	require.Equal(t, http.StatusOK, setupResponse.Code, setupResponse.Body.String())
	assert.False(t, bootstrap.BootstrapSetupPending())
	_, err := service.NewAuthService().Login("admin", "local-first-run-123")
	require.NoError(t, err)
}

func TestV1LocalBootstrapSessionRejectsRemoteAndCrossOriginRequests(t *testing.T) {
	resetAuthFixtures(t)
	require.NoError(t, bootstrap.SaveAdminBootstrapInfo(bootstrap.AdminBootstrapInfo{
		Username:  "admin",
		Password:  "admin",
		CreatedAt: time.Now(),
	}))
	r := setupRouter()

	remote := httptest.NewRecorder()
	remoteRequest := httptest.NewRequest(http.MethodPost, "/api/v1/session/bootstrap", nil)
	markRemoteRequest(remoteRequest)
	r.ServeHTTP(remote, remoteRequest)
	require.Equal(t, http.StatusForbidden, remote.Code)
	assert.Contains(t, remote.Body.String(), `"code":"bootstrap_local_only"`)

	crossOrigin := httptest.NewRecorder()
	crossOriginRequest := httptest.NewRequest(http.MethodPost, "/api/v1/session/bootstrap", nil)
	crossOriginRequest.RemoteAddr = testLocalRemoteAddr
	crossOriginRequest.Host = testLocalHost
	crossOriginRequest.Header.Set("Origin", "https://evil.example.net")
	r.ServeHTTP(crossOrigin, crossOriginRequest)
	require.Equal(t, http.StatusForbidden, crossOrigin.Code)
	assert.Contains(t, crossOrigin.Body.String(), `"code":"cross_origin_write"`)
}

func TestV1LocalBootstrapSessionClosesAfterInitialization(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/session/bootstrap", nil)
	markLocalRequest(req)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusConflict, w.Code)
	assert.Contains(t, w.Body.String(), `"code":"setup_not_pending"`)
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

func TestV1TaskSnapshotEndpointsRequireAuthAndReturnTypedState(t *testing.T) {
	resetAuthFixtures(t)
	taskstate.Global.Reset()
	t.Cleanup(taskstate.Global.Reset)
	taskstate.Global.Start("test-scan", "scan", "本地扫描", "正在扫描")
	taskstate.Global.Progress("test-scan", "正在读取剧集", 2, 5)

	r := setupRouter()
	unauthorized := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/tasks", nil)
	markLocalRequest(request)
	r.ServeHTTP(unauthorized, request)
	require.Equal(t, http.StatusUnauthorized, unauthorized.Code)

	cookie, _ := loginCookie(t, r, "admin")
	get := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Cookie", cookie)
		markLocalRequest(req)
		r.ServeHTTP(w, req)
		return w
	}

	list := get("/api/v1/tasks")
	require.Equal(t, http.StatusOK, list.Code, list.Body.String())
	assert.Contains(t, list.Body.String(), `"task_id":"test-scan"`)
	assert.Contains(t, list.Body.String(), `"status":"running"`)
	assert.Contains(t, list.Body.String(), `"current":2`)

	detail := get("/api/v1/tasks/test-scan")
	require.Equal(t, http.StatusOK, detail.Code, detail.Body.String())
	assert.Contains(t, detail.Body.String(), `"kind":"scan"`)
	assert.Contains(t, detail.Body.String(), `"total":5`)

	missing := get("/api/v1/tasks/missing")
	require.Equal(t, http.StatusNotFound, missing.Code)
	assert.Contains(t, missing.Body.String(), `"code":"task_not_found"`)
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

func TestV1SettingsSaveMirrorsValuesToConfigFile(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()
	cookie, _ := loginCookie(t, r, "admin")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/settings", bytes.NewBufferString(`{"values":{"qb_url":"http://local-qb:8080","qb_password":"mirror-secret"}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", cookie)
	markLocalRequest(req)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	data, err := os.ReadFile(config.ConfigFilePath())
	require.NoError(t, err)
	assert.Contains(t, string(data), "qb_url: http://local-qb:8080")
	assert.Contains(t, string(data), "qb_password: mirror-secret")
}

func TestV1SettingsNormalizeAndPersistProxyOptions(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()
	cookie, _ := loginCookie(t, r, "admin")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/settings", bytes.NewBufferString(`{"values":{"proxy_url":"127.0.0.1:7890","proxy_bangumi_enabled":"true","proxy_mikan_enabled":"true","proxy_ai_enabled":"true","proxy_updater_enabled":"true"}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", cookie)
	markLocalRequest(req)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	configs, err := store.NewConfigStore(db.DB).ListMap()
	require.NoError(t, err)
	assert.Equal(t, "http://127.0.0.1:7890", configs[model.ConfigKeyProxyURL])
	assert.Equal(t, model.ConfigValueTrue, configs[model.ConfigKeyProxyMikan])
	assert.Equal(t, model.ConfigValueTrue, configs[model.ConfigKeyProxyAI])
	assert.Equal(t, model.ConfigValueTrue, configs[model.ConfigKeyProxyUpdater])

	data, err := os.ReadFile(config.ConfigFilePath())
	require.NoError(t, err)
	assert.Contains(t, string(data), "proxy_url: http://127.0.0.1:7890")
}

func TestV1SettingsRejectInvalidProxyURL(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()
	cookie, _ := loginCookie(t, r, "admin")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/settings", bytes.NewBufferString(`{"values":{"proxy_url":"ftp://127.0.0.1:21"}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", cookie)
	markLocalRequest(req)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"code":"invalid_proxy_url"`)
	assert.Empty(t, store.NewConfigStore(db.DB).GetDefault(model.ConfigKeyProxyURL, ""))
}

func TestV1ProxyTestUsesSubmittedProxy(t *testing.T) {
	resetAuthFixtures(t)
	var proxyHit atomic.Bool
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyHit.Store(true)
		assert.Equal(t, "http://proxy-probe.test/health", r.URL.String())
		w.WriteHeader(http.StatusNoContent)
	}))
	defer proxyServer.Close()

	previousProbeURL := proxyProbeURL
	proxyProbeURL = "http://proxy-probe.test/health"
	t.Cleanup(func() { proxyProbeURL = previousProbeURL })

	r := setupRouter()
	cookie, _ := loginCookie(t, r, "admin")
	w := httptest.NewRecorder()
	body := `{"proxy_url":` + strconv.Quote(proxyServer.URL) + `}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/proxy/test", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", cookie)
	markLocalRequest(req)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.True(t, proxyHit.Load())
	assert.Contains(t, w.Body.String(), `"connected":true`)
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

func TestV1MikanDiscoveryEndpointsUseTypedContracts(t *testing.T) {
	resetAuthFixtures(t)
	episodes := make([]parser.Episode, 21)
	for i := range episodes {
		episodes[i] = parser.Episode{Title: "Episode", EpisodeNum: "1", SubGroup: "ANi", Resolution: "1080p"}
	}
	fake := &fakeV1MikanClient{
		searchItems: []parser.SearchResult{{MikanID: "3141", Title: "测试番剧", Image: "https://mikanani.me/poster.jpg"}},
		dashboard: &parser.MikanDashboard{Season: "2026 夏季番组", Days: map[string][]parser.SearchResult{
			"1": {{MikanID: "3141", Title: "测试番剧", Image: "https://mikanani.me/poster.jpg"}},
		}},
		subgroups: []parser.Subgroup{{ID: "", Name: "全部 (All)"}, {ID: "583", Name: "ANi"}},
		episodes:  episodes,
	}
	previousFactory := newV1MikanClient
	newV1MikanClient = func() v1MikanClient { return fake }
	t.Cleanup(func() { newV1MikanClient = previousFactory })

	r := setupRouter()
	cookie, _ := loginCookie(t, r, "admin")
	request := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Cookie", cookie)
		markLocalRequest(req)
		r.ServeHTTP(w, req)
		return w
	}

	search := request("/api/v1/subscriptions/search?q=%E6%B5%8B%E8%AF%95")
	require.Equal(t, http.StatusOK, search.Code, search.Body.String())
	assert.Contains(t, search.Body.String(), `"mikan_id":"3141"`)
	assert.NotContains(t, search.Body.String(), `"MikanID"`)
	assert.Equal(t, "测试", fake.lastSearch)

	dashboard := request("/api/v1/subscriptions/mikan/dashboard?year=2026&season=%E5%A4%8F")
	require.Equal(t, http.StatusOK, dashboard.Code, dashboard.Body.String())
	assert.Contains(t, dashboard.Body.String(), `"season":"2026 夏季番组"`)
	assert.Contains(t, dashboard.Body.String(), `"days"`)
	assert.Equal(t, "2026", fake.lastYear)
	assert.Equal(t, "夏", fake.lastSeason)

	groups := request("/api/v1/subscriptions/mikan/subgroups?mikan_id=3141")
	require.Equal(t, http.StatusOK, groups.Code, groups.Body.String())
	assert.Contains(t, groups.Body.String(), `"name":"全部字幕组"`)
	assert.Contains(t, groups.Body.String(), `"is_all":true`)
	assert.Equal(t, "3141", fake.lastSubgroupID)

	preview := request("/api/v1/subscriptions/mikan/episodes?mikan_id=3141&subgroup_id=583")
	require.Equal(t, http.StatusOK, preview.Code, preview.Body.String())
	var previewPayload struct {
		Data struct {
			MikanID string           `json:"mikan_id"`
			Items   []parser.Episode `json:"items"`
			Total   int              `json:"total"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(preview.Body.Bytes(), &previewPayload))
	assert.Equal(t, "3141", previewPayload.Data.MikanID)
	assert.Len(t, previewPayload.Data.Items, 20)
	assert.Equal(t, 21, previewPayload.Data.Total)
	assert.True(t, strings.Contains(fake.lastRSSURL, "bangumiId=3141"))
	assert.True(t, strings.Contains(fake.lastRSSURL, "subgroupid=583"))

	invalid := request("/api/v1/subscriptions/mikan/subgroups?mikan_id=not-a-number")
	require.Equal(t, http.StatusBadRequest, invalid.Code)
	assert.Contains(t, invalid.Body.String(), `"code":"invalid_mikan_id"`)
}

func TestV1SubscriptionPersistsMikanAssociationWithoutUsingBangumiSubjectID(t *testing.T) {
	resetAuthFixtures(t)
	require.NoError(t, db.DB.Exec("DELETE FROM subscriptions").Error)
	require.NoError(t, db.DB.Exec("DELETE FROM anime_metadata").Error)
	previousEnrich := enrichSubscriptionMetadata
	enrichSubscriptionMetadata = func(_ *model.AnimeMetadata, _ string) {}
	previousRun := runSubscriptionCheck
	runComplete := make(chan struct{}, 1)
	runSubscriptionCheck = func(_ *model.Subscription, _ string) error {
		runComplete <- struct{}{}
		return nil
	}
	t.Cleanup(func() {
		enrichSubscriptionMetadata = previousEnrich
		runSubscriptionCheck = previousRun
	})

	r := setupRouter()
	cookie, _ := loginCookie(t, r, "admin")
	body := `{"title":"测试番剧","rss_url":"https://mikanani.me/RSS/Bangumi?bangumiId=3141&subgroupid=583","backup_rss_url":"https://mikanani.me/RSS/Bangumi?bangumiId=3141","mikan_id":"3141","image":"https://mikanani.me/poster.jpg","subtitle_group":"ANi","season":"2026 夏季番组","filter_rule":"ANi","stale_after_hours":168}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/subscriptions", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", cookie)
	markLocalRequest(req)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	<-runComplete

	var created model.Subscription
	require.NoError(t, db.DB.Preload("Metadata").Where("rss_url LIKE ?", "%bangumiId=3141%").First(&created).Error)
	assert.Equal(t, "3141", created.MikanID)
	assert.Equal(t, "ANi", created.SubtitleGroup)
	assert.Equal(t, "2026 夏季番组", created.Season)
	assert.Equal(t, "https://mikanani.me/poster.jpg", created.Image)
	var incorrectMetadataCount int64
	require.NoError(t, db.DB.Model(&model.AnimeMetadata{}).Where("bangumi_id = ?", 3141).Count(&incorrectMetadataCount).Error)
	assert.Zero(t, incorrectMetadataCount, "Mikan identifiers must never be stored as bgm.tv subject IDs")

	updateBody := `{"title":"测试番剧","rss_url":"https://mikanani.me/RSS/Bangumi?bangumiId=3141","mikan_id":"3141","image":"https://mikanani.me/new.jpg","subtitle_group":"","season":"2026 夏季番组","filter_rule":"","allow_multi_subgroup":true,"stale_after_hours":168}`
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/api/v1/subscriptions/"+strconv.FormatUint(uint64(created.ID), 10), bytes.NewBufferString(updateBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", cookie)
	markLocalRequest(req)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, db.DB.First(&created, created.ID).Error)
	assert.Equal(t, "3141", created.MikanID)
	assert.Empty(t, created.SubtitleGroup)
	assert.Empty(t, created.FilterRule)
	assert.True(t, created.AllowMultiSubgroup)
	assert.Equal(t, "https://mikanani.me/new.jpg", created.Image)
}

func TestRestoringSubscriptionRefreshesMikanAssociationFields(t *testing.T) {
	resetAuthFixtures(t)
	require.NoError(t, db.DB.Exec("DELETE FROM subscriptions").Error)
	require.NoError(t, db.DB.Exec("DELETE FROM anime_metadata").Error)
	previousEnrich := enrichSubscriptionMetadata
	enrichSubscriptionMetadata = func(_ *model.AnimeMetadata, _ string) {}
	previousRun := runSubscriptionCheck
	runs := make(chan struct{}, 2)
	runSubscriptionCheck = func(_ *model.Subscription, _ string) error { runs <- struct{}{}; return nil }
	t.Cleanup(func() {
		enrichSubscriptionMetadata = previousEnrich
		runSubscriptionCheck = previousRun
	})

	feedURL := "https://mikanani.me/RSS/Bangumi?bangumiId=3141&subgroupid=583"
	original := &model.Subscription{Title: "旧标题", RSSUrl: feedURL, MikanID: "3141", SubtitleGroup: "旧字幕组", Image: "old.jpg", Season: "旧季度"}
	require.NoError(t, createSubscriptionInternal(original))
	<-runs
	require.NoError(t, db.DB.Delete(original).Error)

	restored := &model.Subscription{Title: "新标题", RSSUrl: feedURL, MikanID: "3141", SubtitleGroup: "ANi", Image: "new.jpg", Season: "2026 夏季番组", FilterRule: "ANi"}
	require.NoError(t, createSubscriptionInternal(restored))
	<-runs

	var got model.Subscription
	require.NoError(t, db.DB.Unscoped().First(&got, original.ID).Error)
	assert.False(t, got.DeletedAt.Valid)
	assert.Equal(t, "3141", got.MikanID)
	assert.Equal(t, "ANi", got.SubtitleGroup)
	assert.Equal(t, "new.jpg", got.Image)
	assert.Equal(t, "2026 夏季番组", got.Season)
	assert.Equal(t, "ANi", got.FilterRule)
}
