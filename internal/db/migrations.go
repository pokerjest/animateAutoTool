package db

import (
	"crypto/sha256"
	"encoding/hex"
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

const completedResourceState = "completed"

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
	{
		ID:          "012_subscription_resources",
		Description: "Create durable RSS candidate reconciliation records and backfill legacy download logs",
		Apply:       backfillSubscriptionResources,
	},
	{
		ID:          "013_local_media_parse_metadata",
		Description: "Store rich local media parsing evidence and incremental scan fingerprints",
		Apply: func(tx *gorm.DB) error {
			return tx.AutoMigrate(&model.LocalEpisode{})
		},
	},
	{
		ID:          "014_anime_metadata_extended_fields",
		Description: "Add extended metadata fields introduced by the local media scraper",
		Apply:       migrateAnimeMetadataExtendedFields,
	},
}

// migrateAnimeMetadataExtendedFields repairs databases created before the
// extended AnimeMetadata fields were added to the model. Those databases can
// legitimately have all explicit migrations through 013 recorded while still
// missing columns such as sort_title, because the fields were introduced after
// the original core-schema migration. Keep this migration focused on the
// affected table so it is safe to run against every supported historical
// database and harmless when a column already exists.
func migrateAnimeMetadataExtendedFields(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&model.AnimeMetadata{}) {
		return tx.AutoMigrate(&model.AnimeMetadata{})
	}

	for _, field := range []string{
		"SortTitle",
		"OriginalTitle",
		"Genres",
		"Studios",
		"Tags",
		"Actors",
		"Directors",
		"RuntimeMinutes",
		"ContentRating",
		"OriginalCountry",
		"TMDBBackdrop",
		"TMDBBackdropRaw",
		"FieldSources",
	} {
		if tx.Migrator().HasColumn(&model.AnimeMetadata{}, field) {
			continue
		}
		if err := tx.Migrator().AddColumn(&model.AnimeMetadata{}, field); err != nil {
			return fmt.Errorf("add anime_metadata.%s: %w", field, err)
		}
	}
	return nil
}

// ensureAnimeMetadataExtendedFields is deliberately called on every startup,
// not only when migration 014 is newly recorded. A previous build could have
// recorded 014 before all of its columns were introduced, leaving an otherwise
// "current" database without sort_title and the other scraper fields. Schema
// history is useful for compatibility decisions, but the actual table shape
// remains the source of truth for this idempotent repair.
func ensureAnimeMetadataExtendedFields(target *gorm.DB) error {
	if target == nil {
		return nil
	}
	if err := migrateAnimeMetadataExtendedFields(target); err != nil {
		return fmt.Errorf("ensure anime_metadata extended fields: %w", err)
	}
	return nil
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
		&model.SubscriptionResource{},
	)
}

// backfillSubscriptionResources creates one durable candidate record for each
// legacy download log. The migration is intentionally idempotent: a rerun
// reuses the same subscription/fingerprint pair and only fills the missing
// resource_id projection on the old log table.
func backfillSubscriptionResources(tx *gorm.DB) error {
	if err := tx.AutoMigrate(&model.SubscriptionResource{}, &model.DownloadLog{}); err != nil {
		return err
	}

	var logs []model.DownloadLog
	if err := tx.Find(&logs).Error; err != nil {
		return err
	}
	for i := range logs {
		entry := &logs[i]
		fingerprint := legacyResourceFingerprint(entry)
		if fingerprint == "" || entry.SubscriptionID == 0 {
			continue
		}
		canonical := legacyResourceCanonicalKey(entry)
		state := legacyResourceState(entry.Status)
		var resource model.SubscriptionResource
		err := tx.Where("subscription_id = ? AND fingerprint = ?", entry.SubscriptionID, fingerprint).
			First(&resource).Error
		switch err {
		case nil:
			updates := map[string]any{
				"canonical_key": canonical,
				"title":         entry.Title,
				"episode":       entry.Episode,
				"season_val":    entry.SeasonVal,
				"info_hash":     entry.InfoHash,
				"target_file":   entry.TargetFile,
				"state":         state,
			}
			if err := tx.Model(&model.SubscriptionResource{}).
				Where("id = ?", resource.ID).
				Updates(updates).Error; err != nil {
				return err
			}
		case gorm.ErrRecordNotFound:
			resource = model.SubscriptionResource{
				SubscriptionID: entry.SubscriptionID,
				CanonicalKey:   canonical,
				Fingerprint:    fingerprint,
				Title:          entry.Title,
				Episode:        entry.Episode,
				SeasonVal:      entry.SeasonVal,
				TorrentURL:     entry.Magnet,
				InfoHash:       entry.InfoHash,
				Source:         "legacy",
				State:          state,
				TargetFile:     entry.TargetFile,
				Selected:       true,
			}
			if state == completedResourceState {
				now := entry.UpdatedAt
				if now.IsZero() {
					now = entry.CreatedAt
				}
				resource.CompletedAt = &now
			}
			if err := tx.Create(&resource).Error; err != nil {
				return err
			}
		default:
			return err
		}

		if resource.ID == 0 {
			if err := tx.Where("subscription_id = ? AND fingerprint = ?", entry.SubscriptionID, fingerprint).
				First(&resource).Error; err != nil {
				return err
			}
		}
		if entry.ResourceID == nil || *entry.ResourceID != resource.ID {
			if err := tx.Model(&model.DownloadLog{}).
				Where("id = ?", entry.ID).
				Update("resource_id", resource.ID).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func legacyResourceFingerprint(entry *model.DownloadLog) string {
	if entry == nil {
		return ""
	}
	raw := strings.TrimSpace(entry.InfoHash)
	if raw == "" {
		raw = strings.TrimSpace(entry.Magnet)
	}
	if raw == "" {
		raw = strings.Join([]string{
			strings.ToLower(strings.TrimSpace(entry.Title)),
			strings.ToLower(strings.TrimSpace(entry.SeasonVal)),
			strings.ToLower(strings.TrimSpace(entry.Episode)),
		}, "|")
	}
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func legacyResourceCanonicalKey(entry *model.DownloadLog) string {
	if entry == nil {
		return ""
	}
	season := parser.NormalizeSeasonNumber(entry.SeasonVal)
	if season == "" {
		season = parser.SeasonNumberFromTitle(entry.Title)
	}
	episode := parser.NormalizeEpisodeNumber(entry.Episode)
	if episode == "" {
		episode = parser.EpisodeNumberFromTitle(entry.Title)
	}
	if season == "" {
		season = "1"
	}
	if episode == "" {
		if title := parser.NormalizeReleaseTitle(entry.Title); title != "" {
			return "title:" + title
		}
		return ""
	}
	return fmt.Sprintf("episode:%s:%s", season, episode)
}

func legacyResourceState(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed", "renamed":
		return "completed"
	case "downloading":
		return "downloading"
	case "failed":
		return "failed"
	case "archived":
		return "archived"
	default:
		return "unknown"
	}
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
	// A sequence backfill may itself have been marked applied by an older
	// database before this code learned to persist numeric order. Keep the
	// compatibility column correct even when that migration is already present.
	if err := backfillSchemaMigrationSequences(target); err != nil {
		return fmt.Errorf("backfill schema migration sequences: %w", err)
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

	// Keep this invariant check outside the "new migration" branch. It repairs
	// databases from builds that marked 014 as applied while still missing one
	// or more columns, without requiring users to delete migration history.
	if err := ensureAnimeMetadataExtendedFields(target); err != nil {
		return err
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

func LatestSchemaVersion() string {
	if len(migrations) == 0 {
		return ""
	}
	return migrations[len(migrations)-1].ID
}

type migrationLock struct {
	path string
	file *os.File
}

func acquireMigrationLock() (migrationLock, error) {
	if CurrentDBPath == "" || strings.HasPrefix(CurrentDBPath, "file:") || CurrentDBPath == sqliteMemoryPath {
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
	if CurrentDBPath == "" || CurrentDBPath == sqliteMemoryPath {
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
