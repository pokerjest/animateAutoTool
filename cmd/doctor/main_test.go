package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	internaldb "github.com/pokerjest/animateAutoTool/internal/db"
	"gorm.io/gorm"
)

func TestValidateReadOnlyDatabaseRejectsMissingRequiredTables(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doctor.db")
	target, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	sqlDB, err := target.DB()
	if err != nil {
		t.Fatalf("get sql database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	if err := target.AutoMigrate(&internaldb.SchemaMigration{}); err != nil {
		t.Fatalf("create schema migration table: %v", err)
	}
	if err := target.Create(&internaldb.SchemaMigration{
		ID:       internaldb.LatestSchemaVersion(),
		Sequence: 1,
	}).Error; err != nil {
		t.Fatalf("record schema version: %v", err)
	}

	err = validateReadOnlyDatabase(target)
	if err == nil || !strings.Contains(err.Error(), "缺少诊断所需的数据表") {
		t.Fatalf("expected missing-table error, got %v", err)
	}
}
