package service

import (
	"strings"
	"testing"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
)

func TestCreateSettingsBackupFileIncludesOnlyGlobalConfigs(t *testing.T) {
	db.InitDB(":memory:")
	t.Cleanup(func() {
		_ = db.CloseDB()
	})

	if err := db.SaveGlobalConfig(model.ConfigKeyQBUrl, "http://localhost:8080"); err != nil {
		t.Fatalf("failed to seed config: %v", err)
	}
	if err := db.DB.Create(&model.Subscription{Title: "Test Anime", RSSUrl: "https://example.com/rss"}).Error; err != nil {
		t.Fatalf("failed to seed subscription: %v", err)
	}

	tempPath := t.TempDir() + "/settings.db"
	if err := CreateBackupFile(tempPath, BackupModeSettings); err != nil {
		t.Fatalf("CreateBackupFile failed: %v", err)
	}

	desc, err := InspectBackup(tempPath)
	if err != nil {
		t.Fatalf("InspectBackup failed: %v", err)
	}

	if desc.Mode != BackupModeSettings {
		t.Fatalf("expected settings backup mode, got %s", desc.Mode)
	}
	if !desc.HasConfigs {
		t.Fatal("expected settings backup to include configs")
	}
	if desc.HasSubscriptions || desc.HasLogs || desc.HasLocal || desc.HasUsers || desc.HasMetadata {
		t.Fatal("settings backup should not include non-settings tables")
	}
	if desc.GlobalConfigCount != 1 {
		t.Fatalf("expected 1 global config, got %d", desc.GlobalConfigCount)
	}
}

func TestCloudflareBackupRestoreMergesConfigs(t *testing.T) {
	db.InitDB(":memory:")
	t.Cleanup(func() {
		_ = db.CloseDB()
	})

	backupValues := map[string]string{
		model.ConfigKeyR2Endpoint:  "https://acct.r2.cloudflarestorage.com",
		model.ConfigKeyR2AccessKey: "ACCESS",
		model.ConfigKeyR2SecretKey: "SECRET",
		model.ConfigKeyR2Bucket:    "bucket-a",
	}
	for key, value := range backupValues {
		if err := db.SaveGlobalConfig(key, value); err != nil {
			t.Fatalf("failed to seed cloudflare config %s: %v", key, err)
		}
	}
	if err := db.SaveGlobalConfig(model.ConfigKeyQBUrl, "http://old-qb"); err != nil {
		t.Fatalf("failed to seed qb config: %v", err)
	}
	initialTMDBToken := strings.Join([]string{"tmdb", "old"}, "-")
	if err := db.SaveGlobalConfig(model.ConfigKeyTMDBToken, initialTMDBToken); err != nil {
		t.Fatalf("failed to seed tmdb config: %v", err)
	}

	tempPath := t.TempDir() + "/cloudflare.db"
	if err := CreateBackupFile(tempPath, BackupModeCloudflare); err != nil {
		t.Fatalf("CreateBackupFile failed: %v", err)
	}

	if err := db.SaveGlobalConfig(model.ConfigKeyQBUrl, "http://current-qb"); err != nil {
		t.Fatalf("failed to update qb config: %v", err)
	}
	currentTMDBToken := strings.Join([]string{"tmdb", "current"}, "-")
	if err := db.SaveGlobalConfig(model.ConfigKeyTMDBToken, currentTMDBToken); err != nil {
		t.Fatalf("failed to update tmdb config: %v", err)
	}
	if err := db.SaveGlobalConfig(model.ConfigKeyR2Bucket, "bucket-current"); err != nil {
		t.Fatalf("failed to update bucket config: %v", err)
	}

	svc := NewRestoreService()
	if err := svc.PerformRestore(tempPath, RestoreOptions{Configs: true}); err != nil {
		t.Fatalf("PerformRestore failed: %v", err)
	}

	var qbURL string
	db.DB.Model(&model.GlobalConfig{}).Where("key = ?", model.ConfigKeyQBUrl).Select("value").Scan(&qbURL)
	if qbURL != "http://current-qb" {
		t.Fatalf("expected qb config to be preserved, got %q", qbURL)
	}

	var tmdbToken string
	db.DB.Model(&model.GlobalConfig{}).Where("key = ?", model.ConfigKeyTMDBToken).Select("value").Scan(&tmdbToken)
	if tmdbToken != currentTMDBToken {
		t.Fatalf("expected tmdb config to be preserved, got %q", tmdbToken)
	}

	var bucket string
	db.DB.Model(&model.GlobalConfig{}).Where("key = ?", model.ConfigKeyR2Bucket).Select("value").Scan(&bucket)
	if bucket != backupValues[model.ConfigKeyR2Bucket] {
		t.Fatalf("expected R2 bucket to be restored from backup, got %q", bucket)
	}
}
