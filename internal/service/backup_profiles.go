package service

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
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
	Mode               string
	ModeLabel          string
	Description        string
	ConfigStrategy     string
	SubscriptionCount  int64
	SubscriptionTitles []string
	DownloadLogCount   int64
	LocalAnimeCount    int64
	UserCount          int64
	GlobalConfigCount  int64
	DatabaseSize       string
	LastModified       string
	HasConfigs         bool
	HasMetadata        bool
	HasSubscriptions   bool
	HasLogs            bool
	HasLocal           bool
	HasUsers           bool
}

type backupManifest struct {
	ID             uint `gorm:"primaryKey"`
	Mode           string
	Label          string
	Description    string
	ConfigStrategy string
	CreatedAt      time.Time
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
		return "Cloudflare 云存档凭据"
	default:
		return "全量备份"
	}
}

func BackupModeDescription(mode string) string {
	switch NormalizeBackupMode(mode) {
	case BackupModeSettings:
		return "只包含系统设置中的配置数据，适合迁移下载器、媒体库、代理和第三方服务配置。"
	case BackupModeCloudflare:
		return "只包含 Cloudflare R2 云备份连接配置。恢复时会合并到当前设置，不会清空其他系统配置。"
	default:
		return "包含当前数据库中的全部业务数据，适合完整迁移和灾难恢复。"
	}
}

func BackupFilename(mode string, t time.Time) string {
	timestamp := t.Format("20060102_150405")
	switch NormalizeBackupMode(mode) {
	case BackupModeSettings:
		return fmt.Sprintf("animateData_settings_%s.db", timestamp)
	case BackupModeCloudflare:
		return fmt.Sprintf("animateData_cloudflare_%s.db", timestamp)
	default:
		return fmt.Sprintf("animateData_full_%s.db", timestamp)
	}
}

func R2BackupObjectKey(mode string, t time.Time) string {
	timestamp := t.Format("20060102_150405")
	switch NormalizeBackupMode(mode) {
	case BackupModeSettings:
		return fmt.Sprintf("animate_backup_settings_%s.db", timestamp)
	case BackupModeCloudflare:
		return fmt.Sprintf("animate_backup_cloudflare_%s.db", timestamp)
	default:
		return fmt.Sprintf("animate_backup_full_%s.db", timestamp)
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
	targetDB, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		return BackupDescriptor{}, err
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
	desc.HasLogs = targetDB.Migrator().HasTable(&model.DownloadLog{})
	desc.HasUsers = targetDB.Migrator().HasTable(&model.User{})
	desc.HasLocal = targetDB.Migrator().HasTable(&model.LocalAnime{}) ||
		targetDB.Migrator().HasTable(&model.LocalAnimeDirectory{}) ||
		targetDB.Migrator().HasTable(&model.LocalEpisode{})

	if desc.HasConfigs {
		targetDB.Model(&model.GlobalConfig{}).Count(&desc.GlobalConfigCount)
	}
	if desc.HasSubscriptions {
		targetDB.Model(&model.Subscription{}).Count(&desc.SubscriptionCount)
		targetDB.Model(&model.Subscription{}).Pluck("title", &desc.SubscriptionTitles)
	}
	if desc.HasLogs {
		targetDB.Model(&model.DownloadLog{}).Count(&desc.DownloadLogCount)
	}
	if desc.HasLocal {
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
			desc.Mode = NormalizeBackupMode(manifest.Mode)
			desc.ModeLabel = manifest.Label
			desc.Description = manifest.Description
			desc.ConfigStrategy = manifest.ConfigStrategy
			return desc, nil
		}
	}

	desc.Mode = inferLegacyBackupMode(targetDB, desc)
	desc.ModeLabel = BackupModeLabel(desc.Mode)
	desc.Description = BackupModeDescription(desc.Mode)
	desc.ConfigStrategy = backupConfigStrategyForMode(desc.Mode)
	return desc, nil
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
	if err := destDB.AutoMigrate(&backupManifest{}); err != nil {
		return err
	}
	if err := destDB.Exec("DELETE FROM backup_manifests").Error; err != nil {
		return err
	}
	return destDB.Create(&backupManifest{
		Mode:           mode,
		Label:          BackupModeLabel(mode),
		Description:    BackupModeDescription(mode),
		ConfigStrategy: backupConfigStrategyForMode(mode),
		CreatedAt:      time.Now(),
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

	if err := destDB.AutoMigrate(&backupManifest{}); err != nil {
		return err
	}

	if err := writeSelectiveBackupData(destDB, mode); err != nil {
		return err
	}

	return destDB.Create(&backupManifest{
		Mode:           mode,
		Label:          BackupModeLabel(mode),
		Description:    BackupModeDescription(mode),
		ConfigStrategy: backupConfigStrategyForMode(mode),
		CreatedAt:      time.Now(),
	}).Error
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
	if len(configs) > 0 {
		return destDB.CreateInBatches(&configs, 500).Error
	}
	return nil
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

func CleanBackupPath(path string) string {
	return filepath.Clean(path)
}
