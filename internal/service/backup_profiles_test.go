package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"gorm.io/gorm"
)

func testGlobalConfigValue(t *testing.T, key string) string {
	t.Helper()
	var cfg model.GlobalConfig
	if err := db.DB.First(&cfg, "key = ?", key).Error; err != nil {
		t.Fatalf("load config %s: %v", key, err)
	}
	return cfg.Value
}

func TestFullBackupRestoresPlaybackHistoryWithLocalLibrary(t *testing.T) {
	db.InitDB(":memory:")
	t.Cleanup(func() { _ = db.CloseDB() })

	directory := model.LocalAnimeDirectory{Path: "/media/anime"}
	if err := db.DB.Create(&directory).Error; err != nil {
		t.Fatalf("seed local directory: %v", err)
	}
	anime := model.LocalAnime{DirectoryID: directory.ID, Title: "Restore Playback", Path: "/media/anime/restore-playback"}
	if err := db.DB.Create(&anime).Error; err != nil {
		t.Fatalf("seed local anime: %v", err)
	}
	episode := model.LocalEpisode{LocalAnimeID: anime.ID, Title: "Episode 1", EpisodeNum: 1, SeasonNum: 1, Path: "/media/anime/restore-playback/01.mkv"}
	if err := db.DB.Create(&episode).Error; err != nil {
		t.Fatalf("seed local episode: %v", err)
	}
	watchedAt := time.Date(2026, 7, 26, 12, 30, 0, 0, time.UTC)
	history := model.PlaybackHistory{UserID: 7, LocalAnimeID: anime.ID, LocalEpisodeID: episode.ID, PositionTicks: 600, DurationTicks: 1000, LastEvent: "pause", LastPlayedAt: watchedAt}
	if err := db.DB.Create(&history).Error; err != nil {
		t.Fatalf("seed playback history: %v", err)
	}

	backupPath := t.TempDir() + "/full.db"
	if err := CreateBackupFile(backupPath, BackupModeFull); err != nil {
		t.Fatalf("create full backup: %v", err)
	}
	if err := db.DB.Exec("DELETE FROM playback_histories").Error; err != nil {
		t.Fatalf("clear playback history: %v", err)
	}

	if err := NewRestoreService().PerformRestore(backupPath, RestoreOptions{Local: true}); err != nil {
		t.Fatalf("restore local library: %v", err)
	}
	var restored model.PlaybackHistory
	if err := db.DB.Where("user_id = ? AND local_episode_id = ?", 7, episode.ID).First(&restored).Error; err != nil {
		t.Fatalf("load restored playback history: %v", err)
	}
	if restored.PositionTicks != 600 || restored.DurationTicks != 1000 || restored.LastEvent != "pause" {
		t.Fatalf("unexpected restored playback history: %+v", restored)
	}
	if !restored.LastPlayedAt.Equal(watchedAt) {
		t.Fatalf("expected last played at %v, got %v", watchedAt, restored.LastPlayedAt)
	}
}

func TestFullBackupRestoresCredentialsWhenManifestContainsSecrets(t *testing.T) {
	db.InitDB(":memory:")
	t.Cleanup(func() { _ = db.CloseDB() })

	if err := db.SaveGlobalConfig(model.ConfigKeyQBPassword, "backup-password"); err != nil {
		t.Fatalf("seed backup password: %v", err)
	}
	backupPath := filepath.Join(t.TempDir(), "full-settings.db")
	if err := CreateBackupFile(backupPath, BackupModeFull); err != nil {
		t.Fatalf("create full backup: %v", err)
	}
	if err := db.SaveGlobalConfig(model.ConfigKeyQBPassword, "current-password"); err != nil {
		t.Fatalf("seed current password: %v", err)
	}
	if err := NewRestoreService().PerformRestore(backupPath, RestoreOptions{Configs: true}); err != nil {
		t.Fatalf("restore full backup settings: %v", err)
	}
	if got := testGlobalConfigValue(t, model.ConfigKeyQBPassword); got != "backup-password" {
		t.Fatalf("expected full backup credential to be restored, got %q", got)
	}
}

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

func TestSettingsBackupOmitsSecretsAndRestorePreservesCurrentCredentials(t *testing.T) {
	db.InitDB(":memory:")
	t.Cleanup(func() { _ = db.CloseDB() })

	if err := db.SaveGlobalConfig(model.ConfigKeyQBUrl, "http://backup-qb:8080"); err != nil {
		t.Fatalf("seed non-sensitive config: %v", err)
	}
	if err := db.SaveGlobalConfig(model.ConfigKeyQBPassword, "backup-password"); err != nil {
		t.Fatalf("seed sensitive config: %v", err)
	}
	if err := db.SaveGlobalConfig(model.ConfigKeyJellyfinApiKey, "backup-api-key"); err != nil {
		t.Fatalf("seed API key: %v", err)
	}

	backupPath := filepath.Join(t.TempDir(), "settings.db")
	if err := CreateBackupFile(backupPath, BackupModeSettings); err != nil {
		t.Fatalf("create settings backup: %v", err)
	}
	backupDB, err := gorm.Open(sqlite.Open(backupPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open settings backup: %v", err)
	}
	var backedUp []model.GlobalConfig
	if err := backupDB.Find(&backedUp).Error; err != nil {
		t.Fatalf("load backed up settings: %v", err)
	}
	for _, cfg := range backedUp {
		if IsSensitiveConfigKey(cfg.Key) {
			t.Fatalf("sensitive config %q was written to selective backup", cfg.Key)
		}
	}

	if err := db.SaveGlobalConfig(model.ConfigKeyQBUrl, "http://current-qb:8080"); err != nil {
		t.Fatalf("update non-sensitive config: %v", err)
	}
	if err := db.SaveGlobalConfig(model.ConfigKeyQBPassword, "current-password"); err != nil {
		t.Fatalf("update password: %v", err)
	}
	if err := db.SaveGlobalConfig(model.ConfigKeyJellyfinApiKey, "current-api-key"); err != nil {
		t.Fatalf("update API key: %v", err)
	}
	if err := NewRestoreService().PerformRestore(backupPath, RestoreOptions{Configs: true}); err != nil {
		t.Fatalf("restore settings backup: %v", err)
	}

	if got := testGlobalConfigValue(t, model.ConfigKeyQBUrl); got != "http://backup-qb:8080" {
		t.Fatalf("expected non-sensitive setting from backup, got %q", got)
	}
	if got := testGlobalConfigValue(t, model.ConfigKeyQBPassword); got != "current-password" {
		t.Fatalf("expected current password to be preserved, got %q", got)
	}
	if got := testGlobalConfigValue(t, model.ConfigKeyJellyfinApiKey); got != "current-api-key" {
		t.Fatalf("expected current API key to be preserved, got %q", got)
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
	if err := db.SaveGlobalConfig(model.ConfigKeyR2AccessKey, "CURRENT-ACCESS"); err != nil {
		t.Fatalf("failed to update access key: %v", err)
	}
	if err := db.SaveGlobalConfig(model.ConfigKeyR2SecretKey, "CURRENT-SECRET"); err != nil {
		t.Fatalf("failed to update secret key: %v", err)
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
	if got := testGlobalConfigValue(t, model.ConfigKeyR2AccessKey); got != "CURRENT-ACCESS" {
		t.Fatalf("expected current R2 access key to be preserved, got %q", got)
	}
	if got := testGlobalConfigValue(t, model.ConfigKeyR2SecretKey); got != "CURRENT-SECRET" {
		t.Fatalf("expected current R2 secret key to be preserved, got %q", got)
	}
}

func TestPartialLocalBackupDoesNotDeleteMissingEpisodeTable(t *testing.T) {
	db.InitDB(":memory:")
	t.Cleanup(func() { _ = db.CloseDB() })

	currentDir := model.LocalAnimeDirectory{Path: "/current"}
	if err := db.DB.Create(&currentDir).Error; err != nil {
		t.Fatalf("seed current directory: %v", err)
	}
	currentAnime := model.LocalAnime{DirectoryID: currentDir.ID, Title: "Current", Path: "/current/show"}
	if err := db.DB.Create(&currentAnime).Error; err != nil {
		t.Fatalf("seed current anime: %v", err)
	}
	currentEpisode := model.LocalEpisode{LocalAnimeID: currentAnime.ID, Title: "Keep Me", EpisodeNum: 7, Path: "/current/show/07.mkv"}
	if err := db.DB.Create(&currentEpisode).Error; err != nil {
		t.Fatalf("seed current episode: %v", err)
	}

	backupPath := filepath.Join(t.TempDir(), "partial-local.db")
	backupDB, err := gorm.Open(sqlite.Open(backupPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open partial backup: %v", err)
	}
	if err := backupDB.AutoMigrate(&model.LocalAnimeDirectory{}, &model.LocalAnime{}); err != nil {
		t.Fatalf("create partial local schema: %v", err)
	}
	backupDir := model.LocalAnimeDirectory{Model: gorm.Model{ID: currentDir.ID}, Path: "/restored"}
	backupAnime := model.LocalAnime{Model: gorm.Model{ID: currentAnime.ID}, DirectoryID: currentDir.ID, Title: "Restored", Path: "/restored/show"}
	if err := backupDB.Create(&backupDir).Error; err != nil {
		t.Fatalf("seed backup directory: %v", err)
	}
	if err := backupDB.Create(&backupAnime).Error; err != nil {
		t.Fatalf("seed backup anime: %v", err)
	}
	sqlDB, err := backupDB.DB()
	if err != nil {
		t.Fatalf("resolve backup SQL handle: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close partial backup: %v", err)
	}

	if err := NewRestoreService().PerformRestore(backupPath, RestoreOptions{Local: true}); err != nil {
		t.Fatalf("restore partial local backup: %v", err)
	}
	var episode model.LocalEpisode
	if err := db.DB.First(&episode, currentEpisode.ID).Error; err != nil {
		t.Fatalf("expected episode from missing backup table to remain: %v", err)
	}
	if episode.Path != currentEpisode.Path {
		t.Fatalf("expected episode path %q to remain, got %q", currentEpisode.Path, episode.Path)
	}
}

func TestRestoreRejectsBackupWithNewerSchema(t *testing.T) {
	db.InitDB(":memory:")
	t.Cleanup(func() { _ = db.CloseDB() })

	backupPath := filepath.Join(t.TempDir(), "future.db")
	if err := CreateBackupFile(backupPath, BackupModeSettings); err != nil {
		t.Fatalf("create backup: %v", err)
	}
	backupDB, err := gorm.Open(sqlite.Open(backupPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open backup: %v", err)
	}
	if err := backupDB.Model(&backupManifest{}).Where("id = ?", 1).Update("schema_version", "999_future").Error; err != nil {
		t.Fatalf("mark backup as future schema: %v", err)
	}
	sqlDB, _ := backupDB.DB()
	_ = sqlDB.Close()

	err = NewRestoreService().PerformRestore(backupPath, RestoreOptions{Configs: true})
	if err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("expected newer schema rejection, got %v", err)
	}
}

func TestInspectBackupRejectsNonSQLiteFile(t *testing.T) {
	tempPath := t.TempDir() + "/not-a-db.txt"
	if err := os.WriteFile(tempPath, []byte("definitely not sqlite"), 0o600); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	if _, err := InspectBackup(tempPath); err == nil {
		t.Fatal("expected InspectBackup to reject non-sqlite file")
	}
}
