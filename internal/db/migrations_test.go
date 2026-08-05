package db

import (
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/pokerjest/animateAutoTool/internal/config"
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

	migrations = []migration{{ID: "001_missing_fingerprint"}}
	if err := validateMigrationOrder(); err == nil || !strings.Contains(err.Error(), "missing an immutable fingerprint") {
		t.Fatalf("expected missing fingerprint failure, got %v", err)
	}

	migrations = []migration{
		{ID: "001_first", Fingerprint: strings.Repeat("a", sha256.Size*2)},
		{ID: "002_second", Fingerprint: strings.Repeat("a", sha256.Size*2)},
	}
	if err := validateMigrationOrder(); err == nil || !strings.Contains(err.Error(), "reuses fingerprint") {
		t.Fatalf("expected duplicate fingerprint failure, got %v", err)
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

func TestInitDBSetsFilePathBeforeMigrationSnapshot(t *testing.T) {
	if DB != nil {
		_ = CloseDB()
	}
	tempRoot := t.TempDir()
	databasePath := filepath.Join(tempRoot, "animate.db")
	previousPaths := config.AppPaths
	previousDBPath := CurrentDBPath
	config.AppPaths = config.Paths{DataDir: tempRoot}
	t.Cleanup(func() {
		_ = CloseDB()
		config.AppPaths = previousPaths
		CurrentDBPath = previousDBPath
	})

	if err := InitDBWithError(databasePath); err != nil {
		t.Fatalf("initialize file database: %v", err)
	}
	if CurrentDBPath != databasePath {
		t.Fatalf("expected current database path %q, got %q", databasePath, CurrentDBPath)
	}

	manifests, err := filepath.Glob(filepath.Join(tempRoot, "updates", "migration-snapshots", "*", "manifest.json"))
	if err != nil {
		t.Fatalf("find migration snapshots: %v", err)
	}
	if len(manifests) != 1 {
		t.Fatalf("expected one migration snapshot manifest, got %d", len(manifests))
	}
	payload, err := os.ReadFile(manifests[0])
	if err != nil {
		t.Fatalf("read migration snapshot manifest: %v", err)
	}
	var snapshot migrationSnapshotManifest
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		t.Fatalf("decode migration snapshot manifest: %v", err)
	}
	if snapshot.DatabasePath != "database.db" || len(snapshot.DatabaseSHA256) != sha256.Size*2 {
		t.Fatalf("snapshot did not include file database checksum: %+v", snapshot)
	}

	runPayload, err := os.ReadFile(filepath.Join(tempRoot, "updates", "migration-runs", "current.json")) // #nosec G304 -- path is confined to t.TempDir()
	if err != nil {
		t.Fatalf("read migration run manifest: %v", err)
	}
	var run migrationRunManifest
	if err := json.Unmarshal(runPayload, &run); err != nil {
		t.Fatalf("decode migration run manifest: %v", err)
	}
	if run.Status != migrationRunCompleted || run.SnapshotID == "" || len(run.SnapshotSHA256) != sha256.Size*2 {
		t.Fatalf("migration run manifest missing completed snapshot details: %+v", run)
	}
	for _, migrationID := range []string{migration009ID, migration015ID} {
		reportPayload, err := os.ReadFile(filepath.Join(tempRoot, "updates", "migration-reports", migrationID, "report.json")) // #nosec G304 -- migrationID is a fixed test constant under t.TempDir()
		if err != nil {
			t.Fatalf("read %s repair report: %v", migrationID, err)
		}
		var report migrationRepairReport
		if err := json.Unmarshal(reportPayload, &report); err != nil {
			t.Fatalf("decode %s repair report: %v", migrationID, err)
		}
		if report.Status != migrationRunCompleted || report.SnapshotID != run.SnapshotID || report.SnapshotSHA256 != run.SnapshotSHA256 {
			t.Fatalf("%s repair report is not tied to the committed migration snapshot: %+v", migrationID, report)
		}
	}
	if _, err := os.Stat(databasePath + ".migration.lock"); !os.IsNotExist(err) {
		t.Fatalf("migration lock should be released after startup, stat error=%v", err)
	}
}

func TestRunMigrationsRejectsRewrittenHistoryAndFutureSchema(t *testing.T) {
	target, err := gorm.Open(sqlite.Open(sqliteMemoryPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { closeTestDB(t, target) })
	if err := RunMigrations(target); err != nil {
		t.Fatalf("seed migrations: %v", err)
	}

	if err := target.Model(&SchemaMigration{}).
		Where("id = ?", migrations[0].ID).
		Update("checksum", strings.Repeat("0", sha256.Size*2)).Error; err != nil {
		t.Fatalf("rewrite checksum: %v", err)
	}
	if err := RunMigrations(target); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected rewritten migration to be rejected, got %v", err)
	}

	if err := target.Model(&SchemaMigration{}).
		Where("id = ?", migrations[0].ID).
		Update("checksum", migrations[0].Fingerprint).Error; err != nil {
		t.Fatalf("restore checksum: %v", err)
	}
	if err := target.Create(&SchemaMigration{ID: "999_future_schema", Sequence: 999, Description: "future"}).Error; err != nil {
		t.Fatalf("seed future schema: %v", err)
	}
	if err := RunMigrations(target); err == nil || !strings.Contains(err.Error(), "unknown or future") {
		t.Fatalf("expected future schema to be rejected, got %v", err)
	}
}

func TestRunMigrationsBackfillsChecksumBaselineOnlyOnce(t *testing.T) {
	target, err := gorm.Open(sqlite.Open(sqliteMemoryPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { closeTestDB(t, target) })
	if err := target.AutoMigrate(&SchemaMigration{}); err != nil {
		t.Fatalf("create migration history: %v", err)
	}
	for _, item := range migrations[:len(migrations)-1] {
		if err := target.Create(&SchemaMigration{ID: item.ID, Description: item.Description}).Error; err != nil {
			t.Fatalf("seed migration %s: %v", item.ID, err)
		}
	}
	if err := RunMigrations(target); err != nil {
		t.Fatalf("baseline migration: %v", err)
	}
	var missing int64
	if err := target.Model(&SchemaMigration{}).Where("checksum = '' OR checksum IS NULL").Count(&missing).Error; err != nil {
		t.Fatalf("count missing checksums: %v", err)
	}
	if missing != 0 {
		t.Fatalf("expected checksum baseline to be complete, missing=%d", missing)
	}
	if err := target.Model(&SchemaMigration{}).Where("id = ?", migrations[0].ID).Update("checksum", "").Error; err != nil {
		t.Fatalf("clear one checksum: %v", err)
	}
	if err := RunMigrations(target); err == nil || !strings.Contains(err.Error(), "partial checksum history") {
		t.Fatalf("expected partial checksum history to be rejected, got %v", err)
	}
}

func TestSubscriptionResourceMigrationBackfillsLegacyLogsIdempotently(t *testing.T) {
	target, err := gorm.Open(sqlite.Open(sqliteMemoryPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { closeTestDB(t, target) })
	if err := target.AutoMigrate(&model.Subscription{}, &model.DownloadLog{}); err != nil {
		t.Fatalf("migrate legacy tables: %v", err)
	}
	sub := model.Subscription{Title: "Legacy Resource Show", RSSUrl: "https://example.test/legacy-resource"}
	if err := target.Create(&sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	entry := model.DownloadLog{
		SubscriptionID: sub.ID,
		Title:          "[Group] Legacy Resource Show - 01",
		Magnet:         "magnet:?xt=urn:btih:legacy-resource-hash",
		Episode:        "01",
		SeasonVal:      "S01",
		Status:         completedResourceState,
		InfoHash:       "legacy-resource-hash",
		TargetFile:     "/media/legacy/01.mkv",
	}
	if err := target.Create(&entry).Error; err != nil {
		t.Fatalf("create legacy log: %v", err)
	}

	if err := backfillSubscriptionResources(target); err != nil {
		t.Fatalf("first resource backfill: %v", err)
	}
	if err := backfillSubscriptionResources(target); err != nil {
		t.Fatalf("second resource backfill: %v", err)
	}

	var resources []model.SubscriptionResource
	if err := target.Find(&resources).Error; err != nil {
		t.Fatalf("load resources: %v", err)
	}
	if len(resources) != 1 {
		t.Fatalf("expected one idempotent resource row, got %d", len(resources))
	}
	if resources[0].State != completedResourceState || resources[0].TargetFile != entry.TargetFile {
		t.Fatalf("unexpected resource backfill: %+v", resources[0])
	}
	if resources[0].CanonicalKey != "episode:1:1" {
		t.Fatalf("expected normalized canonical key, got %q", resources[0].CanonicalKey)
	}
	var updated model.DownloadLog
	if err := target.First(&updated, entry.ID).Error; err != nil {
		t.Fatalf("reload legacy log: %v", err)
	}
	if updated.ResourceID == nil || *updated.ResourceID != resources[0].ID {
		t.Fatalf("expected legacy log resource link, got %+v", updated.ResourceID)
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
		if item.ID == migration009ID {
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

func TestLocalAnimeIdentityMigrationMergesPopulatedDuplicates(t *testing.T) {
	target, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "local-anime-identity.db")), &gorm.Config{})
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
		if item.ID == migration015ID {
			break
		}
		if err := target.Create(&SchemaMigration{ID: item.ID, Description: item.Description}).Error; err != nil {
			t.Fatalf("seed migration %s: %v", item.ID, err)
		}
	}

	dir := model.LocalAnimeDirectory{Path: `E:\Bangumi`}
	if err := target.Create(&dir).Error; err != nil {
		t.Fatalf("create directory: %v", err)
	}
	metadata := model.AnimeMetadata{Title: "Transparent Night", AniListID: 202269}
	if err := target.Create(&metadata).Error; err != nil {
		t.Fatalf("create metadata: %v", err)
	}
	first := model.LocalAnime{Model: gorm.Model{ID: 229}, DirectoryID: dir.ID, Title: "Transparent Night", Path: `E:\Bangumi\与奔驰于透明之夜的你，谈一场看不见的恋爱。`, MetadataID: &metadata.ID, JellyfinSeriesID: "series-229"}
	second := model.LocalAnime{Model: gorm.Model{ID: 249}, DirectoryID: dir.ID, Title: "Transparent Night", Path: first.Path, FileCount: 3, TotalSize: 300, JellyfinSeriesID: "series-249"}
	if err := target.Create(&first).Error; err != nil {
		t.Fatalf("create first duplicate: %v", err)
	}
	if err := target.Create(&second).Error; err != nil {
		t.Fatalf("create second duplicate: %v", err)
	}
	episodes := []model.LocalEpisode{
		{LocalAnimeID: first.ID, EpisodeNum: 1, Path: first.Path + `\01.mkv`, FileSize: 100},
		{LocalAnimeID: second.ID, EpisodeNum: 2, Path: second.Path + `\02.mkv`, FileSize: 200},
	}
	if err := target.Create(&episodes).Error; err != nil {
		t.Fatalf("create episodes: %v", err)
	}
	history := model.PlaybackHistory{LocalAnimeID: second.ID, LocalEpisodeID: episodes[1].ID}
	if err := target.Create(&history).Error; err != nil {
		t.Fatalf("create playback history: %v", err)
	}
	issue := model.LibraryIssue{IssueKey: "scrape:249", LocalAnimeID: &second.ID}
	if err := target.Create(&issue).Error; err != nil {
		t.Fatalf("create library issue: %v", err)
	}

	if err := RunMigrations(target); err != nil {
		t.Fatalf("run identity migration: %v", err)
	}
	var rows []model.LocalAnime
	if err := target.Preload("Episodes").Find(&rows).Error; err != nil {
		t.Fatalf("load merged local anime: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != first.ID {
		t.Fatalf("expected populated survivor %d, got %+v", first.ID, rows)
	}
	if len(rows[0].Episodes) != 2 || rows[0].MetadataID == nil || rows[0].JellyfinSeriesID != "series-229" {
		t.Fatalf("merged fields were not preserved: %+v", rows[0])
	}
	var gotHistory model.PlaybackHistory
	if err := target.First(&gotHistory).Error; err != nil || gotHistory.LocalAnimeID != first.ID {
		t.Fatalf("playback history was not reassigned: %+v, %v", gotHistory, err)
	}
	var gotIssue model.LibraryIssue
	if err := target.First(&gotIssue).Error; err != nil || gotIssue.LocalAnimeID == nil || *gotIssue.LocalAnimeID != first.ID {
		t.Fatalf("library issue was not reassigned: %+v, %v", gotIssue, err)
	}
	if rows[0].ScanKey == nil || !strings.HasPrefix(*rows[0].ScanKey, "folder:") {
		t.Fatalf("expected stable scan key, got %v", rows[0].ScanKey)
	}
}
