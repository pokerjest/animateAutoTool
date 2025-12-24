package db

import (
	"log"
	"os"
	"path/filepath"

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
	dir := filepath.Dir(storagePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Fatalf("failed to create storage directory: %v", err)
	}

	DB, err = gorm.Open(sqlite.Open(storagePath), &gorm.Config{})
	if err != nil {
		log.Fatalf("failed to connect database: %v", err)
	}

	// 自动迁移模式
	err = DB.AutoMigrate(
		&model.Subscription{},
		&model.DownloadLog{},
		&model.GlobalConfig{},
		&model.LocalAnimeDirectory{},
		&model.LocalAnime{},
	)
	if err != nil {
		log.Fatalf("failed to migrate database: %v", err)
	}
}

func CloseDB() error {
	sqlDB, err := DB.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
