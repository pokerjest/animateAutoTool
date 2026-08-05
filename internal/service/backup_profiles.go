package service

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/safeio"
	appversion "github.com/pokerjest/animateAutoTool/internal/version"
	"gorm.io/gorm"
)

const (
	BackupModeFull       = "full"
	BackupModeSettings   = "settings"
	BackupModeCloudflare = "cloudflare"
)

const (
	backupConfigStrategyReplace = "replace"
	backupConfigStrategyMerge   = "merge"
)

type BackupDescriptor struct {
	Mode                     string
	ModeLabel                string
	Description              string
	ConfigStrategy           string
	SubscriptionCount        int64
	SubscriptionTitles       []string
	DownloadLogCount         int64
	LocalAnimeCount          int64
	UserCount                int64
	GlobalConfigCount        int64
	DatabaseSize             string
	LastModified             string
	HasConfigs               bool
	HasMetadata              bool
	HasSubscriptions         bool
	HasLogs                  bool
	HasLocal                 bool
	HasUsers                 bool
	HasDownloadLogs          bool
	HasSubscriptionResources bool
	HasRunLogs               bool
	HasLocalDirectories      bool
	HasLocalAnime            bool
	HasLocalEpisodes         bool
	HasPlaybackHistory       bool
	FormatVersion            int
	DatabaseFormat           int
	SchemaFormat             int
	AppVersion               string
	SchemaVersion            string
	ContainsSecrets          bool
	DatabaseSHA256           string
}

type backupManifest struct {
	ID              uint `gorm:"primaryKey"`
	FormatVersion   int
	DatabaseFormat  int
	SchemaFormat    int
	Mode            string
	Label           string
	Description     string
	ConfigStrategy  string
	AppVersion      string
	SchemaVersion   string
	ContainsSecrets bool
	DatabaseSHA256  string
	CreatedAt       time.Time
}

func NormalizeBackupMode(mode string) string {
	switch mode {
	case BackupModeSettings:
		return BackupModeSettings
	case BackupModeCloudflare:
		return BackupModeCloudflare
	default:
		return BackupModeFull
	}
}

func BackupModeLabel(mode string) string {
	switch NormalizeBackupMode(mode) {
	case BackupModeSettings:
		return "系统设置备份"
	case BackupModeCloudflare:
		return "Cloudflare 云存档设置"
	default:
		return "全量备份"
	}
}

func BackupModeDescription(mode string) string {
	switch NormalizeBackupMode(mode) {
	case BackupModeSettings:
		return "包含系统设置中的非敏感配置；密码、Token、API Key 等凭据不会写入备份，恢复时保留当前设备凭据。"
	case BackupModeCloudflare:
		return "包含 Cloudflare R2 的 Endpoint 和 Bucket；访问密钥不会写入备份，恢复时保留当前设备凭据。"
	default:
		return "包含当前数据库中的全部业务数据和已保存凭据，适合完整迁移和灾难恢复；请妥善保管备份文件。"
	}
}

func BackupFilename(mode string, t time.Time) string {
	timestamp := t.Format("20060102_150405")
	switch NormalizeBackupMode(mode) {
	case BackupModeSettings:
		return fmt.Sprintf("animateData_settings_%s.zip", timestamp)
	case BackupModeCloudflare:
		return fmt.Sprintf("animateData_cloudflare_%s.zip", timestamp)
	default:
		return fmt.Sprintf("animateData_full_%s.zip", timestamp)
	}
}

func R2BackupObjectKey(mode string, t time.Time) string {
	timestamp := t.Format("20060102_150405")
	switch NormalizeBackupMode(mode) {
	case BackupModeSettings:
		return fmt.Sprintf("animate_backup_settings_%s.zip", timestamp)
	case BackupModeCloudflare:
		return fmt.Sprintf("animate_backup_cloudflare_%s.zip", timestamp)
	default:
		return fmt.Sprintf("animate_backup_full_%s.zip", timestamp)
	}
}

func CreateBackupFile(destPath string, mode string) error {
	mode = NormalizeBackupMode(mode)
	if mode == BackupModeFull {
		if err := createFullBackupFile(destPath); err != nil {
			return err
		}
		return annotateBackupFile(destPath, mode)
	}

	return createSelectiveBackupFile(destPath, mode)
}

func InspectBackup(path string) (BackupDescriptor, error) {
	if !isSQLiteBackupFile(path) {
		return BackupDescriptor{}, fmt.Errorf("invalid sqlite backup file")
	}

	targetDB, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		return BackupDescriptor{}, err
	}
	if sqlDB, err := targetDB.DB(); err == nil {
		defer safeio.Close(sqlDB)
	}

	desc := BackupDescriptor{
		Mode:           BackupModeFull,
		ModeLabel:      BackupModeLabel(BackupModeFull),
		Description:    BackupModeDescription(BackupModeFull),
		ConfigStrategy: backupConfigStrategyReplace,
	}

	desc.HasConfigs = targetDB.Migrator().HasTable(&model.GlobalConfig{})
	desc.HasMetadata = targetDB.Migrator().HasTable(&model.AnimeMetadata{})
	desc.HasSubscriptions = targetDB.Migrator().HasTable(&model.Subscription{})
	desc.HasDownloadLogs = targetDB.Migrator().HasTable(&model.DownloadLog{})
	desc.HasSubscriptionResources = targetDB.Migrator().HasTable(&model.SubscriptionResource{})
	desc.HasRunLogs = targetDB.Migrator().HasTable(&model.SubscriptionRunLog{})
	desc.HasLogs = desc.HasDownloadLogs || desc.HasSubscriptionResources || desc.HasRunLogs
	desc.HasUsers = targetDB.Migrator().HasTable(&model.User{})
	desc.HasLocalDirectories = targetDB.Migrator().HasTable(&model.LocalAnimeDirectory{})
	desc.HasLocalAnime = targetDB.Migrator().HasTable(&model.LocalAnime{})
	desc.HasLocalEpisodes = targetDB.Migrator().HasTable(&model.LocalEpisode{})
	desc.HasPlaybackHistory = targetDB.Migrator().HasTable(&model.PlaybackHistory{})
	desc.HasLocal = desc.HasLocalDirectories || desc.HasLocalAnime || desc.HasLocalEpisodes || desc.HasPlaybackHistory

	if desc.HasConfigs {
		targetDB.Model(&model.GlobalConfig{}).Count(&desc.GlobalConfigCount)
	}
	if desc.HasSubscriptions {
		targetDB.Model(&model.Subscription{}).Count(&desc.SubscriptionCount)
		targetDB.Model(&model.Subscription{}).Pluck("title", &desc.SubscriptionTitles)
	}
	if desc.HasLogs {
		if desc.HasDownloadLogs {
			targetDB.Model(&model.DownloadLog{}).Count(&desc.DownloadLogCount)
		}
		if desc.HasRunLogs {
			var runLogCount int64
			targetDB.Model(&model.SubscriptionRunLog{}).Count(&runLogCount)
			desc.DownloadLogCount += runLogCount
		}
		if desc.HasSubscriptionResources {
			var resourceCount int64
			targetDB.Model(&model.SubscriptionResource{}).Count(&resourceCount)
			desc.DownloadLogCount += resourceCount
		}
	}
	if desc.HasLocalAnime {
		targetDB.Model(&model.LocalAnime{}).Count(&desc.LocalAnimeCount)
	}
	if desc.HasUsers {
		targetDB.Model(&model.User{}).Count(&desc.UserCount)
	}

	if info, err := os.Stat(path); err == nil {
		desc.DatabaseSize = fmt.Sprintf("%.2f MB", float64(info.Size())/1024/1024)
		desc.LastModified = info.ModTime().Format("2006-01-02 15:04:05")
	} else {
		desc.DatabaseSize = "Unknown"
		desc.LastModified = "Unknown"
	}

	if targetDB.Migrator().HasTable(&backupManifest{}) {
		var manifest backupManifest
		if err := targetDB.Order("id desc").First(&manifest).Error; err == nil {
			if manifest.FormatVersion > 1 {
				return BackupDescriptor{}, fmt.Errorf("backup format version %d is newer than this application supports", manifest.FormatVersion)
			}
			desc.Mode = NormalizeBackupMode(manifest.Mode)
			desc.ModeLabel = manifest.Label
			desc.Description = manifest.Description
			desc.ConfigStrategy = manifest.ConfigStrategy
			desc.FormatVersion = manifest.FormatVersion
			desc.DatabaseFormat = manifest.DatabaseFormat
			desc.SchemaFormat = manifest.SchemaFormat
			desc.AppVersion = manifest.AppVersion
			desc.SchemaVersion = manifest.SchemaVersion
			desc.ContainsSecrets = manifest.ContainsSecrets
			desc.DatabaseSHA256 = manifest.DatabaseSHA256
			if desc.FormatVersion == 0 {
				desc.FormatVersion = 1
			}
			if manifest.SchemaVersion != "" && backupSchemaNumber(manifest.SchemaVersion) < 0 {
				return BackupDescriptor{}, fmt.Errorf("backup schema version %q is invalid", manifest.SchemaVersion)
			}
			return desc, nil
		}
	}

	desc.Mode = inferLegacyBackupMode(targetDB, desc)
	desc.ModeLabel = BackupModeLabel(desc.Mode)
	desc.Description = BackupModeDescription(desc.Mode)
	desc.ConfigStrategy = backupConfigStrategyForMode(desc.Mode)
	desc.ContainsSecrets = backupContainsSensitiveConfigs(path)
	return desc, nil
}

func backupSchemaNumber(value string) int {
	value = strings.TrimSpace(value)
	if index := strings.IndexByte(value, '_'); index >= 0 {
		value = value[:index]
	}
	if value == "" {
		return -1
	}
	number := 0
	for _, r := range value {
		if r < '0' || r > '9' {
			return -1
		}
		number = number*10 + int(r-'0')
	}
	return number
}

func isSQLiteBackupFile(path string) bool {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return false
	}
	defer func() {
		_ = f.Close()
	}()

	header := make([]byte, 16)
	if _, err := f.Read(header); err != nil {
		return false
	}

	return string(header) == "SQLite format 3\x00"
}

func BackupConfigMerges(mode string) bool {
	return backupConfigStrategyForMode(mode) == backupConfigStrategyMerge
}

func backupConfigStrategyForMode(mode string) string {
	if NormalizeBackupMode(mode) == BackupModeCloudflare {
		return backupConfigStrategyMerge
	}
	return backupConfigStrategyReplace
}

func createFullBackupFile(destPath string) error {
	if err := os.Remove(destPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to clear target file: %v", err)
	}
	if err := db.DB.Exec("VACUUM INTO ?", destPath).Error; err != nil {
		return fmt.Errorf("backup failed (VACUUM INTO): %v", err)
	}
	return nil
}

func annotateBackupFile(destPath string, mode string) error {
	destDB, err := gorm.Open(sqlite.Open(destPath), &gorm.Config{})
	if err != nil {
		return err
	}
	if sqlDB, err := destDB.DB(); err == nil {
		defer safeio.Close(sqlDB)
	}
	if err := destDB.AutoMigrate(&backupManifest{}); err != nil {
		return err
	}
	if err := destDB.Exec("DELETE FROM backup_manifests").Error; err != nil {
		return err
	}
	return destDB.Create(&backupManifest{
		FormatVersion:   1,
		DatabaseFormat:  db.DatabaseFormat,
		SchemaFormat:    db.SchemaFormat,
		Mode:            mode,
		Label:           BackupModeLabel(mode),
		Description:     BackupModeDescription(mode),
		ConfigStrategy:  backupConfigStrategyForMode(mode),
		AppVersion:      appversion.AppVersion,
		SchemaVersion:   db.CurrentSchemaVersion(db.DB),
		ContainsSecrets: mode == BackupModeFull,
		DatabaseSHA256:  sha256File(destPath),
		CreatedAt:       time.Now().UTC(),
	}).Error
}

func createSelectiveBackupFile(destPath string, mode string) error {
	if err := os.Remove(destPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to clear target file: %v", err)
	}

	destDB, err := gorm.Open(sqlite.Open(destPath), &gorm.Config{})
	if err != nil {
		return err
	}
	if sqlDB, err := destDB.DB(); err == nil {
		defer safeio.Close(sqlDB)
	}

	if err := destDB.AutoMigrate(&backupManifest{}); err != nil {
		return err
	}

	if err := writeSelectiveBackupData(destDB, mode); err != nil {
		return err
	}

	return destDB.Create(&backupManifest{
		FormatVersion:   1,
		DatabaseFormat:  db.DatabaseFormat,
		SchemaFormat:    db.SchemaFormat,
		Mode:            mode,
		Label:           BackupModeLabel(mode),
		Description:     BackupModeDescription(mode),
		ConfigStrategy:  backupConfigStrategyForMode(mode),
		AppVersion:      appversion.AppVersion,
		SchemaVersion:   db.CurrentSchemaVersion(db.DB),
		ContainsSecrets: false,
		DatabaseSHA256:  sha256File(destPath),
		CreatedAt:       time.Now().UTC(),
	}).Error
}

func IsSensitiveConfigKey(key string) bool {
	lowerKey := strings.ToLower(key)
	return strings.Contains(lowerKey, "password") ||
		strings.Contains(lowerKey, "secret") ||
		strings.Contains(lowerKey, "token") ||
		strings.Contains(lowerKey, "key") ||
		strings.Contains(lowerKey, "credential")
}

func writeSelectiveBackupData(destDB *gorm.DB, mode string) error {
	if err := destDB.AutoMigrate(&model.GlobalConfig{}); err != nil {
		return err
	}

	var configs []model.GlobalConfig
	query := db.DB.Model(&model.GlobalConfig{})
	if NormalizeBackupMode(mode) == BackupModeCloudflare {
		query = query.Where("key IN ?", cloudflareConfigKeys())
	}
	if err := query.Find(&configs).Error; err != nil {
		return err
	}

	// Selective backups intentionally omit sensitive credentials rather than
	// writing empty values. Restore can therefore preserve the current device's
	// credentials instead of clearing them.
	filtered := configs[:0]
	for _, cfg := range configs {
		if !IsSensitiveConfigKey(cfg.Key) {
			filtered = append(filtered, cfg)
		}
	}
	configs = filtered

	if len(configs) > 0 {
		return destDB.CreateInBatches(&configs, 500).Error
	}
	return nil
}

func sha256File(path string) string {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return ""
	}
	defer safeio.Close(file)
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return ""
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func backupContainsSensitiveConfigs(path string) bool {
	targetDB, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		return false
	}
	if sqlDB, err := targetDB.DB(); err == nil {
		defer safeio.Close(sqlDB)
	}
	if !targetDB.Migrator().HasTable(&model.GlobalConfig{}) {
		return false
	}
	var keys []string
	if err := targetDB.Model(&model.GlobalConfig{}).Pluck("key", &keys).Error; err != nil {
		return false
	}
	for _, key := range keys {
		if IsSensitiveConfigKey(key) {
			return true
		}
	}
	return false
}

func inferLegacyBackupMode(targetDB *gorm.DB, desc BackupDescriptor) string {
	if desc.HasConfigs && !desc.HasMetadata && !desc.HasSubscriptions && !desc.HasLogs && !desc.HasLocal && !desc.HasUsers {
		var keys []string
		if err := targetDB.Model(&model.GlobalConfig{}).Pluck("key", &keys).Error; err == nil && len(keys) > 0 {
			allR2 := true
			for _, key := range keys {
				if !isCloudflareConfigKey(key) {
					allR2 = false
					break
				}
			}
			if allR2 {
				return BackupModeCloudflare
			}
		}
		return BackupModeSettings
	}
	return BackupModeFull
}

func cloudflareConfigKeys() []string {
	return []string{
		model.ConfigKeyR2Endpoint,
		model.ConfigKeyR2AccessKey,
		model.ConfigKeyR2SecretKey,
		model.ConfigKeyR2Bucket,
	}
}

func isCloudflareConfigKey(key string) bool {
	for _, allowed := range cloudflareConfigKeys() {
		if key == allowed {
			return true
		}
	}
	return false
}

func BackupContainsConfigsOnly(mode string) bool {
	mode = NormalizeBackupMode(mode)
	return mode == BackupModeSettings || mode == BackupModeCloudflare
}

func CleanBackupPath(value string) string {
	return path.Clean(strings.ReplaceAll(value, "\\", "/"))
}
