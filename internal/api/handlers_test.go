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

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/bootstrap"
	"github.com/pokerjest/animateAutoTool/internal/config"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/parser"
	"github.com/pokerjest/animateAutoTool/internal/safeio"
	"github.com/pokerjest/animateAutoTool/internal/service"
)

// Keep this file limited to package-wide test lifecycle and shared fixtures.
// Handler scenarios belong in the domain-specific *_handlers_test.go files.

const (
	testJellyfinUsersPath = "/Users"
	testJellyfinItemsPath = "/Items"
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
	initLegacyTestRoutes(r)
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
	testRemoteAddr       = "203.0.113.25:45678"
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
	req.RemoteAddr = testRemoteAddr
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
