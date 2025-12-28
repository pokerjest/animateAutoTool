package config

import (
	"os"
	"testing"
)

func TestLoadConfig_Defaults(t *testing.T) {
	// Initialize with empty config
	err := LoadConfig("")
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
	if AppConfig.Database.Path != "data/animate.db" {
		t.Errorf("Expected default db path 'data/animate.db', got %s", AppConfig.Database.Path)
	}
}

func TestLoadConfig_EnvOverride(t *testing.T) {
	// Set environment variable
	os.Setenv("ANIME_SERVER_PORT", "9999")
	defer os.Unsetenv("ANIME_SERVER_PORT")

	err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if AppConfig.Server.Port != 9999 {
		t.Errorf("Expected port 9999 from env, got %d", AppConfig.Server.Port)
	}
}
