package db

import (
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"gorm.io/gorm"
)

func closeTestDB(t *testing.T, target *gorm.DB) {
	t.Helper()
	sqlDB, err := target.DB()
	if err != nil {
		t.Fatalf("resolve sql handle: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close sqlite db: %v", err)
	}
}

func TestRunMigrationsRecordsCurrentVersionAndIsIdempotent(t *testing.T) {
	tempPath := filepath.Join(t.TempDir(), "app.db")

	target, err := gorm.Open(sqlite.Open(tempPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() {
		closeTestDB(t, target)
	})

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
	t.Cleanup(func() {
		closeTestDB(t, target)
	})

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
	if !target.Migrator().HasColumn(&model.Subscription{}, "backup_rss_url") {
		t.Fatal("expected backup_rss_url column to be added to subscriptions")
	}
	if !target.Migrator().HasColumn(&model.Subscription{}, "expected_episodes") {
		t.Fatal("expected expected_episodes column to be added to subscriptions")
	}
	if !target.Migrator().HasTable(&model.LibraryIssue{}) {
		t.Fatal("expected library_issues table to be created for legacy databases")
	}
}

func TestMikanIDMigrationBackfillsOnlyMissingOfficialRSSAssociations(t *testing.T) {
	target, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "mikan-backfill.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { closeTestDB(t, target) })

	if err := autoMigrateCoreSchema(target); err != nil {
		t.Fatalf("migrate core schema: %v", err)
	}
	if err := target.AutoMigrate(&SchemaMigration{}); err != nil {
		t.Fatalf("migrate schema history: %v", err)
	}
	for _, item := range migrations[:len(migrations)-1] {
		if err := target.Create(&SchemaMigration{ID: item.ID, Description: item.Description}).Error; err != nil {
			t.Fatalf("seed migration %s: %v", item.ID, err)
		}
	}

	items := []model.Subscription{
		{Title: "Missing", RSSUrl: "https://mikanani.me/RSS/Bangumi?bangumiId=3141&subgroupid=583"},
		{Title: "Existing", RSSUrl: "https://mikanani.me/RSS/Bangumi?bangumiId=99", MikanID: "keep-me"},
		{Title: "External", RSSUrl: "https://example.com/RSS/Bangumi?bangumiId=88"},
	}
	if err := target.Create(&items).Error; err != nil {
		t.Fatalf("seed subscriptions: %v", err)
	}

	if err := RunMigrations(target); err != nil {
		t.Fatalf("run Mikan backfill migration: %v", err)
	}
	if err := RunMigrations(target); err != nil {
		t.Fatalf("rerun migrations: %v", err)
	}

	var got []model.Subscription
	if err := target.Order("id").Find(&got).Error; err != nil {
		t.Fatalf("load subscriptions: %v", err)
	}
	if got[0].MikanID != "3141" {
		t.Fatalf("expected missing association to be backfilled, got %q", got[0].MikanID)
	}
	if got[1].MikanID != "keep-me" {
		t.Fatalf("expected existing association to be preserved, got %q", got[1].MikanID)
	}
	if got[2].MikanID != "" {
		t.Fatalf("expected external RSS to remain untouched, got %q", got[2].MikanID)
	}
}
