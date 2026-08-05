package db

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"gorm.io/gorm"
)

// TestHistoricalFixturesUpgradeToCurrent exercises the upgrade matrix used by
// the release process. The fixture checkpoints mirror the schema generations
// shipped as v0.6 through v0.9; each is deliberately upgraded in a fresh
// database so a migration can never rely on a later fixture having run first.
func TestHistoricalFixturesUpgradeToCurrent(t *testing.T) {
	checkpoints := []struct {
		name  string
		count int
	}{
		{name: "v0.6", count: 0},
		{name: "v0.7", count: 4},
		{name: "v0.8", count: 7},
		{name: "v0.9", count: 10},
	}

	originalDBPath := CurrentDBPath
	CurrentDBPath = sqliteMemoryPath
	t.Cleanup(func() { CurrentDBPath = originalDBPath })

	for _, fixture := range checkpoints {
		t.Run(fixture.name, func(t *testing.T) {
			target, err := gorm.Open(sqlite.Open(sqliteMemoryPath), &gorm.Config{})
			if err != nil {
				t.Fatalf("open fixture database: %v", err)
			}
			t.Cleanup(func() {
				sqlDB, dbErr := target.DB()
				if dbErr == nil {
					_ = sqlDB.Close()
				}
			})

			if fixture.count == 0 {
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
					t.Fatalf("create v0.6 fixture: %v", err)
				}
			} else {
				if err := target.AutoMigrate(&SchemaMigration{}); err != nil {
					t.Fatalf("create fixture migration history: %v", err)
				}
				for index := 0; index < fixture.count; index++ {
					item := migrations[index]
					if err := item.Apply(target); err != nil {
						t.Fatalf("apply fixture migration %s: %v", item.ID, err)
					}
					if err := target.Create(&SchemaMigration{ID: item.ID, Description: item.Description}).Error; err != nil {
						t.Fatalf("record fixture migration %s: %v", item.ID, err)
					}
				}
			}

			if err := RunMigrations(target); err != nil {
				t.Fatalf("upgrade %s fixture: %v", fixture.name, err)
			}
			if got := CurrentSchemaVersion(target); got != migrations[len(migrations)-1].ID {
				t.Fatalf("expected current schema %q, got %q", migrations[len(migrations)-1].ID, got)
			}
			var rows []SchemaMigration
			if err := target.Order("sequence").Find(&rows).Error; err != nil {
				t.Fatalf("read migration history: %v", err)
			}
			if len(rows) != len(migrations) {
				t.Fatalf("expected %d migration rows, got %d", len(migrations), len(rows))
			}
			for index, row := range rows {
				if row.Sequence != index+1 {
					t.Fatalf("migration %s sequence=%d, want %d", row.ID, row.Sequence, index+1)
				}
			}

			for _, value := range []any{
				&model.Subscription{},
				&model.GlobalConfig{},
				&model.LocalAnime{},
				&model.LocalEpisode{},
				&model.PlaybackHistory{},
				&model.AIProposal{},
				&model.AIToolRun{},
			} {
				if !target.Migrator().HasTable(value) {
					t.Fatalf("fixture %s is missing table for %T", fixture.name, value)
				}
			}
		})
	}
}

func TestAnimeMetadataExtendedFieldsMigrationRepairsLegacyTable(t *testing.T) {
	target, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "metadata-legacy.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, dbErr := target.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})

	// This is representative of a database that has run the original core
	// schema migration but predates the extended metadata fields.
	if err := target.Exec(`
		CREATE TABLE anime_metadata (
			id integer primary key autoincrement,
			created_at datetime,
			updated_at datetime,
			deleted_at datetime,
			title text,
			title_cn text,
			title_en text,
			title_jp text,
			bangumi_id integer default 0,
			tmdb_id integer default 0,
			anilist_id integer default 0,
			data_source text default 'jellyfin'
		)
	`).Error; err != nil {
		t.Fatalf("create legacy anime_metadata table: %v", err)
	}
	if err := target.Exec(
		"INSERT INTO anime_metadata (title, bangumi_id, tmdb_id, anilist_id, data_source) VALUES (?, 0, 0, 0, ?)",
		"保留的旧数据",
		"jellyfin",
	).Error; err != nil {
		t.Fatalf("seed legacy metadata row: %v", err)
	}
	if err := target.AutoMigrate(&SchemaMigration{}); err != nil {
		t.Fatalf("create migration history: %v", err)
	}
	for _, item := range migrations {
		if item.ID == "014_anime_metadata_extended_fields" {
			break
		}
		if err := target.Create(&SchemaMigration{
			ID:          item.ID,
			Sequence:    migrationSequence(item.ID),
			Description: item.Description,
			AppliedAt:   time.Now().UTC(),
		}).Error; err != nil {
			t.Fatalf("seed migration %s: %v", item.ID, err)
		}
	}

	originalDBPath := CurrentDBPath
	CurrentDBPath = sqliteMemoryPath
	t.Cleanup(func() { CurrentDBPath = originalDBPath })
	if err := RunMigrations(target); err != nil {
		t.Fatalf("run metadata extension migration: %v", err)
	}

	for _, field := range []string{
		"sort_title",
		"original_title",
		"genres",
		"studios",
		"tags",
		"actors",
		"directors",
		"runtime_minutes",
		"content_rating",
		"original_country",
		"tmdb_backdrop",
		"tmdb_backdrop_raw",
		"field_sources",
	} {
		if !target.Migrator().HasColumn(&model.AnimeMetadata{}, field) {
			t.Fatalf("expected anime_metadata.%s to be added", field)
		}
	}
	var metadata model.AnimeMetadata
	if err := target.First(&metadata).Error; err != nil {
		t.Fatalf("load legacy metadata after migration: %v", err)
	}
	if metadata.Title != "保留的旧数据" {
		t.Fatalf("legacy metadata was not preserved: %+v", metadata)
	}
	if got := CurrentSchemaVersion(target); got != migrations[len(migrations)-1].ID {
		t.Fatalf("expected current schema %q, got %q", migrations[len(migrations)-1].ID, got)
	}

	// A second run must not attempt to add the columns again.
	if err := RunMigrations(target); err != nil {
		t.Fatalf("rerun metadata extension migration: %v", err)
	}
}

func TestRunMigrationsRepairsExtendedFieldsWhenMigrationWasAlreadyRecorded(t *testing.T) {
	target, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "metadata-recorded.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, dbErr := target.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})

	if err := target.AutoMigrate(&model.AnimeMetadata{}, &SchemaMigration{}); err != nil {
		t.Fatalf("create metadata and migration tables: %v", err)
	}
	for _, field := range []string{"sort_title", "original_title", "genres", "studios", "tags", "actors", "directors", "runtime_minutes", "content_rating", "original_country", "tmdb_backdrop", "tmdb_backdrop_raw", "field_sources"} {
		if err := target.Migrator().DropColumn(&model.AnimeMetadata{}, field); err != nil {
			t.Fatalf("drop %s from simulated broken database: %v", field, err)
		}
	}
	for _, item := range migrations {
		if err := target.Create(&SchemaMigration{
			ID:          item.ID,
			Sequence:    migrationSequence(item.ID),
			Description: item.Description,
			AppliedAt:   time.Now().UTC(),
		}).Error; err != nil {
			t.Fatalf("seed migration %s: %v", item.ID, err)
		}
	}

	originalDBPath := CurrentDBPath
	CurrentDBPath = sqliteMemoryPath
	t.Cleanup(func() { CurrentDBPath = originalDBPath })
	if target.Migrator().HasColumn(&model.AnimeMetadata{}, "sort_title") {
		t.Fatal("test fixture unexpectedly contains sort_title before repair")
	}
	if err := RunMigrations(target); err != nil {
		t.Fatalf("run invariant repair: %v", err)
	}
	for _, field := range []string{"sort_title", "original_title", "genres", "studios", "tags", "actors", "directors", "runtime_minutes", "content_rating", "original_country", "tmdb_backdrop", "tmdb_backdrop_raw", "field_sources"} {
		if !target.Migrator().HasColumn(&model.AnimeMetadata{}, field) {
			t.Fatalf("expected repaired anime_metadata.%s column", field)
		}
	}
	if got := CurrentSchemaVersion(target); got != migrations[len(migrations)-1].ID {
		t.Fatalf("schema version changed during invariant repair: %q", got)
	}
}
