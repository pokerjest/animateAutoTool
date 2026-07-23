package db

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pokerjest/animateAutoTool/internal/config"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"gopkg.in/yaml.v3"
)

func TestSyncGlobalConfigsWithConfigFileImportsAndExports(t *testing.T) {
	tempRoot := t.TempDir()
	fixture := []byte(`server:
  port: 8306
database:
  path: data/test.db
auth:
  secret_key: test-secret-that-is-long-enough
system_settings:
  qb_url: http://yaml-qb:8080
  qb_password: yaml-secret
`)
	if err := os.WriteFile(filepath.Join(tempRoot, "config.yaml"), fixture, 0o600); err != nil {
		t.Fatalf("write config fixture: %v", err)
	}
	if err := config.LoadConfig(tempRoot); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	InitDB(":memory:")
	t.Cleanup(func() {
		_ = CloseDB()
		DB = nil
	})
	if err := DB.Create(&model.GlobalConfig{Key: model.ConfigKeyAIModel, Value: "db-model"}).Error; err != nil {
		t.Fatalf("seed database: %v", err)
	}

	if err := SyncGlobalConfigsWithConfigFile(); err != nil {
		t.Fatalf("SyncGlobalConfigsWithConfigFile: %v", err)
	}
	var qb model.GlobalConfig
	if err := DB.First(&qb, "key = ?", model.ConfigKeyQBUrl).Error; err != nil {
		t.Fatalf("read imported setting: %v", err)
	}
	if qb.Value != "http://yaml-qb:8080" {
		t.Fatalf("imported qb_url = %q", qb.Value)
	}

	data, err := os.ReadFile(config.ConfigFilePath())
	if err != nil {
		t.Fatalf("read exported config: %v", err)
	}
	var document struct {
		SystemSettings map[string]string `yaml:"system_settings"`
	}
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatalf("parse exported config: %v", err)
	}
	if document.SystemSettings[model.ConfigKeyAIModel] != "db-model" || document.SystemSettings[model.ConfigKeyQBPassword] != "yaml-secret" {
		t.Fatalf("unexpected exported settings: %#v", document.SystemSettings)
	}

	if err := SaveGlobalConfig(model.ConfigKeyAIModel, "new-model"); err != nil {
		t.Fatalf("SaveGlobalConfig: %v", err)
	}
	data, err = os.ReadFile(config.ConfigFilePath())
	if err != nil {
		t.Fatalf("read updated config: %v", err)
	}
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatalf("parse updated config: %v", err)
	}
	if document.SystemSettings[model.ConfigKeyAIModel] != "new-model" {
		t.Fatalf("single setting was not mirrored: %#v", document.SystemSettings)
	}
}
