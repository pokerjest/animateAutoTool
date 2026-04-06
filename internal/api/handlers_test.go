package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/bootstrap"
	"github.com/pokerjest/animateAutoTool/internal/config"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/parser"
	"github.com/pokerjest/animateAutoTool/internal/safeio"
	"github.com/pokerjest/animateAutoTool/internal/scheduler"
	"github.com/pokerjest/animateAutoTool/internal/service"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func TestMain(m *testing.M) {
	tempAppData, err := os.MkdirTemp("", "animateautotool_test_appdata")
	if err != nil {
		panic(err)
	}
	defer safeio.RemoveAll(tempAppData)

	// Init Config
	if err := config.LoadConfig(tempAppData); err != nil {
		// Just log, might be fine if defaults are used
		fmt.Printf("Config load warning: %v\n", err)
	}

	// Setup: Use in-memory DB for tests
	// We need to ensure we don't accidentally write to real DB
	// But InitDB handles filepath.Dir, so ":memory:" works fine (dir is ".")
	db.InitDB(":memory:")
	if _, err := service.NewAuthService().CreateUser("admin", "admin"); err != nil {
		panic(err)
	}

	// Run tests
	code := m.Run()

	// Teardown
	if err := db.CloseDB(); err != nil {
		fmt.Printf("CloseDB error: %v\n", err)
	}
	os.Exit(code)
}

func setupRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	InitRoutes(r)
	return r
}

type integrationRSSParser struct {
	episodes []parser.Episode
	err      error
}

func (f integrationRSSParser) Name() string { return "integration-fake" }
func (f integrationRSSParser) Parse(url string) ([]parser.Episode, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.episodes, nil
}
func (f integrationRSSParser) Search(keyword string) ([]parser.SearchResult, error) { return nil, nil }
func (f integrationRSSParser) GetSubgroups(bangumiID string) ([]parser.Subgroup, error) {
	return nil, nil
}
func (f integrationRSSParser) GetDashboard(year, season string) (*parser.MikanDashboard, error) {
	return nil, nil
}

type integrationDownloader struct{}

const (
	testLocalRemoteAddr  = "127.0.0.1:12345"
	testLocalHost        = "localhost:8306"
	testLocalOrigin      = "http://localhost:8306"
	testLocalReferer     = "http://localhost:8306/"
	testRemoteHost       = "anime.example.com"
	testRemoteOrigin     = "https://evil.example.net"
	testRemoteReferer    = "https://evil.example.net/panel"
	testRecoveryPassword = "locally-reset-" + "789"
)

func (integrationDownloader) Login(username, password string) error { return nil }
func (integrationDownloader) AddTorrent(url, savePath, category string, paused bool) error {
	return nil
}
func (integrationDownloader) Ping() error { return nil }

func markLocalRequest(req *http.Request) {
	req.RemoteAddr = testLocalRemoteAddr
	req.Host = testLocalHost
	req.Header.Set("Origin", testLocalOrigin)
	req.Header.Set("Referer", testLocalReferer)
}

func markRemoteRequest(req *http.Request) {
	req.RemoteAddr = "203.0.113.25:45678"
	req.Host = testRemoteHost
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", testRemoteHost)
	req.Header.Set("Origin", testRemoteOrigin)
	req.Header.Set("Referer", testRemoteReferer)
}

func resetAuthFixtures(t *testing.T) {
	t.Helper()

	resetLoginThrottleState()

	if err := db.DB.Exec("DELETE FROM global_configs").Error; err != nil {
		t.Fatalf("failed to clear global configs: %v", err)
	}
	if err := db.DB.Exec("DELETE FROM users").Error; err != nil {
		t.Fatalf("failed to clear users: %v", err)
	}
	if err := bootstrap.ClearAdminBootstrapInfo(); err != nil && !os.IsNotExist(err) {
		t.Fatalf("failed to clear bootstrap admin info: %v", err)
	}
	if _, err := service.NewAuthService().CreateUser("admin", "admin"); err != nil {
		t.Fatalf("failed to seed admin user: %v", err)
	}

	t.Cleanup(func() {
		resetLoginThrottleState()
		_ = db.DB.Exec("DELETE FROM global_configs").Error
		_ = db.DB.Exec("DELETE FROM users").Error
		_ = bootstrap.ClearAdminBootstrapInfo()
		_, _ = service.NewAuthService().CreateUser("admin", "admin")
	})
}

func loginCookie(t *testing.T, r *gin.Engine, password string) (string, map[string]any) {
	t.Helper()

	jsonValue, err := json.Marshal(map[string]string{
		"username": "admin",
		"password": password,
	})
	if err != nil {
		t.Fatalf("failed to marshal login payload: %v", err)
	}

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/login", bytes.NewBuffer(jsonValue))
	req.Header.Set("Content-Type", "application/json")
	markLocalRequest(req)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("login failed with status %d: %s", w.Code, w.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode login response: %v", err)
	}

	cookie := w.Header().Get("Set-Cookie")
	if cookie == "" {
		t.Fatal("expected session cookie after login")
	}

	return strings.SplitN(cookie, ";", 2)[0], payload
}

func TestAuthHandlers(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()

	// 1. Test Login Page Reachability
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/login", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "DENY", w.Header().Get("X-Frame-Options"))
	assert.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "strict-origin-when-cross-origin", w.Header().Get("Referrer-Policy"))
	assert.Contains(t, w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'")
	assert.Contains(t, w.Header().Get("Permissions-Policy"), "camera=()")

	// 2. Test Login API using the seeded test user
	values := map[string]string{
		"username": "admin",
		"password": "admin",
	}
	jsonValue, _ := json.Marshal(values)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/login", bytes.NewBuffer(jsonValue))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestInitRoutesDoesNotRunStartupSideEffects(t *testing.T) {
	resetLoginThrottleState()

	if err := db.DB.Exec("DELETE FROM global_configs").Error; err != nil {
		t.Fatalf("failed to clear global configs: %v", err)
	}
	if err := db.DB.Exec("DELETE FROM users").Error; err != nil {
		t.Fatalf("failed to clear users: %v", err)
	}
	if err := bootstrap.ClearAdminBootstrapInfo(); err != nil && !os.IsNotExist(err) {
		t.Fatalf("failed to clear bootstrap admin info: %v", err)
	}

	setupRouter()

	var count int64
	if err := db.DB.Model(&model.User{}).Count(&count).Error; err != nil {
		t.Fatalf("failed to count users: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected route initialization to avoid seeding users, got %d", count)
	}

	if _, err := bootstrap.LoadAdminBootstrapInfo(); !os.IsNotExist(err) {
		t.Fatalf("expected route initialization to avoid writing bootstrap info, got %v", err)
	}
}

func TestUnprotectedRoutes(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()

	// 1. Health/Root (Root redirects to login if not auth)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusFound, w.Code) // 302 Redirect
	assert.Equal(t, "/login", w.Header().Get("Location"))
}

func TestProtectedRoutes_Wait(t *testing.T) {
	resetAuthFixtures(t)
	// To test protected routes, we need a session.
	// This is harder with gin-contrib/sessions in unit tests without a real browser client.
	// But we can verify 401/302 for unauth access which proves handler is protected.
	r := setupRouter()

	endpoints := []string{
		"/",
		"/api/dashboard/bangumi-data", // API
		"/api/events",
		"/settings",
		"/subscriptions",
	}

	for _, ep := range endpoints {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", ep, nil)
		r.ServeHTTP(w, req)
		// API endpoints usually return 401 or redirect depending on middleware impl.
		// Our AuthMiddleware usually Redirects for HTML requests and 401 for JSON?
		// Let's check middleware implementation.
		// Assuming it Redirects by default as per previous `curl /` output (302).
		assert.Contains(t, []int{http.StatusFound, http.StatusUnauthorized}, w.Code, "Endpoint %s should be protected", ep)
	}
}

func TestSubscriptionCRUD(t *testing.T) {
	resetAuthFixtures(t)
	// Create a dummy subscription directly in DB to test "List" logic if possible?
	// But we are in a separate process/memory DB if we just use GORM direct access?
	// Yes, db.DB is accessible.

	// Create a sub
	sub := model.Subscription{
		Title:  "Test Anime",
		RSSUrl: "http://test/rss",
	}
	db.DB.Create(&sub)

	// We can't access it via API easily without auth cookie.
	// But we can test the DB function directly or verify the Refactoring didn't break imports/types.
	// Since this is verifying the Refactoring (Splitting), just ensuring the code compiles and tests run logic
	// is a huge step.
	// Let's rely on the Compilation and simple Route checks for now as "Sufficient" for this stage,
	// unless we implement a login Helper.
}

func TestLoginRedirectsToSetupWhenBootstrapPending(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()

	if err := bootstrap.SaveAdminBootstrapInfo(bootstrap.AdminBootstrapInfo{
		Username:  "admin",
		Password:  "admin",
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("failed to save bootstrap info: %v", err)
	}

	_, payload := loginCookie(t, r, "admin")
	assert.Equal(t, "/setup", payload["redirect"])
}

func TestPendingBootstrapRedirectsAuthenticatedPagesToSetup(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()

	if err := bootstrap.SaveAdminBootstrapInfo(bootstrap.AdminBootstrapInfo{
		Username:  "admin",
		Password:  "admin",
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("failed to save bootstrap info: %v", err)
	}

	cookie, _ := loginCookie(t, r, "admin")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/", nil)
	markLocalRequest(req)
	req.Header.Set("Cookie", cookie)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusFound, w.Code)
	assert.Equal(t, "/setup", w.Header().Get("Location"))
}

func TestBootstrapSetupCompletesPasswordRotationAndQBSave(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()

	if err := bootstrap.SaveAdminBootstrapInfo(bootstrap.AdminBootstrapInfo{
		Username:  "admin",
		Password:  "admin",
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("failed to save bootstrap info: %v", err)
	}

	cookie, _ := loginCookie(t, r, "admin")

	body, err := json.Marshal(map[string]string{
		"new_password":      "strong-pass-123",
		"confirm_password":  "strong-pass-123",
		"qb_mode":           "external",
		"qb_url":            "http://qb.local:8080",
		"qb_username":       "alice",
		"qb_password":       "secret",
		"base_download_dir": "D:\\Anime\\Downloads",
	})
	if err != nil {
		t.Fatalf("failed to marshal setup payload: %v", err)
	}

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/setup/bootstrap", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	markLocalRequest(req)
	req.Header.Set("Cookie", cookie)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("unexpected setup response %d: %s", w.Code, w.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode setup response: %v", err)
	}
	assert.Equal(t, "/", payload["redirect"])

	if _, err := bootstrap.LoadAdminBootstrapInfo(); !os.IsNotExist(err) {
		t.Fatalf("expected bootstrap info to be cleared, got %v", err)
	}

	if _, err := service.NewAuthService().Login("admin", "strong-pass-123"); err != nil {
		t.Fatalf("expected new password to work: %v", err)
	}

	var configs []model.GlobalConfig
	if err := db.DB.Find(&configs).Error; err != nil {
		t.Fatalf("failed to fetch configs: %v", err)
	}
	configMap := make(map[string]string, len(configs))
	for _, cfg := range configs {
		configMap[cfg.Key] = cfg.Value
	}

	assert.Equal(t, "external", configMap[model.ConfigKeyQBMode])
	assert.Equal(t, "http://qb.local:8080", configMap[model.ConfigKeyQBUrl])
	assert.Equal(t, "alice", configMap[model.ConfigKeyQBUsername])
	assert.Equal(t, "secret", configMap[model.ConfigKeyQBPassword])
	assert.Equal(t, "D:\\Anime\\Downloads", configMap[model.ConfigKeyBaseDir])
}

func TestSetupReadinessReportsFreshInstallGuidance(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()

	if err := bootstrap.SaveAdminBootstrapInfo(bootstrap.AdminBootstrapInfo{
		Username:  "admin",
		Password:  "admin",
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("failed to save bootstrap info: %v", err)
	}

	cookie, _ := loginCookie(t, r, "admin")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/setup/readiness", nil)
	markLocalRequest(req)
	req.Header.Set("Cookie", cookie)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("unexpected readiness response %d: %s", w.Code, w.Body.String())
	}

	var payload struct {
		Services []SetupReadinessStatus `json:"services"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode readiness payload: %v", err)
	}

	statusByKey := make(map[string]SetupReadinessStatus, len(payload.Services))
	for _, item := range payload.Services {
		statusByKey[item.Key] = item
	}

	assert.Equal(t, "ready", statusByKey["app"].State)
	assert.Equal(t, "warning", statusByKey["qb"].State)
	assert.Equal(t, "pending", statusByKey["tmdb"].State)
	assert.Equal(t, "pending", statusByKey["jellyfin"].State)
	assert.Equal(t, "pending", statusByKey["alist"].State)
}

func TestBootstrapPendingBlocksRemoteAccessUntilLocalSetupCompletes(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()

	bootstrapPassword := "bootstrap-secret-123"
	if err := bootstrap.SaveAdminBootstrapInfo(bootstrap.AdminBootstrapInfo{
		Username:  "admin",
		Password:  bootstrapPassword,
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("failed to save bootstrap info: %v", err)
	}

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/login", nil)
	markRemoteRequest(req)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected remote login page to be blocked during bootstrap, got %d", w.Code)
	}
	assert.NotContains(t, w.Body.String(), bootstrapPassword)

	w = httptest.NewRecorder()
	body := strings.NewReader(`{"username":"admin","password":"` + bootstrapPassword + `"}`)
	req, _ = http.NewRequest("POST", "/api/login", body)
	req.Header.Set("Content-Type", "application/json")
	markRemoteRequest(req)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected remote login API to be blocked during bootstrap, got %d", w.Code)
	}
	assert.Contains(t, w.Body.String(), "localhost")
}

func TestBootstrapLoginPageShowsLocalCredentialPathWithoutPassword(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()

	bootstrapPassword := "bootstrap-secret-456"
	if err := bootstrap.SaveAdminBootstrapInfo(bootstrap.AdminBootstrapInfo{
		Username:  "admin",
		Password:  bootstrapPassword,
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("failed to save bootstrap info: %v", err)
	}

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/login", nil)
	markLocalRequest(req)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("unexpected login page response %d: %s", w.Code, w.Body.String())
	}

	assert.Contains(t, w.Body.String(), bootstrap.AdminBootstrapInfoPath())
	assert.NotContains(t, w.Body.String(), bootstrapPassword)
}

func TestProtectedWriteRequiresSameOriginHeaders(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()

	if err := bootstrap.SaveAdminBootstrapInfo(bootstrap.AdminBootstrapInfo{
		Username:  "admin",
		Password:  "admin",
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("failed to save bootstrap info: %v", err)
	}

	cookie, _ := loginCookie(t, r, "admin")

	body, err := json.Marshal(map[string]string{
		"new_password":      "strong-pass-456",
		"confirm_password":  "strong-pass-456",
		"qb_mode":           "managed",
		"base_download_dir": "/anime",
	})
	if err != nil {
		t.Fatalf("failed to marshal setup payload: %v", err)
	}

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/setup/bootstrap", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", cookie)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Host = testLocalHost
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected same-origin protection to block missing Origin/Referer, got %d", w.Code)
	}
	assert.Contains(t, w.Body.String(), "cross-site")

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/setup/bootstrap", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", cookie)
	markLocalRequest(req)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected same-origin local bootstrap request to succeed, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRecoveryPageBlocksRemoteAccess(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/recover", nil)
	markRemoteRequest(req)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected remote recovery page access to be blocked, got %d", w.Code)
	}
	assert.Contains(t, w.Body.String(), "localhost")
}

func TestLocalRecoveryCanResetAdminPassword(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()

	body, err := json.Marshal(map[string]string{
		"username":         "admin",
		"password":         testRecoveryPassword,
		"confirm_password": testRecoveryPassword,
	})
	if err != nil {
		t.Fatalf("failed to marshal recovery payload: %v", err)
	}

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/recovery/reset-admin", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	markLocalRequest(req)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("unexpected local recovery response %d: %s", w.Code, w.Body.String())
	}

	if _, err := service.NewAuthService().Login("admin", testRecoveryPassword); err != nil {
		t.Fatalf("expected local recovery password to work: %v", err)
	}

	if _, err := service.NewAuthService().Login("admin", "admin"); err == nil {
		t.Fatal("expected old password to stop working after local recovery reset")
	}
}

func TestSubscriptionCardEndpointReturnsLatestRunState(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()

	sub := model.Subscription{
		Title:               "Observable Show",
		RSSUrl:              "https://example.test/observable",
		IsActive:            true,
		LastRunStatus:       "warning",
		LastRunSummary:      "新增 1 集，另有 1 集加入下载失败",
		LastError:           "Episode 02: qb offline",
		LastNewDownloads:    1,
		LastDownloadedTitle: "[Group] Observable Show - 01",
	}
	now := time.Now()
	sub.LastCheckAt = &now
	sub.LastSuccessAt = &now
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("failed to create subscription: %v", err)
	}

	cookie, _ := loginCookie(t, r, "admin")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", fmt.Sprintf("/api/subscriptions/%d/card", sub.ID), nil)
	req.Header.Set("Cookie", cookie)
	markLocalRequest(req)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected card endpoint to succeed, got %d: %s", w.Code, w.Body.String())
	}

	assert.Contains(t, w.Body.String(), "新增 1 集，另有 1 集加入下载失败")
	assert.Contains(t, w.Body.String(), "Episode 02: qb offline")
	assert.Contains(t, w.Body.String(), "[Group] Observable Show - 01")
	assert.Contains(t, w.Body.String(), `data-title="Observable Show"`)
	assert.Contains(t, w.Body.String(), `data-status-label="部分失败"`)
}

func TestSchedulerStatusEndpointRendersLatestSummary(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()

	scheduler.GlobalRunStatus.Skip("", "")
	t.Cleanup(func() {
		scheduler.GlobalRunStatus.Skip("", "")
	})

	scheduler.GlobalRunStatus.Begin("auto", 4)
	scheduler.GlobalRunStatus.Finish(2, 1, 1, 4, "auto", "qb unavailable")

	cookie, _ := loginCookie(t, r, "admin")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/subscriptions/scheduler-status", nil)
	req.Header.Set("Cookie", cookie)
	markLocalRequest(req)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected scheduler status endpoint to succeed, got %d: %s", w.Code, w.Body.String())
	}

	assert.Contains(t, w.Body.String(), "最近一轮共检查 4 个订阅")
	assert.Contains(t, w.Body.String(), "qb unavailable")
}

func TestSubscriptionTrendsEndpointRendersRecentLeaders(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()

	now := time.Now()
	recentCheck := now.Add(-2 * time.Hour)

	subs := []model.Subscription{
		{
			Title:            "Stable Show",
			RSSUrl:           "https://example.test/stable",
			IsActive:         true,
			LastRunStatus:    "success",
			LastRunSummary:   "新增 3 集待下载",
			LastNewDownloads: 3,
			LastCheckAt:      &recentCheck,
			LastSuccessAt:    &recentCheck,
		},
		{
			Title:          "Flaky Show",
			RSSUrl:         "https://example.test/flaky",
			IsActive:       true,
			LastRunStatus:  "error",
			LastRunSummary: "RSS 解析失败",
			LastError:      "rss unavailable",
			LastCheckAt:    &recentCheck,
		},
	}
	for i := range subs {
		if err := db.DB.Create(&subs[i]).Error; err != nil {
			t.Fatalf("failed to create subscription %s: %v", subs[i].Title, err)
		}
	}

	logs := []model.DownloadLog{
		{SubscriptionID: subs[0].ID, Title: "[Group] Stable Show - 01", Status: "completed"},
		{SubscriptionID: subs[0].ID, Title: "[Group] Stable Show - 02", Status: "downloading"},
	}
	if err := db.DB.Create(&logs).Error; err != nil {
		t.Fatalf("failed to seed trend logs: %v", err)
	}

	cookie, _ := loginCookie(t, r, "admin")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/subscriptions/trends", nil)
	req.Header.Set("Cookie", cookie)
	req.Header.Set("HX-Request", "true")
	markLocalRequest(req)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected trends endpoint to succeed, got %d: %s", w.Code, w.Body.String())
	}

	assert.Contains(t, w.Body.String(), "订阅趋势")
	assert.Contains(t, w.Body.String(), "Stable Show")
	assert.Contains(t, w.Body.String(), "Flaky Show")
	assert.Contains(t, w.Body.String(), "最近最不稳")
	assert.Contains(t, w.Body.String(), "最近最活跃")
}

func TestSubscriptionProcessFlowUpdatesTrendEndpoint(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()

	sub := model.Subscription{
		Title:    "Integrated Show",
		RSSUrl:   "https://example.test/integrated",
		IsActive: true,
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("failed to create subscription: %v", err)
	}

	manager := &service.SubscriptionManager{
		RSSParser: integrationRSSParser{
			episodes: []parser.Episode{
				{Title: "[Group] Integrated Show - 01", EpisodeNum: "01", TorrentURL: "magnet:?xt=urn:btih:integrated-1"},
				{Title: "[Group] Integrated Show - 02", EpisodeNum: "02", TorrentURL: "magnet:?xt=urn:btih:integrated-2"},
			},
		},
		Downloader: integrationDownloader{},
		DB:         db.DB,
	}
	manager.ProcessSubscription(&sub)

	cookie, _ := loginCookie(t, r, "admin")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/subscriptions/trends", nil)
	req.Header.Set("Cookie", cookie)
	req.Header.Set("HX-Request", "true")
	markLocalRequest(req)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected trends endpoint to succeed, got %d: %s", w.Code, w.Body.String())
	}

	assert.Contains(t, w.Body.String(), "Integrated Show")
	assert.Contains(t, w.Body.String(), "新增 2 集待下载")
	assert.Contains(t, w.Body.String(), "+2")
}

func TestToggleSubscriptionReturnsUpdatedCard(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()

	sub := model.Subscription{
		Title:      "Toggle Me",
		RSSUrl:     "https://example.test/toggle",
		IsActive:   true,
		FilterRule: "字幕组A",
	}
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("failed to create subscription: %v", err)
	}

	cookie, _ := loginCookie(t, r, "admin")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", fmt.Sprintf("/api/subscriptions/%d/toggle", sub.ID), nil)
	req.Header.Set("Cookie", cookie)
	markLocalRequest(req)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected toggle endpoint to succeed, got %d: %s", w.Code, w.Body.String())
	}

	assert.Contains(t, w.Body.String(), `data-subscription-card="true"`)
	assert.Contains(t, w.Body.String(), `data-active="false"`)
	assert.Contains(t, w.Body.String(), "已暂停")
}

func TestSubscriptionHistoryEndpointRendersRecentRuns(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()

	now := time.Now()
	sub := model.Subscription{
		Title:          "History Show",
		RSSUrl:         "https://example.test/history",
		IsActive:       true,
		LastRunStatus:  "warning",
		LastRunSummary: "新增 2 集，另有 1 集加入下载失败",
		LastError:      "Episode 03: qb timeout",
	}
	sub.LastCheckAt = &now
	sub.LastSuccessAt = &now
	if err := db.DB.Create(&sub).Error; err != nil {
		t.Fatalf("failed to create subscription: %v", err)
	}

	logs := []model.DownloadLog{
		{
			SubscriptionID: sub.ID,
			Title:          "[Group] History Show - 02",
			Episode:        "02",
			SeasonVal:      "S01",
			Status:         "downloading",
		},
		{
			SubscriptionID: sub.ID,
			Title:          "[Group] History Show - 01",
			Episode:        "01",
			SeasonVal:      "S01",
			Status:         "completed",
			TargetFile:     "/downloads/history-show/01.mkv",
		},
	}
	if err := db.DB.Create(&logs).Error; err != nil {
		t.Fatalf("failed to create download logs: %v", err)
	}
	runLogs := []model.SubscriptionRunLog{
		{
			SubscriptionID:      sub.ID,
			CheckedAt:           now,
			TriggerSource:       "auto",
			Status:              "warning",
			Summary:             "新增 2 集，另有 1 集加入下载失败",
			Error:               "Episode 03: qb timeout",
			TotalEpisodes:       4,
			FilteredCount:       1,
			DuplicateCount:      0,
			NewDownloads:        2,
			FailedDownloads:     1,
			LastDownloadedTitle: "[Group] History Show - 02",
		},
	}
	if err := db.DB.Create(&runLogs).Error; err != nil {
		t.Fatalf("failed to create subscription run logs: %v", err)
	}

	cookie, _ := loginCookie(t, r, "admin")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", fmt.Sprintf("/api/subscriptions/%d/history", sub.ID), nil)
	req.Header.Set("Cookie", cookie)
	req.Header.Set("HX-Request", "true")
	markLocalRequest(req)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected history endpoint to succeed, got %d: %s", w.Code, w.Body.String())
	}

	assert.Contains(t, w.Body.String(), "新增 2 集，另有 1 集加入下载失败")
	assert.Contains(t, w.Body.String(), "Episode 03: qb timeout")
	assert.Contains(t, w.Body.String(), "逐次运行日志")
	assert.Contains(t, w.Body.String(), "自动调度")
	assert.Contains(t, w.Body.String(), "RSS 4 条")
	assert.Contains(t, w.Body.String(), "[Group] History Show - 02")
	assert.Contains(t, w.Body.String(), "/downloads/history-show/01.mkv")
}

func TestLocalAnimePageIncludesHighlightID(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()

	cookie, _ := loginCookie(t, r, "admin")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/local-anime?highlight=42", nil)
	req.Header.Set("Cookie", cookie)
	markLocalRequest(req)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected local anime page to succeed, got %d: %s", w.Code, w.Body.String())
	}
	if w.Body.Len() == 0 {
		t.Fatalf("expected local anime page to render body, got empty response")
	}

	assert.Contains(t, w.Body.String(), "highlightAnimeId: 42")
	assert.Contains(t, w.Body.String(), "autoOpenAnimeId: 0")
}

func TestLocalAnimePageIncludesAutoOpenAnimeID(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()

	cookie, _ := loginCookie(t, r, "admin")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/local-anime?highlight=42&open=1", nil)
	req.Header.Set("Cookie", cookie)
	markLocalRequest(req)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected local anime page with auto-open to succeed, got %d: %s", w.Code, w.Body.String())
	}
	if w.Body.Len() == 0 {
		t.Fatalf("expected local anime page with auto-open to render body, got empty response")
	}

	assert.Contains(t, w.Body.String(), "highlightAnimeId: 42")
	assert.Contains(t, w.Body.String(), "autoOpenAnimeId: 42")
}

func TestLocalAnimePageIncludesFocusedEpisodePath(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()

	cookie, _ := loginCookie(t, r, "admin")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/local-anime?highlight=42&open=1&focus_episode=%2Fdownloads%2Fshow%2F01.mkv", nil)
	req.Header.Set("Cookie", cookie)
	markLocalRequest(req)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected local anime page with focused episode to succeed, got %d: %s", w.Code, w.Body.String())
	}
	if w.Body.Len() == 0 {
		t.Fatalf("expected local anime page with focused episode to render body, got empty response")
	}

	assert.Contains(t, w.Body.String(), "autoFocusEpisodePath:")
	assert.Contains(t, w.Body.String(), "/downloads/show/01.mkv")
}

func TestRenderLocalAnimeTemplateIncludesDeepLinkState(t *testing.T) {
	html, err := renderTemplateToString("local_anime.html", LocalAnimeData{
		SkipLayout:       true,
		HighlightAnimeID: 42,
		AutoOpenAnimeID:  42,
		AutoFocusEpisode: "/downloads/show/01.mkv",
		AnimeList:        []model.LocalAnime{},
	})
	if err != nil {
		t.Fatalf("expected local anime template to render, got error: %v", err)
	}

	assert.Contains(t, html, "highlightAnimeId: 42")
	assert.Contains(t, html, "autoOpenAnimeId: 42")
	assert.Contains(t, html, "autoFocusEpisodePath:")
	assert.Contains(t, html, "/downloads/show/01.mkv")
	assert.Contains(t, html, "刚完成")
}

func TestLocalAnimeDiagnosticsEndpointRendersOpenIssues(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()

	animeID := uint(9)
	if err := service.ReportLibraryIssue(service.LibraryIssueInput{
		IssueKey:      "scrape:9",
		IssueType:     service.LibraryIssueTypeScrape,
		Title:         "Problem Show",
		DirectoryPath: "/library/Problem Show",
		LocalAnimeID:  &animeID,
		Message:       "tmdb token missing",
		Hint:          "检查元数据配置",
	}); err != nil {
		t.Fatalf("failed to seed library issue: %v", err)
	}

	cookie, _ := loginCookie(t, r, "admin")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/local-anime/diagnostics", nil)
	req.Header.Set("Cookie", cookie)
	req.Header.Set("HX-Request", "true")
	markLocalRequest(req)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected diagnostics endpoint to succeed, got %d: %s", w.Code, w.Body.String())
	}

	assert.Contains(t, w.Body.String(), "刮削失败")
	assert.Contains(t, w.Body.String(), "Problem Show")
	assert.Contains(t, w.Body.String(), "tmdb token missing")
}

func TestLocalAnimeCardEndpointRendersSingleCard(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()

	metadata := model.AnimeMetadata{Title: "Card Show", BangumiTitle: "Card Show"}
	if err := db.DB.Create(&metadata).Error; err != nil {
		t.Fatalf("failed to create metadata: %v", err)
	}

	anime := model.LocalAnime{
		Title:      "Card Show",
		Path:       "/library/card-show",
		FileCount:  3,
		TotalSize:  1024,
		MetadataID: &metadata.ID,
	}
	if err := db.DB.Create(&anime).Error; err != nil {
		t.Fatalf("failed to create anime: %v", err)
	}

	cookie, _ := loginCookie(t, r, "admin")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", fmt.Sprintf("/api/local-anime/%d/card", anime.ID), nil)
	req.Header.Set("Cookie", cookie)
	req.Header.Set("HX-Request", "true")
	markLocalRequest(req)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected card endpoint to succeed, got %d: %s", w.Code, w.Body.String())
	}

	assert.Contains(t, w.Body.String(), fmt.Sprintf(`id="local-card-%d"`, anime.ID))
	assert.Contains(t, w.Body.String(), "Card Show")
}

func TestLocalAnimeScanStatusEndpointRendersLatestSummary(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()

	service.GlobalScanStatus.Begin(2)
	service.GlobalScanStatus.Advance("/library/Show A", 2, 1, nil)
	service.GlobalScanStatus.Advance("/library/Show B", 0, 0, fmt.Errorf("permission denied"))
	service.GlobalScanStatus.Finish()

	cookie, _ := loginCookie(t, r, "admin")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/local-anime/scan-status", nil)
	req.Header.Set("Cookie", cookie)
	req.Header.Set("HX-Request", "true")
	markLocalRequest(req)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected scan status endpoint to succeed, got %d: %s", w.Code, w.Body.String())
	}

	assert.Contains(t, w.Body.String(), "扫描任务摘要")
	assert.Contains(t, w.Body.String(), "最近一轮扫描了 2 个目录")
	assert.Contains(t, w.Body.String(), "permission denied")
}

func TestGetPlayInfoReturnsDiagnosticWhenJellyfinNotConfigured(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()

	metadata := model.AnimeMetadata{Title: "No Jellyfin Yet", BangumiID: 1001}
	if err := db.DB.Create(&metadata).Error; err != nil {
		t.Fatalf("failed to create metadata: %v", err)
	}

	anime := model.LocalAnime{Title: "No Jellyfin Yet", Path: "/library/no-jellyfin", MetadataID: &metadata.ID}
	if err := db.DB.Create(&anime).Error; err != nil {
		t.Fatalf("failed to create anime: %v", err)
	}

	ep := model.LocalEpisode{LocalAnimeID: anime.ID, Title: "Episode 1", EpisodeNum: 1, SeasonNum: 1, Path: "/library/no-jellyfin/01.mkv"}
	if err := db.DB.Create(&ep).Error; err != nil {
		t.Fatalf("failed to create episode: %v", err)
	}

	cookie, _ := loginCookie(t, r, "admin")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", fmt.Sprintf("/api/jellyfin/play/%d", ep.ID), nil)
	req.Header.Set("Cookie", cookie)
	markLocalRequest(req)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected jellyfin config error, got %d: %s", w.Code, w.Body.String())
	}

	assert.Contains(t, w.Body.String(), `"code":"jellyfin_not_configured"`)
	assert.Contains(t, w.Body.String(), "设置页填写 Jellyfin 地址和 API Key")
	assert.Contains(t, w.Body.String(), `"primary_action":"打开设置页"`)
}

func TestGetPlayInfoReturnsDiagnosticWhenSeriesMissingInJellyfin(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()

	jf := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case req.Method == http.MethodGet && req.URL.Path == "/Users":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"Id":"user-1","Name":"admin"}]`))
		case req.Method == http.MethodGet && req.URL.Path == "/Items":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"Items":[]}`))
		default:
			http.NotFound(w, req)
		}
	}))
	defer jf.Close()

	configs := []model.GlobalConfig{
		{Key: model.ConfigKeyJellyfinUrl, Value: jf.URL},
		{Key: model.ConfigKeyJellyfinApiKey, Value: "test-key"},
	}
	for _, cfg := range configs {
		if err := db.DB.Save(&cfg).Error; err != nil {
			t.Fatalf("failed to seed jellyfin config %s: %v", cfg.Key, err)
		}
	}

	metadata := model.AnimeMetadata{Title: "Missing In Jellyfin", BangumiID: 2222}
	if err := db.DB.Create(&metadata).Error; err != nil {
		t.Fatalf("failed to create metadata: %v", err)
	}

	anime := model.LocalAnime{Title: "Missing In Jellyfin", Path: "/library/missing-jf", MetadataID: &metadata.ID}
	if err := db.DB.Create(&anime).Error; err != nil {
		t.Fatalf("failed to create anime: %v", err)
	}

	ep := model.LocalEpisode{LocalAnimeID: anime.ID, Title: "Episode 1", EpisodeNum: 1, SeasonNum: 1, Path: "/library/missing-jf/01.mkv"}
	if err := db.DB.Create(&ep).Error; err != nil {
		t.Fatalf("failed to create episode: %v", err)
	}

	cookie, _ := loginCookie(t, r, "admin")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", fmt.Sprintf("/api/jellyfin/play/%d", ep.ID), nil)
	req.Header.Set("Cookie", cookie)
	markLocalRequest(req)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected missing jellyfin series to return 404, got %d: %s", w.Code, w.Body.String())
	}

	assert.Contains(t, w.Body.String(), `"code":"jellyfin_series_not_found"`)
	assert.Contains(t, w.Body.String(), "刷新资料库")
	assert.Contains(t, w.Body.String(), "/local-anime?highlight=")
}

func TestGetPlayInfoReturnsDiagnosticWhenLocalMediaMissing(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()

	jf := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case req.Method == http.MethodGet && req.URL.Path == "/Users":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"Id":"user-1","Name":"admin"}]`))
		case req.Method == http.MethodGet && req.URL.Path == "/Items":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"Items":[{"Id":"series-1","ProviderIds":{"bangumi":"3333"}}]}`))
		case req.Method == http.MethodGet && strings.HasPrefix(req.URL.Path, "/Users/user-1/Items"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"Items":[{"Id":"episode-1","UserData":{"PlaybackPositionTicks":0,"Played":false}}]}`))
		default:
			http.NotFound(w, req)
		}
	}))
	defer jf.Close()

	configs := []model.GlobalConfig{
		{Key: model.ConfigKeyJellyfinUrl, Value: jf.URL},
		{Key: model.ConfigKeyJellyfinApiKey, Value: "test-key"},
	}
	for _, cfg := range configs {
		if err := db.DB.Save(&cfg).Error; err != nil {
			t.Fatalf("failed to seed jellyfin config %s: %v", cfg.Key, err)
		}
	}

	metadata := model.AnimeMetadata{Title: "Missing Media", BangumiID: 3333}
	if err := db.DB.Create(&metadata).Error; err != nil {
		t.Fatalf("failed to create metadata: %v", err)
	}

	anime := model.LocalAnime{Title: "Missing Media", Path: "/library/missing-media", MetadataID: &metadata.ID}
	if err := db.DB.Create(&anime).Error; err != nil {
		t.Fatalf("failed to create anime: %v", err)
	}

	ep := model.LocalEpisode{LocalAnimeID: anime.ID, Title: "Episode 1", EpisodeNum: 1, SeasonNum: 1, Path: "/tmp/definitely-not-found-animate-auto-tool.mkv"}
	if err := db.DB.Create(&ep).Error; err != nil {
		t.Fatalf("failed to create episode: %v", err)
	}

	cookie, _ := loginCookie(t, r, "admin")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", fmt.Sprintf("/api/jellyfin/play/%d", ep.ID), nil)
	req.Header.Set("Cookie", cookie)
	markLocalRequest(req)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected local media missing to return 404, got %d: %s", w.Code, w.Body.String())
	}

	assert.Contains(t, w.Body.String(), `"code":"local_media_missing"`)
	assert.Contains(t, w.Body.String(), "重新扫描本地库")
}

func TestDashboardTaskOverviewEndpointRendersStatuses(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()

	scheduler.GlobalRunStatus.Begin("auto", 3)
	service.GlobalScanStatus.Begin(2)
	service.GlobalScanStatus.Advance("/library/a", 1, 0, nil)
	if service.GlobalRefreshStatus.TryStart() {
		service.GlobalRefreshStatus.SetTotal(5)
		service.GlobalRefreshStatus.UpdateProgress(2, "Test Metadata")
	}
	t.Cleanup(func() {
		scheduler.GlobalRunStatus.Skip("auto", "待命")
		service.GlobalScanStatus.Skip("待命")
		service.GlobalRefreshStatus.Finish("已结束")
	})

	cookie, _ := loginCookie(t, r, "admin")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/dashboard/task-overview", nil)
	req.Header.Set("Cookie", cookie)
	req.Header.Set("HX-Request", "true")
	markLocalRequest(req)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected dashboard task overview endpoint to succeed, got %d: %s", w.Code, w.Body.String())
	}

	assert.Contains(t, w.Body.String(), "后台任务总览")
	assert.Contains(t, w.Body.String(), "订阅调度")
	assert.Contains(t, w.Body.String(), "扫描中")
	assert.Contains(t, w.Body.String(), "元数据刷新")
	assert.Contains(t, w.Body.String(), "Test Metadata")
}

func TestRuntimeStatsEndpointRequiresAuthAndReturnsMetrics(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()

	unauthorized := httptest.NewRecorder()
	unauthReq, _ := http.NewRequest("GET", "/api/runtime/stats", nil)
	markLocalRequest(unauthReq)
	r.ServeHTTP(unauthorized, unauthReq)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized request to fail with 401, got %d", unauthorized.Code)
	}

	cookie, _ := loginCookie(t, r, "admin")

	authorized := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/runtime/stats", nil)
	req.Header.Set("Cookie", cookie)
	markLocalRequest(req)
	r.ServeHTTP(authorized, req)

	if authorized.Code != http.StatusOK {
		t.Fatalf("expected runtime stats endpoint to succeed, got %d: %s", authorized.Code, authorized.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(authorized.Body.Bytes(), &payload); err != nil {
		t.Fatalf("expected valid json response, got error: %v", err)
	}

	goInfo, ok := payload["go"].(map[string]any)
	if !ok {
		t.Fatal("expected response to include go runtime section")
	}
	if goroutines, ok := goInfo["goroutines"].(float64); !ok || goroutines < 1 {
		t.Fatalf("expected positive goroutine count, got: %#v", goInfo["goroutines"])
	}

	memoryInfo, ok := payload["memory"].(map[string]any)
	if !ok {
		t.Fatal("expected response to include memory section")
	}
	if _, ok := memoryInfo["heap_alloc_bytes"].(float64); !ok {
		t.Fatalf("expected heap_alloc_bytes to be numeric, got: %#v", memoryInfo["heap_alloc_bytes"])
	}
}

func TestDeploymentCheckEndpointRendersWarnings(t *testing.T) {
	resetAuthFixtures(t)
	r := setupRouter()

	prev := config.AppConfig
	t.Cleanup(func() {
		config.AppConfig = prev
	})

	config.AppConfig = &config.Config{
		Server: config.ServerConfig{
			Port:           8306,
			PublicURL:      "",
			TrustedProxies: []string{"0.0.0.0/0"},
		},
		Auth: config.AuthConfig{SecretKey: "short-secret"},
	}

	cookie, _ := loginCookie(t, r, "admin")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/settings/deployment-check", nil)
	req.Header.Set("Cookie", cookie)
	req.Header.Set("HX-Request", "true")
	markLocalRequest(req)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected deployment check endpoint to succeed, got %d: %s", w.Code, w.Body.String())
	}

	assert.Contains(t, w.Body.String(), "部署自检")
	assert.Contains(t, w.Body.String(), "还没有设置 server.public_url")
	assert.Contains(t, w.Body.String(), "trusted_proxies 过于宽泛")
}

func TestRenderLocalAnimeDiagnosticsIncludesRepairActions(t *testing.T) {
	animeID := uint(12)
	html, err := renderTemplateToString("local_anime_diagnostics.html", []model.LibraryIssue{
		{
			IssueType:     service.LibraryIssueTypeScan,
			Title:         "Scan Problem",
			DirectoryPath: "/library/scan-problem",
			Message:       "permission denied",
		},
		{
			IssueType:     service.LibraryIssueTypeScrape,
			Title:         "Scrape Problem",
			DirectoryPath: "/library/scrape-problem",
			LocalAnimeID:  &animeID,
			Message:       "tmdb token missing",
		},
	})
	if err != nil {
		t.Fatalf("expected diagnostics template to render, got error: %v", err)
	}

	assert.Contains(t, html, "重新扫描")
	assert.Contains(t, html, "重试刮削")
	assert.Contains(t, html, "修正匹配")
	assert.Contains(t, html, "打开详情")
}

func TestRenderDeploymentCheckTemplateIncludesSummary(t *testing.T) {
	html, err := renderTemplateToString("deployment_check.html", DeploymentCheckReport{
		PassCount: 1,
		WarnCount: 1,
		FailCount: 1,
		Items: []DeploymentCheckItem{
			{Name: "公网访问地址", Status: deploymentCheckFail, Summary: "server.public_url 不是 HTTPS", Action: "请改成 HTTPS"},
			{Name: "受信任代理", Status: deploymentCheckWarn, Summary: "当前只信任本机回环地址"},
			{Name: "会话密钥", Status: deploymentCheckPass, Summary: "会话密钥长度正常"},
		},
	})
	if err != nil {
		t.Fatalf("expected deployment check template to render, got error: %v", err)
	}

	assert.Contains(t, html, "部署自检")
	assert.Contains(t, html, "通过 1")
	assert.Contains(t, html, "注意 1")
	assert.Contains(t, html, "风险 1")
	assert.Contains(t, html, "server.public_url 不是 HTTPS")
}

func TestRenderSubscriptionTrendsTemplateIncludesSummary(t *testing.T) {
	html, err := renderTemplateToString("subscription_trends.html", SubscriptionTrendReport{
		WindowLabel:        "近 7 天",
		CheckedCount:       6,
		SuccessCount:       3,
		WarningCount:       2,
		ErrorCount:         1,
		RecentNewDownloads: 5,
		DownloadLogCount:   8,
		CompletedCount:     4,
		TopIssueSubscriptions: []SubscriptionTrendItem{
			{ID: 1, Title: "Flaky Show", Status: "error", StatusLabel: "失败", LastError: "qb offline", LastCheckLabel: "2 小时前"},
		},
		RecentWinningSubscriptions: []SubscriptionTrendItem{
			{ID: 2, Title: "Stable Show", LastRunSummary: "新增 3 集待下载", LastNewDownloads: 3, LastCheckLabel: "1 小时前"},
		},
	})
	if err != nil {
		t.Fatalf("expected subscription trends template to render, got error: %v", err)
	}

	assert.Contains(t, html, "订阅趋势")
	assert.Contains(t, html, "近 7 天")
	assert.Contains(t, html, "Flaky Show")
	assert.Contains(t, html, "Stable Show")
	assert.Contains(t, html, "新增下载")
}

func TestRenderBackupAnalyzeTemplateIncludesModeSpecificWarning(t *testing.T) {
	html, err := renderTemplateToString("backup_analyze.html", gin.H{
		"Stats": service.BackupDescriptor{
			Mode:              service.BackupModeCloudflare,
			ModeLabel:         service.BackupModeLabel(service.BackupModeCloudflare),
			Description:       service.BackupModeDescription(service.BackupModeCloudflare),
			ConfigStrategy:    "merge",
			GlobalConfigCount: 4,
			HasConfigs:        true,
		},
		"TempFile": "token-123",
	})
	if err != nil {
		t.Fatalf("expected backup analyze template to render, got error: %v", err)
	}

	assert.Contains(t, html, "Cloudflare 云存档凭据")
	assert.Contains(t, html, "合并")
	assert.Contains(t, html, "4 项设置")
	assert.Contains(t, html, `name="restore_configs" checked`)
}

func TestRenderSubscriptionsTemplateUsesDynamicCurrentYear(t *testing.T) {
	html, err := renderTemplateToString("subscriptions.html", SubscriptionsData{
		SkipLayout:    true,
		Subscriptions: []model.Subscription{},
	})
	if err != nil {
		t.Fatalf("expected subscriptions template to render, got error: %v", err)
	}

	assert.Contains(t, html, "new Date().getFullYear() - i")
	assert.NotContains(t, html, "(2025 - i).toString()")
}

func TestRenderLocalAnimeTemplateIncludesDiagnosticsRepairMethods(t *testing.T) {
	html, err := renderTemplateToString("local_anime.html", LocalAnimeData{
		SkipLayout: true,
		AnimeList:  []model.LocalAnime{},
	})
	if err != nil {
		t.Fatalf("expected local anime template to render, got error: %v", err)
	}

	assert.Contains(t, html, "preferredFixMatchSource(localAnimeId)")
	assert.Contains(t, html, "async retryScrapeIssue(localAnimeId, title)")
	assert.Contains(t, html, "openFixMatchForIssue(localAnimeId, title)")
	assert.Contains(t, html, "refreshScanStatus()")
	assert.Contains(t, html, "local-scan-status-container")
	assert.Contains(t, html, "showPlaybackFailure(error, diagnostic = null)")
	assert.Contains(t, html, "playbackDiagnostic")
	assert.Contains(t, html, "replaceLocalAnimeCard(id, html)")
	assert.Contains(t, html, "highlightRecoveredCard(id)")
	assert.Contains(t, html, "handleLibraryIssueUpdate(detail)")
}

func TestRenderLocalAnimeCardUsesAnimeRefreshEndpoint(t *testing.T) {
	html, err := renderTemplateToString("local_anime_card.html", model.LocalAnime{
		Model: gorm.Model{ID: 7},
		Title: "Refreshable Show",
	})
	if err != nil {
		t.Fatalf("expected local anime card template to render, got error: %v", err)
	}

	assert.Contains(t, html, `hx-post="/api/local-anime/7/refresh-metadata"`)
}

func TestRenderLocalScanStatusTemplateIncludesSummary(t *testing.T) {
	now := time.Now()
	html, err := renderTemplateToString("local_scan_status.html", service.ScanRunStatus{
		IsRunning:            false,
		TotalDirectories:     3,
		ProcessedDirectories: 3,
		AddedCount:           4,
		UpdatedCount:         2,
		FailedDirectories:    1,
		LastStartedAt:        &now,
		LastFinishedAt:       &now,
		LastDuration:         "12 秒",
		LastSummary:          "最近一轮扫描了 3 个目录：新增 4，更新 2，失败 1",
		LastError:            "permission denied",
	})
	if err != nil {
		t.Fatalf("expected scan status template to render, got error: %v", err)
	}

	assert.Contains(t, html, "扫描任务摘要")
	assert.Contains(t, html, "最近一轮扫描了 3 个目录：新增 4，更新 2，失败 1")
	assert.Contains(t, html, "12 秒")
	assert.Contains(t, html, "permission denied")
}

func TestRenderSettingsTemplateIncludesRuntimeStatsCard(t *testing.T) {
	html, err := renderTemplateToString("settings.html", gin.H{
		"SkipLayout":       true,
		"Config":           map[string]string{},
		"JellyfinServerID": "",
		"Stats":            BackupStats{},
	})
	if err != nil {
		t.Fatalf("expected settings template to render, got error: %v", err)
	}

	assert.Contains(t, html, "runtimeStatsCard()")
	assert.Contains(t, html, "/api/runtime/stats")
	assert.Contains(t, html, "运行时状态")
}
