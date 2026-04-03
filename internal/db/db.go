package db

import (
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/glebarez/sqlite"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"gorm.io/gorm"
)

var DB *gorm.DB
var CurrentDBPath string

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

	driverPath := storagePath
	if isInMemoryDB(storagePath) {
		// SQLite keeps plain :memory: databases per connection, which causes tables to
		// disappear when GORM opens additional pooled connections during tests.
		driverPath = "file::memory:?cache=shared"
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
	err := DB.Where(model.GlobalConfig{Key: key}).Assign(model.GlobalConfig{Value: value}).FirstOrCreate(&conf).Error
	return err
}

func isInMemoryDB(storagePath string) bool {
	return storagePath == ":memory:" || strings.HasPrefix(storagePath, "file::memory:")
}
