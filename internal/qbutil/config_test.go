package qbutil

import (
	"path/filepath"
	"testing"

	"github.com/pokerjest/animateAutoTool/internal/bootstrap"
	"github.com/pokerjest/animateAutoTool/internal/config"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
)

func withQBUtilConfigPaths(t *testing.T) {
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

func TestIsDefaultLocalURL(t *testing.T) {
	cases := []string{
		"http://localhost:8080",
		"http://localhost:8080/",
		"http://127.0.0.1:8080",
		"http://localhost:7603",
		"http://127.0.0.1:7603",
	}

	for _, raw := range cases {
		if !IsManagedLocalURL(raw) {
			t.Fatalf("expected %q to be treated as default local URL", raw)
		}
	}
}

func TestManagedBinaryMissingForManagedMode(t *testing.T) {
	cfg := Config{
		URL:  LegacyQBURL,
		Mode: ModeManaged,
	}

	if !ManagedBinaryMissing(cfg, t.TempDir()) {
		t.Fatal("expected missing managed binary to be detected for implicit default URL")
	}
}

func TestManagedBinaryMissingSkipsExternalMode(t *testing.T) {
	cfg := Config{
		URL:  "http://192.168.1.5:8080",
		Mode: ModeExternal,
	}

	if ManagedBinaryMissing(cfg, t.TempDir()) {
		t.Fatal("explicit QB URL should not be treated as missing managed binary")
	}
}

func TestMissingExternalURL(t *testing.T) {
	cfg := Config{Mode: ModeExternal}
	if !MissingExternalURL(cfg) {
		t.Fatal("expected empty external config to be flagged")
	}

	cfg.URL = "http://qb.example.com"
	if MissingExternalURL(cfg) {
		t.Fatal("expected external URL to satisfy validation")
	}
}

func TestLoadConfigUsesManagedBootstrapCredentials(t *testing.T) {
	withQBUtilConfigPaths(t)
	db.InitDB(":memory:")
	t.Cleanup(func() {
		_ = db.CloseDB()
	})

	if err := bootstrap.SaveQBCredentials(bootstrap.QBCredentials{
		URL:      "http://127.0.0.1:8080",
		Username: "admin",
		Password: "managed-secret",
	}); err != nil {
		t.Fatalf("failed to save managed qb credentials: %v", err)
	}

	if err := db.SaveGlobalConfig(model.ConfigKeyQBMode, ModeManaged); err != nil {
		t.Fatalf("failed to save qb mode: %v", err)
	}

	cfg := LoadConfig()

	if cfg.Mode != ModeManaged {
		t.Fatalf("expected managed mode, got %s", cfg.Mode)
	}
	if cfg.URL != "http://127.0.0.1:8080" {
		t.Fatalf("expected managed qb url, got %s", cfg.URL)
	}
	if cfg.Username != "admin" || cfg.Password != "managed-secret" {
		t.Fatalf("expected managed bootstrap credentials, got %+v", cfg)
	}
}
