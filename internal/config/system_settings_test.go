package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestSystemSettingsMirrorPreservesConfigAndSecuresFile(t *testing.T) {
	tempRoot := t.TempDir()
	previousPaths := AppPaths
	previousConfig := AppConfig
	AppPaths = newPaths(tempRoot)
	AppConfig = &Config{}
	t.Cleanup(func() {
		AppPaths = previousPaths
		AppConfig = previousConfig
	})

	initial := []byte("# keep this comment\nserver:\n  port: 8306\nsystem_settings:\n  old_key: old-value\n")
	if err := os.WriteFile(ConfigFilePath(), initial, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	if err := UpdateSystemSettings(map[string]string{
		"qb_password": "local secret",
		"enabled":     "true",
	}); err != nil {
		t.Fatalf("UpdateSystemSettings: %v", err)
	}

	var document struct {
		Server struct {
			Port int `yaml:"port"`
		} `yaml:"server"`
		SystemSettings map[string]string `yaml:"system_settings"`
	}
	data, err := os.ReadFile(ConfigFilePath())
	if err != nil {
		t.Fatalf("read mirror: %v", err)
	}
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatalf("parse mirror: %v", err)
	}
	if document.Server.Port != 8306 {
		t.Fatalf("server config was not preserved: %#v", document.Server)
	}
	if document.SystemSettings["old_key"] != "old-value" || document.SystemSettings["qb_password"] != "local secret" || document.SystemSettings["enabled"] != "true" {
		t.Fatalf("unexpected mirrored settings: %#v", document.SystemSettings)
	}
	if AppConfig.SystemSettings["qb_password"] != "local secret" {
		t.Fatalf("in-memory settings were not refreshed: %#v", AppConfig.SystemSettings)
	}

	if runtime.GOOS != goosWindows {
		info, err := os.Stat(ConfigFilePath())
		if err != nil {
			t.Fatalf("stat mirror: %v", err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("config permissions = %o, want 600", got)
		}
	}
}

func TestReplaceSystemSettingsRemovesStaleKeys(t *testing.T) {
	tempRoot := t.TempDir()
	previousPaths := AppPaths
	previousConfig := AppConfig
	AppPaths = newPaths(tempRoot)
	AppConfig = &Config{}
	t.Cleanup(func() {
		AppPaths = previousPaths
		AppConfig = previousConfig
	})

	configData := []byte("server:\n  port: 8306\nsystem_settings:\n  stale: remove-me\n")
	if err := os.WriteFile(filepath.Join(tempRoot, "config.yaml"), configData, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if err := ReplaceSystemSettings(map[string]string{"current": "value"}); err != nil {
		t.Fatalf("ReplaceSystemSettings: %v", err)
	}

	doc, err := readConfigDocument()
	if err != nil {
		t.Fatalf("readConfigDocument: %v", err)
	}
	settings, err := settingsFromDocument(doc)
	if err != nil {
		t.Fatalf("settingsFromDocument: %v", err)
	}
	if _, exists := settings["stale"]; exists {
		t.Fatalf("stale key survived replacement: %#v", settings)
	}
	if settings["current"] != "value" {
		t.Fatalf("replacement value missing: %#v", settings)
	}
}

func TestUpdateSystemSettingsWithoutLoadedPathsIsNoop(t *testing.T) {
	previousPaths := AppPaths
	AppPaths = Paths{}
	t.Cleanup(func() { AppPaths = previousPaths })

	if err := UpdateSystemSettings(map[string]string{"key": "value"}); err != nil {
		t.Fatalf("uninitialized update should be a no-op: %v", err)
	}
}
