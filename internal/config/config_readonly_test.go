package config

import (
	"os"
	"path/filepath"
	"testing"
)

func preserveConfigState(t *testing.T) {
	t.Helper()
	previousAppConfig := AppConfig
	previousAppPaths := AppPaths
	previousAutoCreated := ConfigAutoCreated
	previousRootOverride := appRootOverride
	previousSecretPath := authSecretFallbackPath
	previousSecretOverride := authSecretFallbackPathOverride
	previousExecutablePathFunc := executablePathFunc
	previousUserConfigDirFunc := userConfigDirFunc
	previousGOOSOverride := goosOverride
	t.Cleanup(func() {
		AppConfig = previousAppConfig
		AppPaths = previousAppPaths
		ConfigAutoCreated = previousAutoCreated
		appRootOverride = previousRootOverride
		authSecretFallbackPath = previousSecretPath
		authSecretFallbackPathOverride = previousSecretOverride
		executablePathFunc = previousExecutablePathFunc
		userConfigDirFunc = previousUserConfigDirFunc
		goosOverride = previousGOOSOverride
	})
}

func TestLoadConfigReadOnlyDoesNotCreateMissingRoot(t *testing.T) {
	preserveConfigState(t)
	root := filepath.Join(t.TempDir(), "missing", "app")
	appRootOverride = ""
	authSecretFallbackPathOverride = ""

	if err := LoadConfigReadOnly(root); err != nil {
		t.Fatalf("LoadConfigReadOnly failed: %v", err)
	}
	if ConfigAutoCreated {
		t.Fatal("read-only config load must not report an auto-created config")
	}
	if AppConfig == nil {
		t.Fatal("expected defaults to be available in memory")
	}
	if AppConfig.Auth.SecretKey != defaultAuthSecret {
		t.Fatalf("expected in-memory placeholder secret, got %q", AppConfig.Auth.SecretKey)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("read-only config load created the missing root: %v", err)
	}
}

func TestLoadConfigReadOnlyReusesFallbackSecretWithoutCreatingConfig(t *testing.T) {
	preserveConfigState(t)
	root := t.TempDir()
	secretPath := filepath.Join(root, "bootstrap", "auth_secret")
	if err := os.MkdirAll(filepath.Dir(secretPath), 0o700); err != nil {
		t.Fatalf("create secret directory: %v", err)
	}
	const secret = "existing-read-only-secret"
	if err := os.WriteFile(secretPath, []byte(secret+"\n"), 0o600); err != nil {
		t.Fatalf("write fallback secret: %v", err)
	}
	appRootOverride = ""
	authSecretFallbackPathOverride = secretPath

	if err := LoadConfigReadOnly(root); err != nil {
		t.Fatalf("LoadConfigReadOnly failed: %v", err)
	}
	if AppConfig.Auth.SecretKey != secret {
		t.Fatalf("expected fallback secret %q, got %q", secret, AppConfig.Auth.SecretKey)
	}
	if ConfigAutoCreated {
		t.Fatal("read-only config load must not report an auto-created config")
	}
	if _, err := os.Stat(filepath.Join(root, defaultConfigFileName)); !os.IsNotExist(err) {
		t.Fatalf("read-only config load created a config file: %v", err)
	}
	data, err := os.ReadFile(secretPath) //nolint:gosec // secretPath is inside t.TempDir.
	if err != nil {
		t.Fatalf("read fallback secret after load: %v", err)
	}
	if string(data) != secret+"\n" {
		t.Fatalf("read-only config load modified the fallback secret: %q", string(data))
	}
}
