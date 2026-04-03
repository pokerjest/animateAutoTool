package launcher

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/pokerjest/animateAutoTool/internal/bootstrap"
	"github.com/pokerjest/animateAutoTool/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withLauncherBootstrapDir(t *testing.T) {
	t.Helper()

	tempRoot := t.TempDir()
	prevPaths := config.AppPaths
	config.AppPaths = config.Paths{
		RootDir: tempRoot,
		DataDir: filepath.Join(tempRoot, "data"),
	}
	t.Cleanup(func() {
		config.AppPaths = prevPaths
	})
}

func TestManagedJellyfinConflictReasonDetectsExistingJellyfin(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/System/Info/Public", r.URL.Path)
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"ServerName": "NAVI",
			"Version":    "10.11.5",
		}))
	}))
	server.Listener = listener
	server.Start()
	defer server.Close()

	reason := managedJellyfinConflictReason(listener.Addr().String(), server.URL)

	assert.Contains(t, reason, "NAVI 10.11.5")
	assert.Contains(t, reason, listener.Addr().String())
}

func TestManagedJellyfinConflictReasonDetectsNonJellyfinPortOccupant(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	server.Listener = listener
	server.Start()
	defer server.Close()

	reason := managedJellyfinConflictReason(listener.Addr().String(), server.URL)

	assert.Equal(t, "address "+listener.Addr().String()+" is already in use by another process", reason)
}

func TestManagedAListConflictReasonDetectsExistingAList(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/public/settings", r.URL.Path)
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"code":    200,
			"message": "success",
			"data": map[string]any{
				"site_title": "AList",
				"version":    "v3.55.0",
			},
		}))
	}))
	server.Listener = listener
	server.Start()
	defer server.Close()

	reason := managedAListConflictReason(listener.Addr().String(), server.URL)

	assert.Contains(t, reason, "AList v3.55.0")
	assert.Contains(t, reason, listener.Addr().String())
}

func TestManagedAListConflictReasonDetectsNonAListPortOccupant(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	server.Listener = listener
	server.Start()
	defer server.Close()

	reason := managedAListConflictReason(listener.Addr().String(), server.URL)

	assert.Equal(t, "address "+listener.Addr().String()+" is already in use by another process", reason)
}

func TestEnsureQBConfigCreatesAuthenticatedConfigAndBootstrapCredentials(t *testing.T) {
	withLauncherBootstrapDir(t)

	mgr := &Manager{}
	profileDir := t.TempDir()

	require.NoError(t, mgr.ensureQBConfig(profileDir))

	confPath := filepath.Join(profileDir, "qBittorrent", "config", "qBittorrent.conf")
	content, err := os.ReadFile(filepath.Clean(confPath))
	require.NoError(t, err)

	assert.Contains(t, string(content), "WebUI\\Enabled=true")
	assert.Contains(t, string(content), "WebUI\\Port=8080")
	assert.Contains(t, string(content), "WebUI\\LocalHostAuth=true")
	assert.Contains(t, string(content), "WebUI\\Username=admin")
	assert.Contains(t, string(content), "WebUI\\Password_PBKDF2=\"@ByteArray(")
	assert.NotContains(t, string(content), "WebUI\\LocalHostAuth=false")

	creds, err := bootstrap.LoadQBCredentials()
	require.NoError(t, err)
	assert.Equal(t, managedQBURL, creds.URL)
	assert.Equal(t, managedDefaultUsername, creds.Username)
	assert.NotEmpty(t, creds.Password)
}

func TestEnsureQBConfigPreservesExistingCustomConfig(t *testing.T) {
	withLauncherBootstrapDir(t)

	mgr := &Manager{}
	profileDir := t.TempDir()
	confDir := filepath.Join(profileDir, "qBittorrent", "config")
	require.NoError(t, os.MkdirAll(confDir, 0755))

	customContent := `[Preferences]
WebUI\Enabled=true
WebUI\Port=8080
WebUI\LocalHostAuth=true
WebUI\Username=custom-admin
`
	confPath := filepath.Join(confDir, "qBittorrent.conf")
	require.NoError(t, os.WriteFile(confPath, []byte(customContent), 0600))

	require.NoError(t, mgr.ensureQBConfig(profileDir))

	content, err := os.ReadFile(filepath.Clean(confPath))
	require.NoError(t, err)
	assert.Equal(t, customContent, string(content))

	_, err = bootstrap.LoadQBCredentials()
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestEnsureQBConfigMigratesLegacyManagedConfig(t *testing.T) {
	withLauncherBootstrapDir(t)

	mgr := &Manager{}
	profileDir := t.TempDir()
	confDir := filepath.Join(profileDir, "qBittorrent", "config")
	require.NoError(t, os.MkdirAll(confDir, 0755))

	confPath := filepath.Join(confDir, "qBittorrent.conf")
	require.NoError(t, os.WriteFile(confPath, []byte(legacyManagedQBConfig), 0600))

	require.NoError(t, mgr.ensureQBConfig(profileDir))

	content, err := os.ReadFile(filepath.Clean(confPath))
	require.NoError(t, err)
	assert.Contains(t, string(content), "WebUI\\LocalHostAuth=true")
	assert.Contains(t, string(content), "WebUI\\Password_PBKDF2=\"@ByteArray(")
	assert.NotContains(t, string(content), "WebUI\\LocalHostAuth=false")

	creds, err := bootstrap.LoadQBCredentials()
	require.NoError(t, err)
	assert.NotEmpty(t, creds.Password)
}

func TestEnsureQBConfigMigratesLegacyManagedConfigWithExtraLines(t *testing.T) {
	withLauncherBootstrapDir(t)

	mgr := &Manager{}
	profileDir := t.TempDir()
	confDir := filepath.Join(profileDir, "qBittorrent", "config")
	require.NoError(t, os.MkdirAll(confDir, 0755))

	legacyWithExtras := `[Preferences]
WebUI\Enabled=true
WebUI\Port=8080
WebUI\LocalHostAuth=false
Session\TempPath=/tmp/qb
`
	confPath := filepath.Join(confDir, "qBittorrent.conf")
	require.NoError(t, os.WriteFile(confPath, []byte(legacyWithExtras), 0600))

	require.NoError(t, mgr.ensureQBConfig(profileDir))

	content, err := os.ReadFile(filepath.Clean(confPath))
	require.NoError(t, err)
	assert.Contains(t, string(content), "WebUI\\LocalHostAuth=true")
	assert.Contains(t, string(content), "WebUI\\Username=admin")
	assert.Contains(t, string(content), "WebUI\\Password_PBKDF2=\"@ByteArray(")
	assert.Contains(t, string(content), "Session\\TempPath=/tmp/qb")
	assert.NotContains(t, string(content), "WebUI\\LocalHostAuth=false")
}
