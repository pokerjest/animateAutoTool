package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig_Defaults(t *testing.T) {
	tempRoot := t.TempDir()
	prevRootOverride := appRootOverride
	prevSecretOverride := authSecretFallbackPathOverride
	appRootOverride = tempRoot
	authSecretFallbackPathOverride = ""
	defer func() {
		appRootOverride = prevRootOverride
		authSecretFallbackPathOverride = prevSecretOverride
	}()

	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd failed: %v", err)
	}
	if err := os.Chdir(tempRoot); err != nil {
		t.Fatalf("Chdir failed: %v", err)
	}
	defer func() { _ = os.Chdir(prevWD) }()

	// Initialize with empty config
	err = LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if AppConfig == nil {
		t.Fatal("AppConfig is nil")
	}

	// Check defaults
	if AppConfig.Server.Port != 8306 {
		t.Errorf("Expected default port 8306, got %d", AppConfig.Server.Port)
	}
	if AppConfig.Server.Mode != "release" {
		t.Errorf("Expected default mode 'release', got %s", AppConfig.Server.Mode)
	}
	if AppConfig.Server.PublicURL != "" {
		t.Errorf("Expected empty default public URL, got %q", AppConfig.Server.PublicURL)
	}
	if len(AppConfig.Server.TrustedProxies) != 2 || AppConfig.Server.TrustedProxies[0] != "127.0.0.1" {
		t.Errorf("Expected loopback trusted proxies by default, got %#v", AppConfig.Server.TrustedProxies)
	}
	expectedDBPath := filepath.Join(tempRoot, "data", "animate.db")
	if AppConfig.Database.Path != expectedDBPath {
		t.Errorf("Expected default db path %q, got %s", expectedDBPath, AppConfig.Database.Path)
	}
	if AppConfig.Auth.SecretKey == "" || AppConfig.Auth.SecretKey == defaultAuthSecret {
		t.Errorf("Expected generated auth secret, got %q", AppConfig.Auth.SecretKey)
	}
	if AppConfig.Managed.DownloadMissing {
		t.Errorf("Expected managed sidecar auto-download to default to false")
	}
	if !ConfigAutoCreated {
		t.Fatalf("Expected default config file to be auto-created")
	}
	if _, err := os.Stat(filepath.Join(tempRoot, "config.yaml")); err != nil {
		t.Fatalf("Expected config.yaml to be auto-created, got error: %v", err)
	}
	tempSecretPath := filepath.Join(tempRoot, "data", "bootstrap", "auth_secret")
	if _, err := os.Stat(tempSecretPath); err != nil {
		t.Fatalf("Expected fallback auth secret to be persisted, got error: %v", err)
	}
}

func TestLoadConfig_EnvOverride(t *testing.T) {
	tempRoot := t.TempDir()
	prevRootOverride := appRootOverride
	prevSecretOverride := authSecretFallbackPathOverride
	appRootOverride = tempRoot
	authSecretFallbackPathOverride = ""
	defer func() {
		appRootOverride = prevRootOverride
		authSecretFallbackPathOverride = prevSecretOverride
	}()

	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd failed: %v", err)
	}
	if err := os.Chdir(tempRoot); err != nil {
		t.Fatalf("Chdir failed: %v", err)
	}
	defer func() { _ = os.Chdir(prevWD) }()

	// Set environment variable
	if err := os.Setenv("ANIME_SERVER_PORT", "9999"); err != nil {
		t.Fatalf("Setenv failed: %v", err)
	}
	defer func() {
		if err := os.Unsetenv("ANIME_SERVER_PORT"); err != nil {
			t.Fatalf("Unsetenv failed: %v", err)
		}
	}()

	err = LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if AppConfig.Server.Port != 9999 {
		t.Errorf("Expected port 9999 from env, got %d", AppConfig.Server.Port)
	}
	if !ConfigAutoCreated {
		t.Fatalf("Expected config file to be auto-created during env override test")
	}
}

func TestLoadConfig_ReusesPersistedFallbackAuthSecret(t *testing.T) {
	tempRoot := t.TempDir()
	tempSecretPath := filepath.Join(tempRoot, "auth_secret")
	prevRootOverride := appRootOverride
	prevSecretOverride := authSecretFallbackPathOverride
	appRootOverride = tempRoot
	authSecretFallbackPathOverride = tempSecretPath
	defer func() {
		appRootOverride = prevRootOverride
		authSecretFallbackPathOverride = prevSecretOverride
	}()

	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd failed: %v", err)
	}
	if err := os.Chdir(tempRoot); err != nil {
		t.Fatalf("Chdir failed: %v", err)
	}
	defer func() { _ = os.Chdir(prevWD) }()

	const existingSecret = "persisted-secret-value"
	if err := os.MkdirAll(filepath.Dir(tempSecretPath), 0o700); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(tempSecretPath, []byte(existingSecret+"\n"), 0o600); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	if err := LoadConfig(""); err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if AppConfig.Auth.SecretKey != existingSecret {
		t.Fatalf("Expected persisted auth secret %q, got %q", existingSecret, AppConfig.Auth.SecretKey)
	}
	if !ConfigAutoCreated {
		t.Fatalf("Expected config file to be auto-created when using persisted auth secret")
	}
}

func TestLoadConfig_ExplicitConfigDirCreatesFilesThere(t *testing.T) {
	tempRoot := t.TempDir()
	otherWD := t.TempDir()

	prevSecretOverride := authSecretFallbackPathOverride
	authSecretFallbackPathOverride = ""
	defer func() {
		authSecretFallbackPathOverride = prevSecretOverride
	}()

	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd failed: %v", err)
	}
	if err := os.Chdir(otherWD); err != nil {
		t.Fatalf("Chdir failed: %v", err)
	}
	defer func() { _ = os.Chdir(prevWD) }()

	if err := LoadConfig(tempRoot); err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if AppPaths.RootDir != tempRoot {
		t.Fatalf("Expected explicit root %q, got %q", tempRoot, AppPaths.RootDir)
	}
	if ConfigFilePath() != filepath.Join(tempRoot, "config.yaml") {
		t.Fatalf("Expected config file in explicit root, got %q", ConfigFilePath())
	}
	if DataDir() != filepath.Join(tempRoot, "data") {
		t.Fatalf("Expected data dir in explicit root, got %q", DataDir())
	}
	if _, err := os.Stat(filepath.Join(tempRoot, "config.yaml")); err != nil {
		t.Fatalf("Expected config file to be created in explicit root, got %v", err)
	}
}
