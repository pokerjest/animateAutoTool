package db

import (
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/glebarez/sqlite"
	"github.com/pokerjest/animateAutoTool/internal/config"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"gorm.io/gorm"
)

var DB *gorm.DB
var CurrentDBPath string
var currentDBGOOS = func() string { return runtime.GOOS }

func InitDB(storagePath string) {
	CurrentDBPath = storagePath
	var err error

	// 确保存储目录存在
	if !isInMemoryDB(storagePath) {
		dir := filepath.Dir(storagePath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Fatalf("failed to create storage directory: %v", err)
		}
	}

	var driverPath string
	if isInMemoryDB(storagePath) {
		// SQLite keeps plain :memory: databases per connection, which causes tables to
		// disappear when GORM opens additional pooled connections during tests.
		driverPath = "file::memory:?cache=shared"
	} else {
		driverPath = sqliteDriverPath(storagePath)
	}

	DB, err = gorm.Open(sqlite.Open(driverPath), &gorm.Config{})
	if err != nil {
		log.Fatalf("failed to connect database: %v", err)
	}

	sqlDB, err := DB.DB()
	if err != nil {
		log.Fatalf("failed to access sql database handle: %v", err)
	}
	if isInMemoryDB(storagePath) {
		sqlDB.SetMaxOpenConns(1)
		sqlDB.SetMaxIdleConns(1)
	}

	err = RunMigrations(DB)
	if err != nil {
		log.Fatalf("failed to migrate database: %v", err)
	}
	if version := CurrentSchemaVersion(DB); version != "" {
		log.Printf("database schema is now at %s", version)
	}
}

func CloseDB() error {
	sqlDB, err := DB.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// SaveGlobalConfig helper to upsert config
func SaveGlobalConfig(key string, value string) error {
	var conf model.GlobalConfig
	if err := DB.Where(model.GlobalConfig{Key: key}).Assign(model.GlobalConfig{Value: value}).FirstOrCreate(&conf).Error; err != nil {
		return err
	}
	return config.UpdateSystemSettings(map[string]string{key: value})
}

// SyncGlobalConfigsWithConfigFile applies explicitly configured YAML values to
// the database, then exports the complete effective database set back to the
// local mirror. YAML therefore supports portable/manual configuration without
// replacing the database as the runtime query source.
func SyncGlobalConfigsWithConfigFile() error {
	if config.AppConfig == nil || config.AppPaths.ConfigFile == "" {
		return nil
	}
	if len(config.AppConfig.SystemSettings) > 0 {
		if err := DB.Transaction(func(tx *gorm.DB) error {
			for key, value := range config.AppConfig.SystemSettings {
				var conf model.GlobalConfig
				if err := tx.Where(model.GlobalConfig{Key: key}).
					Assign(model.GlobalConfig{Value: value}).
					FirstOrCreate(&conf).Error; err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return ExportGlobalConfigsToConfigFile()
}

// ExportGlobalConfigsToConfigFile replaces the YAML mirror with the current
// database values. It is also called after restoring system settings.
func ExportGlobalConfigsToConfigFile() error {
	if config.AppConfig == nil || config.AppPaths.ConfigFile == "" {
		return nil
	}
	var configs []model.GlobalConfig
	if err := DB.Find(&configs).Error; err != nil {
		return err
	}
	values := make(map[string]string, len(configs))
	for _, item := range configs {
		values[item.Key] = item.Value
	}
	return config.ReplaceSystemSettings(values)
}

func isInMemoryDB(storagePath string) bool {
	return storagePath == ":memory:" || strings.HasPrefix(storagePath, "file::memory:")
}

func sqliteDriverPath(storagePath string) string {
	if currentDBGOOS() != "windows" {
		return storagePath
	}

	separator := "?"
	if strings.Contains(storagePath, "?") {
		separator = "&"
	}

	// modernc/glebarez SQLite can fail to clean up rollback journals on Windows
	// in some portable/self-contained layouts. WAL keeps crash recovery and
	// durability characteristics closer to the default mode while avoiding the
	// most fragile rollback-journal path.
	return storagePath + separator + "_pragma=journal_mode(WAL)"
}
