package db

import (
	"database/sql"
	"fmt"
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

const (
	sqliteMemoryPath       = ":memory:"
	sqliteSharedMemoryPath = "file::memory:?cache=shared"
)

var DB *gorm.DB
var CurrentDBPath string
var currentDBGOOS = func() string { return runtime.GOOS }

func InitDB(storagePath string) {
	if err := InitDBWithError(storagePath); err != nil {
		log.Fatalf("failed to initialize database: %v", err)
	}
}

// InitDBWithError initializes the writable application database and returns
// failures to callers that must unwind other resources before process exit.
func InitDBWithError(storagePath string) error {
	if !isInMemoryDB(storagePath) {
		dir := filepath.Dir(storagePath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create storage directory: %w", err)
		}
	}

	driverPath := sqliteSharedMemoryPath
	if !isInMemoryDB(storagePath) {
		driverPath = sqliteDriverPath(storagePath)
	}

	target, err := gorm.Open(sqlite.Open(driverPath), &gorm.Config{})
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	sqlDB, err := target.DB()
	if err != nil {
		return fmt.Errorf("access sql database handle: %w", err)
	}
	closeOnError := true
	defer func() {
		if closeOnError {
			_ = sqlDB.Close()
		}
	}()

	// SQLite has a single writer. Keeping one application connection prevents
	// concurrent background metadata jobs from racing for the write lock while
	// still allowing the driver-level busy timeout to cover external locks.
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	if !isInMemoryDB(storagePath) {
		if err := CheckIntegrity(target); err != nil {
			return fmt.Errorf("database integrity check failed: %w", err)
		}
	}
	if err := RunMigrations(target); err != nil {
		return fmt.Errorf("migrate database: %w", err)
	}

	DB = target
	CurrentDBPath = storagePath
	closeOnError = false
	if version := CurrentSchemaVersion(target); version != "" {
		log.Printf("database schema is now at %s", version)
	}
	return nil
}

// OpenReadOnly opens an existing SQLite database without creating directories,
// running migrations, or changing the package-level DB handle.
func OpenReadOnly(storagePath string) (*gorm.DB, *sql.DB, error) {
	if isInMemoryDB(storagePath) {
		return nil, nil, fmt.Errorf("read-only database requires a file path")
	}
	info, err := os.Stat(storagePath)
	if err != nil {
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("database path is not a regular file: %s", storagePath)
	}
	readDB, err := gorm.Open(sqlite.Open(sqliteReadOnlyPath(storagePath)), &gorm.Config{})
	if err != nil {
		return nil, nil, err
	}
	sqlDB, err := readDB.DB()
	if err != nil {
		return nil, nil, err
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	if err := readDB.Exec("PRAGMA query_only = ON").Error; err != nil {
		_ = sqlDB.Close()
		return nil, nil, err
	}
	return readDB, sqlDB, nil
}

func CloseDB() error {
	if DB == nil {
		return nil
	}
	if !isInMemoryDB(CurrentDBPath) {
		var journalMode string
		if err := DB.Raw("PRAGMA journal_mode").Scan(&journalMode).Error; err == nil &&
			strings.EqualFold(strings.TrimSpace(journalMode), "wal") {
			if err := DB.Exec("PRAGMA wal_checkpoint(TRUNCATE)").Error; err != nil {
				log.Printf("WARN: database WAL checkpoint during shutdown failed: %v", err)
			}
		}
	}
	sqlDB, err := DB.DB()
	if err != nil {
		return err
	}
	err = sqlDB.Close()
	DB = nil
	return err
}

func CheckIntegrity(target *gorm.DB) error {
	if target == nil {
		return gorm.ErrInvalidDB
	}
	var result string
	if err := target.Raw("PRAGMA quick_check").Scan(&result).Error; err != nil {
		return err
	}
	if !strings.EqualFold(strings.TrimSpace(result), "ok") {
		return fmt.Errorf("sqlite quick_check returned %q", result)
	}
	return nil
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
	return storagePath == sqliteMemoryPath || strings.HasPrefix(storagePath, "file::memory:")
}

func sqliteDriverPath(storagePath string) string {
	separator := "?"
	if strings.Contains(storagePath, "?") {
		separator = "&"
	}
	driverPath := storagePath + separator +
		"_pragma=busy_timeout(5000)&_pragma=synchronous(FULL)&_pragma=foreign_keys(ON)"
	if currentDBGOOS() != "windows" {
		return driverPath
	}

	// modernc/glebarez SQLite can fail to clean up rollback journals on Windows
	// in some portable/self-contained layouts. WAL keeps crash recovery and
	// durability characteristics closer to the default mode while avoiding the
	// most fragile rollback-journal path.
	return driverPath + "&_pragma=journal_mode(WAL)"
}

func sqliteReadOnlyPath(storagePath string) string {
	separator := "?"
	if strings.Contains(storagePath, "?") {
		separator = "&"
	}
	return "file:" + storagePath + separator + "mode=ro"
}
