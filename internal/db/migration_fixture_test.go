package db

import (
	"testing"

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
