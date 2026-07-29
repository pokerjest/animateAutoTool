package db

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"gorm.io/gorm"
)

func TestValidateMigrationOrderRejectsDuplicateAndOutOfOrderIDs(t *testing.T) {
	original := append([]migration(nil), migrations...)
	t.Cleanup(func() { migrations = original })

	migrations = []migration{{ID: "001_first"}, {ID: "001_duplicate"}}
	if err := validateMigrationOrder(); err == nil || !strings.Contains(err.Error(), "strictly increasing") {
		t.Fatalf("expected duplicate numeric order failure, got %v", err)
	}

	migrations = []migration{{ID: "002_second"}, {ID: "001_first"}}
	if err := validateMigrationOrder(); err == nil || !strings.Contains(err.Error(), "strictly increasing") {
		t.Fatalf("expected out-of-order failure, got %v", err)
	}

	migrations = []migration{{ID: "001_same"}, {ID: "002_same"}, {ID: "002_same"}}
	if err := validateMigrationOrder(); err == nil || !strings.Contains(err.Error(), "duplicate migration id") {
		t.Fatalf("expected duplicate id failure, got %v", err)
	}
}

func TestSchemaSequenceMigrationBackfillsHistoricalRows(t *testing.T) {
	target, err := gorm.Open(sqlite.Open(sqliteMemoryPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { closeTestDB(t, target) })

	if err := target.AutoMigrate(&SchemaMigration{}); err != nil {
		t.Fatalf("create schema history: %v", err)
	}
	for index, item := range migrations[:len(migrations)-1] {
		appliedAt := time.Date(2026, 1, 1, 0, len(migrations)-index, 0, 0, time.UTC)
		if err := target.Create(&SchemaMigration{
			ID:          item.ID,
			Description: item.Description,
			AppliedAt:   appliedAt,
		}).Error; err != nil {
			t.Fatalf("seed migration %s: %v", item.ID, err)
		}
	}

	if err := RunMigrations(target); err != nil {
		t.Fatalf("run sequence migration: %v", err)
	}

	var rows []SchemaMigration
	if err := target.Order("sequence").Find(&rows).Error; err != nil {
		t.Fatalf("load migration history: %v", err)
	}
	if len(rows) != len(migrations) {
		t.Fatalf("expected %d migrations, got %d", len(migrations), len(rows))
	}
	for index, row := range rows {
		if row.Sequence != index+1 {
			t.Fatalf("migration %s has sequence %d, expected %d", row.ID, row.Sequence, index+1)
		}
	}
	if got := CurrentSchemaVersion(target); got != migrations[len(migrations)-1].ID {
		t.Fatalf("expected latest schema %q, got %q", migrations[len(migrations)-1].ID, got)
	}
}

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
	if !target.Migrator().HasTable(&model.PlaybackHistory{}) {
		t.Fatal("expected playback_histories table to exist")
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
	if !target.Migrator().HasColumn(&model.Subscription{}, "resolution_filter") {
		t.Fatal("expected resolution_filter column to be added to subscriptions")
	}
	if !target.Migrator().HasColumn(&model.Subscription{}, "subtitle_language") {
		t.Fatal("expected subtitle_language column to be added to subscriptions")
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
	for _, item := range migrations {
		if item.ID == "005_subscription_mikan_ids" {
			break
		}
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

func TestMikanSubgroupRuleMigrationRemovesOnlyGeneratedRedundancy(t *testing.T) {
	target, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "mikan-subgroup-rules.db")), &gorm.Config{})
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
	for _, item := range migrations {
		if item.ID == "008_remove_redundant_mikan_subgroup_rules" {
			break
		}
		if err := target.Create(&SchemaMigration{ID: item.ID, Description: item.Description}).Error; err != nil {
			t.Fatalf("seed migration %s: %v", item.ID, err)
		}
	}

	items := []model.Subscription{
		{Title: "Generated", RSSUrl: "https://mikanani.me/RSS/Bangumi?bangumiId=4024&subgroupid=615", BackupRSSUrl: "https://mikanani.me/RSS/Bangumi?bangumiId=4024", SubtitleGroup: "Kirara Fantasia", FilterRule: "Kirara Fantasia"},
		{Title: "Escaped", RSSUrl: "https://mikanani.me/RSS/Bangumi?bangumiId=4025&subgroupid=616", BackupRSSUrl: "https://mikanani.me/RSS/Bangumi?bangumiId=4025", SubtitleGroup: "A+B [1080p]", FilterRule: `A\+B \[1080p\]`},
		{Title: "Custom", RSSUrl: "https://mikanani.me/RSS/Bangumi?bangumiId=4026&subgroupid=617", BackupRSSUrl: "https://mikanani.me/RSS/Bangumi?bangumiId=4026", SubtitleGroup: "ANi", FilterRule: `1080[Pp].*(CHS|简中)`},
		{Title: "Aggregate", RSSUrl: "https://mikanani.me/RSS/Bangumi?bangumiId=4027", SubtitleGroup: "ANi", FilterRule: "ANi"},
	}
	if err := target.Create(&items).Error; err != nil {
		t.Fatalf("seed subscriptions: %v", err)
	}
	if err := RunMigrations(target); err != nil {
		t.Fatalf("run subgroup cleanup migration: %v", err)
	}

	var got []model.Subscription
	if err := target.Order("id").Find(&got).Error; err != nil {
		t.Fatalf("load subscriptions: %v", err)
	}
	if got[0].FilterRule != "" || got[0].BackupRSSUrl != "" {
		t.Fatalf("expected generated values to be cleared, got filter=%q backup=%q", got[0].FilterRule, got[0].BackupRSSUrl)
	}
	if got[1].FilterRule != "" || got[1].BackupRSSUrl != "" {
		t.Fatalf("expected escaped generated values to be cleared, got filter=%q backup=%q", got[1].FilterRule, got[1].BackupRSSUrl)
	}
	if got[2].FilterRule != `1080[Pp].*(CHS|简中)` || got[2].BackupRSSUrl != "" {
		t.Fatalf("expected custom regex to remain while generated backup is cleared, got filter=%q backup=%q", got[2].FilterRule, got[2].BackupRSSUrl)
	}
	if got[3].FilterRule != "ANi" {
		t.Fatalf("expected aggregate feed rule to remain, got %q", got[3].FilterRule)
	}
}

func TestBangumiIDMigrationUsesPartialUniqueIndex(t *testing.T) {
	target, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "bangumi-index.db")), &gorm.Config{})
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
	for _, item := range migrations {
		if item.ID == "009_partial_bangumi_id_unique_index" {
			break
		}
		if err := target.Create(&SchemaMigration{ID: item.ID, Description: item.Description}).Error; err != nil {
			t.Fatalf("seed migration %s: %v", item.ID, err)
		}
	}

	if err := target.Exec("DROP INDEX IF EXISTS idx_anime_metadata_bangumi_id").Error; err != nil {
		t.Fatalf("drop current index: %v", err)
	}
	if err := target.Exec("CREATE UNIQUE INDEX idx_anime_metadata_bangumi_id ON anime_metadata(bangumi_id)").Error; err != nil {
		t.Fatalf("create legacy index: %v", err)
	}
	if err := target.Create(&model.AnimeMetadata{Title: "First unmatched"}).Error; err != nil {
		t.Fatalf("seed unmatched metadata: %v", err)
	}

	if err := RunMigrations(target); err != nil {
		t.Fatalf("run partial index migration: %v", err)
	}
	if err := target.Create(&model.AnimeMetadata{Title: "Second unmatched"}).Error; err != nil {
		t.Fatalf("partial index should allow another unmatched row: %v", err)
	}
	if err := target.Create(&model.AnimeMetadata{Title: "Matched", BangumiID: 4242}).Error; err != nil {
		t.Fatalf("create matched metadata: %v", err)
	}
	if err := target.Create(&model.AnimeMetadata{Title: "Duplicate match", BangumiID: 4242}).Error; err == nil {
		t.Fatal("partial index should still reject duplicate real Bangumi IDs")
	}

	var indexSQL string
	if err := target.Raw(
		"SELECT sql FROM sqlite_master WHERE type = 'index' AND name = ?",
		"idx_anime_metadata_bangumi_id",
	).Scan(&indexSQL).Error; err != nil {
		t.Fatalf("read partial index definition: %v", err)
	}
	if !strings.Contains(strings.ToLower(indexSQL), "where bangumi_id != 0") {
		t.Fatalf("expected partial Bangumi index, got %q", indexSQL)
	}
}
