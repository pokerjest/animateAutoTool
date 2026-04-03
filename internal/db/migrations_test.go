package db

import (
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"gorm.io/gorm"
)

func TestRunMigrationsRecordsCurrentVersionAndIsIdempotent(t *testing.T) {
	tempPath := filepath.Join(t.TempDir(), "app.db")

	target, err := gorm.Open(sqlite.Open(tempPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}

	if err := RunMigrations(target); err != nil {
		t.Fatalf("first migration run failed: %v", err)
	}

	if !target.Migrator().HasTable(&SchemaMigration{}) {
		t.Fatal("expected schema_migrations table to exist")
	}
	if !target.Migrator().HasTable(&model.Subscription{}) {
		t.Fatal("expected subscriptions table to exist")
	}

	var count int64
	if err := target.Model(&SchemaMigration{}).Count(&count).Error; err != nil {
		t.Fatalf("count schema migrations: %v", err)
	}
	if count != int64(len(migrations)) {
		t.Fatalf("expected %d schema migrations, got %d", len(migrations), count)
	}

	if got := CurrentSchemaVersion(target); got != migrations[len(migrations)-1].ID {
		t.Fatalf("expected current schema version %q, got %q", migrations[len(migrations)-1].ID, got)
	}

	if err := RunMigrations(target); err != nil {
		t.Fatalf("second migration run failed: %v", err)
	}

	var countAfter int64
	if err := target.Model(&SchemaMigration{}).Count(&countAfter).Error; err != nil {
		t.Fatalf("count schema migrations after rerun: %v", err)
	}
	if countAfter != count {
		t.Fatalf("expected schema migration count to remain %d, got %d", count, countAfter)
	}
}

func TestRunMigrationsUpgradesLegacySubscriptionSchema(t *testing.T) {
	tempPath := filepath.Join(t.TempDir(), "legacy.db")

	target, err := gorm.Open(sqlite.Open(tempPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}

	if err := target.Exec(`
		CREATE TABLE subscriptions (
			id integer primary key autoincrement,
			created_at datetime,
			updated_at datetime,
			deleted_at datetime,
			title text,
			rss_url text
		)
	`).Error; err != nil {
		t.Fatalf("create legacy subscriptions table: %v", err)
	}

	if err := RunMigrations(target); err != nil {
		t.Fatalf("run migrations on legacy schema: %v", err)
	}

	if !target.Migrator().HasColumn(&model.Subscription{}, "last_run_status") {
		t.Fatal("expected last_run_status column to be added to subscriptions")
	}
	if !target.Migrator().HasColumn(&model.Subscription{}, "last_run_summary") {
		t.Fatal("expected last_run_summary column to be added to subscriptions")
	}
	if !target.Migrator().HasTable(&model.LibraryIssue{}) {
		t.Fatal("expected library_issues table to be created for legacy databases")
	}
}
