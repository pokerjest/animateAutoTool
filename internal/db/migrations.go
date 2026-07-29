package db

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/config"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/parser"
	appversion "github.com/pokerjest/animateAutoTool/internal/version"
	"gorm.io/gorm"
)

// SchemaMigration records each explicit schema/data migration that has been
// applied to a database. We keep this separate from app config so future
// releases can safely evolve table layouts and data fixes over time.
type SchemaMigration struct {
	ID          string    `gorm:"primaryKey;size:64"`
	Sequence    int       `gorm:"index"`
	Description string    `gorm:"size:255"`
	AppliedAt   time.Time `gorm:"index"`
}

type migration struct {
	ID          string
	Description string
	Apply       func(tx *gorm.DB) error
}

var migrations = []migration{
	{
		ID:          "001_initial_schema",
		Description: "Create and align the core application schema",
		Apply: func(tx *gorm.DB) error {
			return autoMigrateCoreSchema(tx)
		},
	},
	{
		ID:          "002_subscription_run_logs",
		Description: "Create per-run subscription history records",
		Apply: func(tx *gorm.DB) error {
			return tx.AutoMigrate(&model.SubscriptionRunLog{})
		},
	},
	{
		ID:          "003_subscription_strategy_fields",
		Description: "Add advanced subscription strategy fields",
		Apply: func(tx *gorm.DB) error {
			return tx.AutoMigrate(&model.Subscription{})
		},
	},
	{
		ID:          "004_audit_logs",
		Description: "Create audit_logs table for sensitive operation history",
		Apply: func(tx *gorm.DB) error {
			return tx.AutoMigrate(&model.AuditLog{})
		},
	},
	{
		ID:          "005_subscription_mikan_ids",
		Description: "Backfill missing Mikan identifiers from official RSS URLs",
		Apply:       backfillSubscriptionMikanIDs,
	},
	{
		ID:          "006_subscription_release_filters",
		Description: "Add resolution and subtitle language filters to subscriptions",
		Apply: func(tx *gorm.DB) error {
			return tx.AutoMigrate(&model.Subscription{})
		},
	},
	{
		ID:          "007_playback_history",
		Description: "Create per-user playback history for continue watching",
		Apply: func(tx *gorm.DB) error {
			return tx.AutoMigrate(&model.PlaybackHistory{})
		},
	},
	{
		ID:          "008_remove_redundant_mikan_subgroup_rules",
		Description: "Remove generated subgroup filters and aggregate fallbacks from subgroup-specific Mikan feeds",
		Apply:       removeRedundantMikanSubgroupRules,
	},
	{
		ID:          "009_partial_bangumi_id_unique_index",
		Description: "Allow multiple unmatched metadata rows while keeping real Bangumi identifiers unique",
		Apply:       createPartialBangumiIDUniqueIndex,
	},
	{
		ID:          "010_ai_tool_operations",
		Description: "Create AI proposals and append-only tool execution logs",
		Apply: func(tx *gorm.DB) error {
			return tx.AutoMigrate(&model.AIProposal{}, &model.AIToolRun{})
		},
	},
	{
		ID:          "011_schema_migration_sequence",
		Description: "Track stable numeric migration order for compatibility checks",
		Apply:       backfillSchemaMigrationSequences,
	},
}

func backfillSchemaMigrationSequences(tx *gorm.DB) error {
	if err := tx.AutoMigrate(&SchemaMigration{}); err != nil {
		return err
	}
	var rows []SchemaMigration
	if err := tx.Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		sequence := migrationSequence(row.ID)
		if sequence <= 0 {
			continue
		}
		if err := tx.Model(&SchemaMigration{}).
			Where("id = ?", row.ID).
			Update("sequence", sequence).Error; err != nil {
			return err
		}
	}
	return nil
}

func createPartialBangumiIDUniqueIndex(tx *gorm.DB) error {
	type duplicateBangumiID struct {
		BangumiID int
	}
	var duplicateIDs []duplicateBangumiID
	if err := tx.Unscoped().
		Model(&model.AnimeMetadata{}).
		Select("bangumi_id").
		Where("bangumi_id != 0").
		Group("bangumi_id").
		Having("COUNT(*) > 1").
		Scan(&duplicateIDs).Error; err != nil {
		return err
	}

	for _, duplicate := range duplicateIDs {
		var rows []model.AnimeMetadata
		if err := tx.Unscoped().
			Where("bangumi_id = ?", duplicate.BangumiID).
			Order("CASE WHEN deleted_at IS NULL THEN 0 ELSE 1 END").
			Order("id ASC").
			Find(&rows).Error; err != nil {
			return err
		}
		for i := 1; i < len(rows); i++ {
			updates := map[string]any{
				"bangumi_id":        0,
				"bangumi_title":     "",
				"bangumi_image":     "",
				"bangumi_summary":   "",
				"bangumi_rating":    0,
				"bangumi_image_raw": nil,
			}
			if rows[i].DataSource == "bangumi" {
				updates["data_source"] = ""
			}
			if err := tx.Unscoped().
				Model(&model.AnimeMetadata{}).
				Where("id = ?", rows[i].ID).
				Updates(updates).Error; err != nil {
				return err
			}
		}
	}

	if err := tx.Exec("DROP INDEX IF EXISTS idx_anime_metadata_bangumi_id").Error; err != nil {
		return err
	}
	return tx.Exec(
		"CREATE UNIQUE INDEX idx_anime_metadata_bangumi_id ON anime_metadata(bangumi_id) WHERE bangumi_id != 0",
	).Error
}

func removeRedundantMikanSubgroupRules(tx *gorm.DB) error {
	var subscriptions []model.Subscription
	if err := tx.Find(&subscriptions).Error; err != nil {
		return err
	}
	for i := range subscriptions {
		sub := &subscriptions[i]
		group := strings.TrimSpace(sub.SubtitleGroup)
		parsed, err := url.Parse(strings.TrimSpace(sub.RSSUrl))
		if group == "" || err != nil || !strings.EqualFold(parsed.Hostname(), "mikanani.me") || parsed.Query().Get("subgroupid") == "" {
			continue
		}

		changes := map[string]any{}
		filter := strings.TrimSpace(sub.FilterRule)
		if filter == group || filter == regexp.QuoteMeta(group) {
			changes["filter_rule"] = ""
		}

		query := parsed.Query()
		query.Del("subgroupid")
		parsed.RawQuery = query.Encode()
		if backup, backupErr := url.Parse(strings.TrimSpace(sub.BackupRSSUrl)); backupErr == nil && backup.String() == parsed.String() {
			changes["backup_rss_url"] = ""
		}

		if len(changes) > 0 {
			if err := tx.Model(&model.Subscription{}).Where("id = ?", sub.ID).Updates(changes).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func backfillSubscriptionMikanIDs(tx *gorm.DB) error {
	var subscriptions []model.Subscription
	if err := tx.Where("mikan_id IS NULL OR mikan_id = ?", "").Find(&subscriptions).Error; err != nil {
		return err
	}

	for i := range subscriptions {
		mikanID, ok := parser.MikanIDFromRSSURL(subscriptions[i].RSSUrl)
		if !ok {
			continue
		}
		if err := tx.Model(&model.Subscription{}).
			Where("id = ? AND (mikan_id IS NULL OR mikan_id = ?)", subscriptions[i].ID, "").
			Update("mikan_id", mikanID).Error; err != nil {
			return err
		}
	}
	return nil
}

func autoMigrateCoreSchema(tx *gorm.DB) error {
	return tx.AutoMigrate(
		&model.Subscription{},
		&model.DownloadLog{},
		&model.GlobalConfig{},
		&model.LocalAnimeDirectory{},
		&model.LocalAnime{},
		&model.LocalEpisode{},
		&model.LibraryIssue{},
		&model.AnimeMetadata{},
		&model.User{},
		&model.PlaybackHistory{},
		&model.AIProposal{},
		&model.AIToolRun{},
	)
}

// RunMigrations applies all known migrations in order and records each one in
// the schema_migrations table. New releases should append to the migrations
// slice instead of relying on ad-hoc AutoMigrate calls spread around the app.
func RunMigrations(target *gorm.DB) error {
	if err := validateMigrationOrder(); err != nil {
		return err
	}
	lock, err := acquireMigrationLock()
	if err != nil {
		return err
	}
	defer releaseMigrationLock(lock)

	if err := target.AutoMigrate(&SchemaMigration{}); err != nil {
		return fmt.Errorf("migrate schema_migrations table: %w", err)
	}
	applied := make(map[string]struct{}, len(migrations))
	var rows []SchemaMigration
	if err := target.Find(&rows).Error; err != nil {
		return fmt.Errorf("load applied schema migrations: %w", err)
	}
	for _, row := range rows {
		applied[row.ID] = struct{}{}
	}
	needsMigration := false
	for _, m := range migrations {
		if _, ok := applied[m.ID]; !ok {
			needsMigration = true
			break
		}
	}
	if needsMigration {
		if err := createMigrationSnapshot(target); err != nil {
			return fmt.Errorf("create migration snapshot: %w", err)
		}
	}

	for sequence, m := range migrations {
		if _, ok := applied[m.ID]; ok {
			continue
		}

		if err := target.Transaction(func(tx *gorm.DB) error {
			if err := m.Apply(tx); err != nil {
				return err
			}

			return tx.Create(&SchemaMigration{
				ID:          m.ID,
				Sequence:    sequence + 1,
				Description: m.Description,
				AppliedAt:   time.Now().UTC(),
			}).Error
		}); err != nil {
			return fmt.Errorf("apply migration %s: %w", m.ID, err)
		}
	}

	return nil
}

func CurrentSchemaVersion(target *gorm.DB) string {
	var row SchemaMigration
	if err := target.Order("sequence desc").Order("applied_at desc").First(&row).Error; err != nil {
		return ""
	}
	return row.ID
}

type migrationLock struct {
	path string
	file *os.File
}

func acquireMigrationLock() (migrationLock, error) {
	if CurrentDBPath == "" || strings.HasPrefix(CurrentDBPath, "file:") || CurrentDBPath == ":memory:" {
		return migrationLock{}, nil
	}
	path := CurrentDBPath + ".migration.lock"
	deadline := time.Now().Add(30 * time.Second)
	for {
		file, err := os.OpenFile(filepath.Clean(path), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err == nil {
			payload := fmt.Sprintf("pid=%d\ncreated_at=%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339Nano))
			_, _ = file.WriteString(payload)
			_ = file.Sync()
			return migrationLock{path: path, file: file}, nil
		}
		if !os.IsExist(err) {
			return migrationLock{}, fmt.Errorf("create migration lock: %w", err)
		}
		if info, statErr := os.Stat(path); statErr == nil && time.Since(info.ModTime()) > 10*time.Minute {
			if removeErr := os.Remove(path); removeErr == nil {
				continue
			}
		}
		if time.Now().After(deadline) {
			return migrationLock{}, fmt.Errorf("migration lock is held: %s", path)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func releaseMigrationLock(lock migrationLock) {
	if lock.file != nil {
		_ = lock.file.Close()
	}
	if lock.path != "" {
		_ = os.Remove(lock.path)
	}
}

type migrationSnapshotManifest struct {
	FormatVersion     int       `json:"format_version"`
	AppVersion        string    `json:"app_version"`
	OperationType     string    `json:"operation_type"`
	RollbackSupported bool      `json:"rollback_supported"`
	CreatedAt         time.Time `json:"created_at"`
	DatabasePath      string    `json:"database_path"`
	SchemaVersion     string    `json:"schema_version,omitempty"`
}

func createMigrationSnapshot(target *gorm.DB) error {
	if CurrentDBPath == "" || CurrentDBPath == ":memory:" {
		return nil
	}
	if _, err := os.Stat(CurrentDBPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	root := config.DataPath("updates", "migration-snapshots")
	if err := os.MkdirAll(root, 0700); err != nil {
		return err
	}
	now := time.Now().UTC()
	id := now.Format("20060102T150405.000000000Z")
	dir := filepath.Join(root, id)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	databasePath := filepath.Join(dir, "database.db")
	if err := target.Exec("VACUUM INTO ?", databasePath).Error; err != nil {
		_ = os.RemoveAll(dir)
		return err
	}
	payload, _ := json.MarshalIndent(migrationSnapshotManifest{
		FormatVersion:     1,
		AppVersion:        appversion.AppVersion,
		OperationType:     "migration",
		RollbackSupported: true,
		CreatedAt:         now,
		DatabasePath:      filepath.Base(databasePath),
		SchemaVersion:     CurrentSchemaVersion(target),
	}, "", "  ")
	return os.WriteFile(filepath.Join(dir, "manifest.json"), append(payload, '\n'), 0600)
}

func migrationSequence(id string) int {
	raw := id
	if index := strings.IndexByte(raw, '_'); index >= 0 {
		raw = raw[:index]
	}
	value, _ := strconv.Atoi(raw)
	return value
}

func validateMigrationOrder() error {
	seen := make(map[string]struct{}, len(migrations))
	lastSequence := 0
	for _, item := range migrations {
		if _, ok := seen[item.ID]; ok {
			return fmt.Errorf("duplicate migration id: %s", item.ID)
		}
		seen[item.ID] = struct{}{}
		sequence := migrationSequence(item.ID)
		if sequence <= lastSequence {
			return fmt.Errorf("migration %s is not in strictly increasing numeric order", item.ID)
		}
		lastSequence = sequence
	}
	return nil
}
