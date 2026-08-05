//go:build historical_fixture_builder

package db

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// TestBuildHistoricalFixture is intentionally build-tagged. The release
// matrix script copies this test into a checked-out historical tag, asks that
// tag's migration engine to create a real SQLite file, and then asks the
// current source to upgrade the file in place.
func TestBuildHistoricalFixture(t *testing.T) {
	output := os.Getenv("ANIMATE_HISTORICAL_OUTPUT")
	input := os.Getenv("ANIMATE_HISTORICAL_INPUT")
	switch {
	case output != "":
		_ = os.Remove(output)
		if err := os.MkdirAll(filepath.Dir(output), 0700); err != nil {
			t.Fatal(err)
		}
		target, err := gorm.Open(sqlite.Open(output), &gorm.Config{})
		if err != nil {
			t.Fatal(err)
		}
		if err := RunMigrations(target); err != nil {
			t.Fatal(err)
		}
		seedHistoricalFixtureData(t, target)
		if err := target.Exec("PRAGMA quick_check").Error; err != nil {
			t.Fatal(err)
		}
		_ = closeHistoricalFixtureDB(target)
	case input != "":
		target, err := gorm.Open(sqlite.Open(input), &gorm.Config{})
		if err != nil {
			t.Fatal(err)
		}
		if err := RunMigrations(target); err != nil {
			t.Fatal(err)
		}
		verifyHistoricalFixtureData(t, target)
		if err := RunMigrations(target); err != nil {
			t.Fatal(err)
		}
		var quick string
		if err := target.Raw("PRAGMA quick_check").Scan(&quick).Error; err != nil {
			t.Fatal(err)
		}
		if quick != "" && quick != "ok" {
			t.Fatalf("quick_check=%q", quick)
		}
		_ = closeHistoricalFixtureDB(target)
	default:
		t.Skip("set ANIMATE_HISTORICAL_OUTPUT or ANIMATE_HISTORICAL_INPUT")
	}
}

func seedHistoricalFixtureData(t *testing.T, target *gorm.DB) {
	t.Helper()
	ids := make(map[string]uint)
	insert := func(table string, values map[string]any) uint {
		if !target.Migrator().HasTable(table) {
			return 0
		}
		filtered := make(map[string]any)
		for column, value := range values {
			if target.Migrator().HasColumn(table, column) {
				filtered[column] = value
			}
		}
		if err := target.Table(table).Create(filtered).Error; err != nil {
			t.Fatalf("seed %s: %v", table, err)
		}
		if !target.Migrator().HasColumn(table, "id") {
			return 0
		}
		var id uint
		if err := target.Table(table).Order("id desc").Limit(1).Pluck("id", &id).Error; err != nil {
			t.Fatalf("read seeded %s id: %v", table, err)
		}
		return id
	}
	ids["metadata"] = insert("anime_metadata", map[string]any{
		"title": "Historical Fixture Show", "summary": "fixture", "bangumi_id": 910099,
	})
	ids["subscription"] = insert("subscriptions", map[string]any{
		"title": "Historical Fixture Subscription", "rss_url": "https://example.invalid/fixture.xml",
		"metadata_id": ids["metadata"],
	})
	ids["user"] = insert("users", map[string]any{
		"username": "fixture-admin", "password_hash": "$2a$10$7EqJtq98hPqEX7fNZaFWoOeGm5B6f4x1Q4n6v2s1jJrKx8s3i3Q9u",
	})
	ids["directory"] = insert("local_anime_directories", map[string]any{"path": "/fixture-library"})
	ids["anime"] = insert("local_animes", map[string]any{
		"title": "Historical Fixture Show", "path": "/fixture-library/Historical Fixture Show",
		"directory_id": ids["directory"], "metadata_id": ids["metadata"],
	})
	ids["episode"] = insert("local_episodes", map[string]any{
		"title": "Historical Fixture Show - 01", "path": "/fixture-library/Historical Fixture Show/01.mkv",
		"local_anime_id": ids["anime"], "episode_num": 1, "season_num": 1,
	})
	insert("download_logs", map[string]any{
		"title": "Historical Fixture Show - 01", "subscription_id": ids["subscription"],
		"status": "completed", "info_hash": "fixture-hash", "target_file": "/fixture-library/Historical Fixture Show/01.mkv",
	})
	insert("subscription_run_logs", map[string]any{
		"subscription_id": ids["subscription"], "status": "success", "summary": "fixture run",
	})
	insert("subscription_resources", map[string]any{
		"subscription_id": ids["subscription"], "fingerprint": "fixture-resource",
		"title": "Historical Fixture Show - 01", "state": "completed",
	})
	insert("playback_histories", map[string]any{
		"user_id": ids["user"], "local_anime_id": ids["anime"], "local_episode_id": ids["episode"],
		"position_ticks": 100, "duration_ticks": 1000,
	})
	insert("library_issues", map[string]any{
		"issue_key": "fixture:1", "local_anime_id": ids["anime"], "status": "open",
	})
	if target.Migrator().HasTable("global_configs") {
		insert("global_configs", map[string]any{"key": "fixture_key", "value": "fixture_value"})
	}
}

func verifyHistoricalFixtureData(t *testing.T, target *gorm.DB) {
	t.Helper()
	for _, table := range []string{
		"subscriptions", "download_logs", "subscription_resources", "subscription_run_logs",
		"anime_metadata", "local_anime_directories", "local_animes", "local_episodes",
		"playback_histories", "library_issues", "users", "global_configs",
	} {
		if !target.Migrator().HasTable(table) {
			continue
		}
		var count int64
		if err := target.Table(table).Count(&count).Error; err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count == 0 {
			t.Fatalf("historical fixture table %s is empty", table)
		}
	}
}

func closeHistoricalFixtureDB(target *gorm.DB) error {
	sqlDB, err := target.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
