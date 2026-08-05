package db

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"sort"
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

// DatabaseFormat describes the on-disk container/backup contract, while
// SchemaFormat describes the additive migration protocol. They intentionally
// evolve independently from the latest migration ID.
const (
	DatabaseFormat = 1
	SchemaFormat   = 1
)

// SchemaMigration records each explicit schema/data migration that has been
// applied to a database. We keep this separate from app config so future
// releases can safely evolve table layouts and data fixes over time.
type SchemaMigration struct {
	ID          string    `gorm:"primaryKey;size:64"`
	Sequence    int       `gorm:"index"`
	Description string    `gorm:"size:255"`
	Checksum    string    `gorm:"size:64;index"`
	AppliedAt   time.Time `gorm:"index"`
}

type migration struct {
	ID          string
	Description string
	// Fingerprint is intentionally explicit and immutable once a migration is
	// released. It is persisted in schema_migrations and lets startup reject
	// rewritten historical migrations before they can touch user data.
	Fingerprint string
	Apply       func(tx *gorm.DB) error
}

var migrations = []migration{
	{
		ID:          "001_initial_schema",
		Description: "Create and align the core application schema",
		Fingerprint: "c560ed55351c503eb8b3636711bae79c983aceb31966f7d233d786e249fccc5e",
		Apply: func(tx *gorm.DB) error {
			return autoMigrateCoreSchema(tx)
		},
	},
	{
		ID:          "002_subscription_run_logs",
		Description: "Create per-run subscription history records",
		Fingerprint: "07abcdda01b2e5c761a65374e7edd719956c4ff2542d536d03c2b66439f814a5",
		Apply: func(tx *gorm.DB) error {
			return tx.AutoMigrate(&model.SubscriptionRunLog{})
		},
	},
	{
		ID:          "003_subscription_strategy_fields",
		Description: "Add advanced subscription strategy fields",
		Fingerprint: "41c53d3d91e93eee42072deecdd93bd53485886eb708123326659b5326272852",
		Apply: func(tx *gorm.DB) error {
			return tx.AutoMigrate(&model.Subscription{})
		},
	},
	{
		ID:          "004_audit_logs",
		Description: "Create audit_logs table for sensitive operation history",
		Fingerprint: "3a1335dd98e7a6fb114d4444c1294c5db066dff4eae52431652da55471f99cd3",
		Apply: func(tx *gorm.DB) error {
			return tx.AutoMigrate(&model.AuditLog{})
		},
	},
	{
		ID:          "005_subscription_mikan_ids",
		Description: "Backfill missing Mikan identifiers from official RSS URLs",
		Fingerprint: "df6df2b378dc6ad1debc6fb85bc815cb30ce461c90da0f5609ba5a29c21b50a4",
		Apply:       backfillSubscriptionMikanIDs,
	},
	{
		ID:          "006_subscription_release_filters",
		Description: "Add resolution and subtitle language filters to subscriptions",
		Fingerprint: "cf86c6ad68cb8ada871041b193e49b725b55d60ca910fa6a97707d702f4f5d32",
		Apply: func(tx *gorm.DB) error {
			return tx.AutoMigrate(&model.Subscription{})
		},
	},
	{
		ID:          "007_playback_history",
		Description: "Create per-user playback history for continue watching",
		Fingerprint: "dafb2e2c33080625863b5b1a1c2e0bb45d07cb2e741bc1cfb46eada26a545965",
		Apply: func(tx *gorm.DB) error {
			return tx.AutoMigrate(&model.PlaybackHistory{})
		},
	},
	{
		ID:          "008_remove_redundant_mikan_subgroup_rules",
		Description: "Remove generated subgroup filters and aggregate fallbacks from subgroup-specific Mikan feeds",
		Fingerprint: "23358000f1c58f8a2e75fb7a5877eabbb12185c90d77e8c5bd340003f122ebba",
		Apply:       removeRedundantMikanSubgroupRules,
	},
	{
		ID:          migration009ID,
		Description: "Allow multiple unmatched metadata rows while keeping real Bangumi identifiers unique",
		Fingerprint: "9a8fe467719bfd95ed24bbcf411e65e50648cf38ad6ae69035f346b302d04d6a",
		Apply:       createPartialBangumiIDUniqueIndex,
	},
	{
		ID:          "010_ai_tool_operations",
		Description: "Create AI proposals and append-only tool execution logs",
		Fingerprint: "2a936b05e8b90f0dee156ab02dba92c77e234e502eff6300ed01759242fb741d",
		Apply: func(tx *gorm.DB) error {
			return tx.AutoMigrate(&model.AIProposal{}, &model.AIToolRun{})
		},
	},
	{
		ID:          "011_schema_migration_sequence",
		Description: "Track stable numeric migration order for compatibility checks",
		Fingerprint: "c242a03956f9761c7f277100598f9791640fc8b7c7836b1da3cedf12e8983656",
		Apply:       backfillSchemaMigrationSequences,
	},
	{
		ID:          "012_subscription_resources",
		Description: "Create durable RSS candidate reconciliation records and backfill legacy download logs",
		Fingerprint: "f78e430b7539bc70f9fb570ed8d4308225f9077b1ad12a34b47fee640bdd16d1",
		Apply:       backfillSubscriptionResources,
	},
	{
		ID:          "013_local_media_parse_metadata",
		Description: "Store rich local media parsing evidence and incremental scan fingerprints",
		Fingerprint: "a71d4eb059d9ef2fbff1ad70ad163ca32a81fd1f14d3d7af8c1119afd2e60243",
		Apply: func(tx *gorm.DB) error {
			return tx.AutoMigrate(&model.LocalEpisode{})
		},
	},
	{
		ID:          "014_anime_metadata_extended_fields",
		Description: "Add extended metadata fields introduced by the local media scraper",
		Fingerprint: "dd918d878954c1693b9f1a72c0038c210622cf713e1bc48044cae9f07d592071",
		Apply:       migrateAnimeMetadataExtendedFields,
	},
	{
		ID:          migration015ID,
		Description: "Merge duplicate folder series and enforce stable local anime identities",
		Fingerprint: "c5ff054ac73a3cdb1e192b86c20fb9dd3fa4cc3c8bf4a6820724f2b4f3cde9f0",
		Apply:       migrateLocalAnimeIdentity,
	},
}

const (
	localAnimeIdentityIndexName = "idx_local_anime_scan_key"
	migration009ID              = "009_partial_bangumi_id_unique_index"
	migration015ID              = "015_local_anime_identity"
	migrationRunFailed          = "failed"
	migrationRunCompleted       = "completed"
)

// migrateLocalAnimeIdentity repairs historical duplicate folder rows before
// adding the unique index. Loose-file series use the library root as their
// path and deliberately keep a NULL identity so they can coexist.
func migrateLocalAnimeIdentity(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&model.LocalAnime{}) {
		return nil
	}
	if !tx.Migrator().HasColumn(&model.LocalAnime{}, "ScanKey") {
		if err := tx.Migrator().AddColumn(&model.LocalAnime{}, "ScanKey"); err != nil {
			return fmt.Errorf("add local_animes.scan_key: %w", err)
		}
	}
	if err := repairLocalAnimeIdentity(tx); err != nil {
		return err
	}
	return ensureLocalAnimeIdentityIndex(tx)
}

func prepareLocalAnimeIdentityRepair(tx *gorm.DB, snapshot migrationSnapshotManifest) (*migrationRepairReport, error) {
	if !tx.Migrator().HasTable(&model.LocalAnime{}) {
		return nil, nil
	}
	if !tx.Migrator().HasColumn(&model.LocalAnime{}, "ScanKey") {
		if err := tx.Migrator().AddColumn(&model.LocalAnime{}, "ScanKey"); err != nil {
			return nil, fmt.Errorf("add local_animes.scan_key: %w", err)
		}
	}
	report := migrationRepairReport{
		FormatVersion:    1,
		MigrationID:      migration015ID,
		DatabaseVersion:  CurrentSchemaVersion(tx),
		SnapshotID:       snapshot.ID,
		SnapshotSHA256:   snapshot.DatabaseSHA256,
		StartedAt:        time.Now().UTC(),
		Status:           migrationRunCompleted,
		RelatedRowsMoved: map[string]int{},
		FieldMergeCounts: map[string]int{},
	}
	if err := repairLocalAnimeIdentityWithReport(tx, &report); err != nil {
		report.Status = migrationRunFailed
		report.Error = err.Error()
		return &report, err
	}
	now := time.Now().UTC()
	report.CompletedAt = &now
	return &report, nil
}

// RepairLocalAnimeIdentity is also used after restoring legacy backups, which
// may contain duplicate rows and may not have the scan_key column at all.
func RepairLocalAnimeIdentity(tx *gorm.DB) error {
	if tx == nil || !tx.Migrator().HasTable(&model.LocalAnime{}) {
		return nil
	}
	if !tx.Migrator().HasColumn(&model.LocalAnime{}, "ScanKey") {
		if err := tx.Migrator().AddColumn(&model.LocalAnime{}, "ScanKey"); err != nil {
			return fmt.Errorf("add local_animes.scan_key: %w", err)
		}
	}
	if err := repairLocalAnimeIdentity(tx); err != nil {
		return err
	}
	return ensureLocalAnimeIdentityIndex(tx)
}

func ensureLocalAnimeIdentityIndex(tx *gorm.DB) error {
	if err := DropLocalAnimeIdentityIndex(tx); err != nil {
		return err
	}
	return tx.Exec(
		"CREATE UNIQUE INDEX " + localAnimeIdentityIndexName +
			" ON local_animes(scan_key) WHERE deleted_at IS NULL AND scan_key IS NOT NULL AND scan_key != ''",
	).Error
}

// DropLocalAnimeIdentityIndex temporarily removes the identity constraint while
// a legacy restore is loading and consolidating duplicate rows.
func DropLocalAnimeIdentityIndex(tx *gorm.DB) error {
	if tx == nil {
		return gorm.ErrInvalidDB
	}
	for _, name := range []string{localAnimeIdentityIndexName, "idx_local_animes_scan_key"} {
		if err := tx.Exec("DROP INDEX IF EXISTS " + name).Error; err != nil {
			return err
		}
	}
	return nil
}

func repairLocalAnimeIdentity(tx *gorm.DB) error {
	return repairLocalAnimeIdentityWithReport(tx, nil)
}

func repairLocalAnimeIdentityWithReport(tx *gorm.DB, report *migrationRepairReport) error {
	var directories []model.LocalAnimeDirectory
	if tx.Migrator().HasTable(&model.LocalAnimeDirectory{}) {
		if err := tx.Unscoped().Find(&directories).Error; err != nil {
			return fmt.Errorf("load local anime directories: %w", err)
		}
	}
	rootByID := make(map[uint]string, len(directories))
	for _, directory := range directories {
		rootByID[directory.ID] = migrationPathKey(directory.Path)
	}

	var animes []model.LocalAnime
	query := tx.Unscoped().Where("deleted_at IS NULL")
	if tx.Migrator().HasTable(&model.LocalEpisode{}) {
		query = query.Preload("Episodes")
	}
	if err := query.Find(&animes).Error; err != nil {
		return fmt.Errorf("load local anime rows: %w", err)
	}
	groups := make(map[string][]*model.LocalAnime)
	for i := range animes {
		anime := &animes[i]
		key := migrationLocalAnimeKey(anime.Path, rootByID[anime.DirectoryID])
		if key == "" {
			if err := tx.Model(&model.LocalAnime{}).Where("id = ?", anime.ID).Update("scan_key", nil).Error; err != nil {
				return err
			}
			continue
		}
		if err := tx.Model(&model.LocalAnime{}).Where("id = ?", anime.ID).Update("scan_key", key).Error; err != nil {
			return err
		}
		groups[key] = append(groups[key], anime)
	}

	for _, group := range groups {
		if len(group) < 2 {
			continue
		}
		sortLocalAnimeIdentityGroup(group)
		survivor := group[0]
		if report != nil {
			report.SurvivorCount++
			report.DuplicateCount += len(group) - 1
		}
		for _, duplicate := range group[1:] {
			if report != nil {
				report.Mappings = append(report.Mappings, migrationRepairMapping{SourceID: duplicate.ID, SurvivorID: survivor.ID})
				report.AffectedRows++
			}
			if err := mergeLocalAnimeIdentityRowsWithReport(tx, survivor, duplicate, report); err != nil {
				return err
			}
		}
	}
	return nil
}

func sortLocalAnimeIdentityGroup(group []*model.LocalAnime) {
	sort.SliceStable(group, func(i, j int) bool {
		left, right := group[i], group[j]
		if (len(left.Episodes) > 0) != (len(right.Episodes) > 0) {
			return len(left.Episodes) > 0
		}
		if (left.MetadataID != nil) != (right.MetadataID != nil) {
			return left.MetadataID != nil
		}
		if (left.JellyfinSeriesID != "") != (right.JellyfinSeriesID != "") {
			return left.JellyfinSeriesID != ""
		}
		if len(left.Episodes) != len(right.Episodes) {
			return len(left.Episodes) > len(right.Episodes)
		}
		return left.ID < right.ID
	})
}

//nolint:gocyclo // explicit field-by-field preservation is intentional and auditable.
func mergeLocalAnimeIdentityRowsWithReport(tx *gorm.DB, survivor, duplicate *model.LocalAnime, report *migrationRepairReport) error {
	if survivor == nil || duplicate == nil || survivor.ID == duplicate.ID {
		return nil
	}
	if tx.Migrator().HasTable(&model.LocalEpisode{}) {
		result := tx.Unscoped().Model(&model.LocalEpisode{}).
			Where("local_anime_id = ?", duplicate.ID).
			Update("local_anime_id", survivor.ID)
		if result.Error != nil {
			return fmt.Errorf("reassign episodes %d -> %d: %w", duplicate.ID, survivor.ID, result.Error)
		}
		if report != nil {
			report.RelatedRowsMoved["local_episodes"] += int(result.RowsAffected)
		}
	}
	if tx.Migrator().HasTable(&model.PlaybackHistory{}) {
		result := tx.Unscoped().Model(&model.PlaybackHistory{}).
			Where("local_anime_id = ?", duplicate.ID).
			Update("local_anime_id", survivor.ID)
		if result.Error != nil {
			return fmt.Errorf("reassign playback history %d -> %d: %w", duplicate.ID, survivor.ID, result.Error)
		}
		if report != nil {
			report.RelatedRowsMoved["playback_histories"] += int(result.RowsAffected)
		}
	}
	if tx.Migrator().HasTable(&model.LibraryIssue{}) {
		result := tx.Unscoped().Model(&model.LibraryIssue{}).
			Where("local_anime_id = ?", duplicate.ID).
			Update("local_anime_id", survivor.ID)
		if result.Error != nil {
			return fmt.Errorf("reassign library issues %d -> %d: %w", duplicate.ID, survivor.ID, result.Error)
		}
		if report != nil {
			report.RelatedRowsMoved["library_issues"] += int(result.RowsAffected)
		}
	}

	updates := map[string]any{}
	if survivor.MetadataID == nil && duplicate.MetadataID != nil {
		updates["metadata_id"] = *duplicate.MetadataID
		survivor.MetadataID = duplicate.MetadataID
		if report != nil {
			report.FieldMergeCounts["metadata_id"]++
		}
	}
	if strings.TrimSpace(survivor.Title) == "" {
		updates["title"] = duplicate.Title
		if report != nil {
			report.FieldMergeCounts["title"]++
		}
	}
	if strings.TrimSpace(survivor.Image) == "" {
		updates["image"] = duplicate.Image
		if report != nil {
			report.FieldMergeCounts["image"]++
		}
	}
	if strings.TrimSpace(survivor.JellyfinSeriesID) == "" {
		updates["jellyfin_series_id"] = duplicate.JellyfinSeriesID
		if report != nil {
			report.FieldMergeCounts["jellyfin_series_id"]++
		}
	}
	if strings.TrimSpace(survivor.Summary) == "" {
		updates["summary"] = duplicate.Summary
		if report != nil {
			report.FieldMergeCounts["summary"]++
		}
	}
	if strings.TrimSpace(survivor.AirDate) == "" {
		updates["air_date"] = duplicate.AirDate
		if report != nil {
			report.FieldMergeCounts["air_date"]++
		}
	}
	if survivor.Season == 0 && duplicate.Season != 0 {
		updates["season"] = duplicate.Season
		if report != nil {
			report.FieldMergeCounts["season"]++
		}
	}
	if survivor.FileCount < duplicate.FileCount {
		updates["file_count"] = duplicate.FileCount
		if report != nil {
			report.FieldMergeCounts["file_count"]++
		}
	}
	if survivor.TotalSize < duplicate.TotalSize {
		updates["total_size"] = duplicate.TotalSize
		if report != nil {
			report.FieldMergeCounts["total_size"]++
		}
	}
	if len(updates) > 0 {
		if err := tx.Model(&model.LocalAnime{}).Where("id = ?", survivor.ID).Updates(updates).Error; err != nil {
			return fmt.Errorf("preserve duplicate anime fields %d -> %d: %w", duplicate.ID, survivor.ID, err)
		}
	}
	if err := tx.Unscoped().Delete(&model.LocalAnime{}, duplicate.ID).Error; err != nil {
		return fmt.Errorf("delete duplicate local anime %d: %w", duplicate.ID, err)
	}
	if tx.Migrator().HasTable(&model.LocalEpisode{}) {
		var stats struct {
			FileCount int
			TotalSize int64
		}
		if err := tx.Unscoped().Model(&model.LocalEpisode{}).
			Select("COUNT(*) AS file_count, COALESCE(SUM(file_size), 0) AS total_size").
			Where("local_anime_id = ? AND deleted_at IS NULL", survivor.ID).
			Scan(&stats).Error; err != nil {
			return fmt.Errorf("recount local anime %d: %w", survivor.ID, err)
		}
		if err := tx.Model(&model.LocalAnime{}).Where("id = ?", survivor.ID).
			Updates(map[string]any{"file_count": stats.FileCount, "total_size": stats.TotalSize}).Error; err != nil {
			return fmt.Errorf("save local anime stats %d: %w", survivor.ID, err)
		}
	}
	return nil
}

func migrationPathKey(raw string) string {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, `\`, `/`))
	if raw == "" {
		return ""
	}
	raw = pathpkg.Clean(raw)
	if regexp.MustCompile(`(?i)^[a-z]:/|^//`).MatchString(raw) {
		raw = strings.ToLower(raw)
	}
	return raw
}

func migrationLocalAnimeKey(path, root string) string {
	pathKey := migrationPathKey(path)
	// Without a matching configured library root we cannot distinguish a
	// folder series from a loose file series restored from a partial backup.
	// Leave the key NULL rather than risk collapsing unrelated root records.
	if pathKey == "" || root == "" || pathKey == root {
		return ""
	}
	return "folder:" + pathKey
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
	if err := tx.Exec("DROP INDEX IF EXISTS idx_anime_metadata_bangumi_id").Error; err != nil {
		return err
	}
	return tx.Exec(
		"CREATE UNIQUE INDEX idx_anime_metadata_bangumi_id ON anime_metadata(bangumi_id) WHERE bangumi_id != 0",
	).Error
}

func prepareBangumiDuplicateRepair(tx *gorm.DB, snapshot migrationSnapshotManifest) (*migrationRepairReport, error) {
	if !tx.Migrator().HasTable(&model.AnimeMetadata{}) {
		return nil, nil
	}
	report := migrationRepairReport{
		FormatVersion:    1,
		MigrationID:      migration009ID,
		DatabaseVersion:  CurrentSchemaVersion(tx),
		SnapshotID:       snapshot.ID,
		SnapshotSHA256:   snapshot.DatabaseSHA256,
		StartedAt:        time.Now().UTC(),
		Status:           migrationRunCompleted,
		RelatedRowsMoved: map[string]int{},
		FieldMergeCounts: map[string]int{},
	}
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
		report.Status = migrationRunFailed
		report.Error = err.Error()
		return &report, err
	}
	for _, duplicate := range duplicateIDs {
		var rows []model.AnimeMetadata
		if err := tx.Unscoped().
			Where("bangumi_id = ?", duplicate.BangumiID).
			Order("CASE WHEN deleted_at IS NULL THEN 0 ELSE 1 END").
			Order("id ASC").
			Find(&rows).Error; err != nil {
			report.Status = migrationRunFailed
			report.Error = err.Error()
			return &report, err
		}
		if len(rows) == 0 {
			continue
		}
		report.SurvivorCount++
		report.DuplicateCount += len(rows) - 1
		for i := 1; i < len(rows); i++ {
			survivor := &rows[0]
			duplicateRow := &rows[i]
			report.Mappings = append(report.Mappings, migrationRepairMapping{SourceID: duplicateRow.ID, SurvivorID: survivor.ID})
			report.AffectedRows++
			updates := map[string]any{"bangumi_id": 0}
			report.ClearedFields = appendUniqueString(report.ClearedFields, "bangumi_id")
			if duplicateRow.DataSource == "bangumi" {
				updates["data_source"] = ""
				report.ClearedFields = appendUniqueString(report.ClearedFields, "data_source")
			}
			mergeAnimeMetadataField := func(name, survivorValue, duplicateValue string) {
				if strings.TrimSpace(survivorValue) == "" && strings.TrimSpace(duplicateValue) != "" {
					updates[name] = duplicateValue
					report.FieldMergeCounts[name]++
				}
			}
			mergeAnimeMetadataField("title", survivor.Title, duplicateRow.Title)
			if strings.TrimSpace(survivor.Title) == "" {
				survivor.Title = duplicateRow.Title
			}
			mergeAnimeMetadataField("image", survivor.Image, duplicateRow.Image)
			if strings.TrimSpace(survivor.Image) == "" {
				survivor.Image = duplicateRow.Image
			}
			mergeAnimeMetadataField("summary", survivor.Summary, duplicateRow.Summary)
			if strings.TrimSpace(survivor.Summary) == "" {
				survivor.Summary = duplicateRow.Summary
			}
			mergeAnimeMetadataField("air_date", survivor.AirDate, duplicateRow.AirDate)
			if strings.TrimSpace(survivor.AirDate) == "" {
				survivor.AirDate = duplicateRow.AirDate
			}
			mergeAnimeMetadataField("title_cn", survivor.TitleCN, duplicateRow.TitleCN)
			if strings.TrimSpace(survivor.TitleCN) == "" {
				survivor.TitleCN = duplicateRow.TitleCN
			}
			mergeAnimeMetadataField("title_en", survivor.TitleEN, duplicateRow.TitleEN)
			if strings.TrimSpace(survivor.TitleEN) == "" {
				survivor.TitleEN = duplicateRow.TitleEN
			}
			mergeAnimeMetadataField("title_jp", survivor.TitleJP, duplicateRow.TitleJP)
			if strings.TrimSpace(survivor.TitleJP) == "" {
				survivor.TitleJP = duplicateRow.TitleJP
			}
			mergeAnimeMetadataField("bangumi_title", survivor.BangumiTitle, duplicateRow.BangumiTitle)
			if strings.TrimSpace(survivor.BangumiTitle) == "" {
				survivor.BangumiTitle = duplicateRow.BangumiTitle
			}
			mergeAnimeMetadataField("bangumi_image", survivor.BangumiImage, duplicateRow.BangumiImage)
			if strings.TrimSpace(survivor.BangumiImage) == "" {
				survivor.BangumiImage = duplicateRow.BangumiImage
			}
			mergeAnimeMetadataField("bangumi_summary", survivor.BangumiSummary, duplicateRow.BangumiSummary)
			if strings.TrimSpace(survivor.BangumiSummary) == "" {
				survivor.BangumiSummary = duplicateRow.BangumiSummary
			}
			mergeAnimeMetadataField("field_sources", survivor.FieldSources, duplicateRow.FieldSources)
			if strings.TrimSpace(survivor.FieldSources) == "" {
				survivor.FieldSources = duplicateRow.FieldSources
			}
			if survivor.BangumiRating == 0 && duplicateRow.BangumiRating != 0 {
				updates["bangumi_rating"] = duplicateRow.BangumiRating
				survivor.BangumiRating = duplicateRow.BangumiRating
				report.FieldMergeCounts["bangumi_rating"]++
			}
			if len(survivor.BangumiImageRaw) == 0 && len(duplicateRow.BangumiImageRaw) > 0 {
				updates["bangumi_image_raw"] = duplicateRow.BangumiImageRaw
				survivor.BangumiImageRaw = duplicateRow.BangumiImageRaw
				report.FieldMergeCounts["bangumi_image_raw"]++
			}
			if err := tx.Unscoped().
				Model(&model.AnimeMetadata{}).
				Where("id = ?", rows[i].ID).
				Updates(updates).Error; err != nil {
				report.Status = migrationRunFailed
				report.Error = err.Error()
				return &report, err
			}
		}
	}

	now := time.Now().UTC()
	report.CompletedAt = &now
	return &report, nil
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
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
	case completedResourceState, "renamed":
		return completedResourceState
	case "downloading":
		return "downloading"
	case migrationRunFailed:
		return migrationRunFailed
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
	if err := validateAppliedMigrations(target, rows); err != nil {
		return err
	}
	if err := ensureHistoricalDestructiveMigrationReports(target, rows); err != nil {
		return err
	}

	applied := make(map[string]struct{}, len(migrations))
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
	run := migrationRunManifest{
		FormatVersion: 1,
		RunID:         time.Now().UTC().Format("20060102T150405.000000000Z"),
		Status:        "started",
		StartedAt:     time.Now().UTC(),
	}
	if previous, err := loadMigrationRunManifest(); err == nil && previous.Status == migrationRunFailed {
		run.RetryCount = previous.RetryCount + 1
	}

	var snapshot migrationSnapshotManifest
	if needsMigration {
		if snapshot, err = createMigrationSnapshotManifest(target); err != nil {
			return fmt.Errorf("create migration snapshot: %w", err)
		}
		run.SnapshotID = snapshot.ID
		run.SnapshotSHA256 = snapshot.DatabaseSHA256
	}
	if err := writeMigrationRunManifest(run); err != nil {
		return fmt.Errorf("write migration run manifest: %w", err)
	}

	for sequence, m := range migrations {
		if _, ok := applied[m.ID]; ok {
			continue
		}
		if err := runMigrationRepairPreflight(target, m, snapshot); err != nil {
			run.Status = migrationRunFailed
			run.FailedMigration = m.ID
			run.LastError = err.Error()
			_ = writeMigrationRunManifest(run)
			return fmt.Errorf("preflight migration %s: %w", m.ID, err)
		}

		if err := target.Transaction(func(tx *gorm.DB) error {
			if err := m.Apply(tx); err != nil {
				return err
			}

			return tx.Create(&SchemaMigration{
				ID:          m.ID,
				Sequence:    sequence + 1,
				Description: m.Description,
				Checksum:    m.Fingerprint,
				AppliedAt:   time.Now().UTC(),
			}).Error
		}); err != nil {
			run.Status = migrationRunFailed
			run.FailedMigration = m.ID
			run.LastError = err.Error()
			_ = writeMigrationRunManifest(run)
			return fmt.Errorf("apply migration %s: %w", m.ID, err)
		}
		applied[m.ID] = struct{}{}
		run.FailedMigration = ""
		run.LastError = ""
		if err := writeMigrationRunManifest(run); err != nil {
			return fmt.Errorf("write migration run manifest: %w", err)
		}
	}

	// Keep this invariant check outside the "new migration" branch. It repairs
	// databases from builds that marked 014 as applied while still missing one
	// or more columns, without requiring users to delete migration history.
	if err := ensureAnimeMetadataExtendedFields(target); err != nil {
		run.Status = migrationRunFailed
		run.LastError = err.Error()
		_ = writeMigrationRunManifest(run)
		return err
	}

	run.Status = migrationRunCompleted
	now := time.Now().UTC()
	run.CompletedAt = &now
	_ = writeMigrationRunManifest(run)
	return nil
}

func validateAppliedMigrations(target *gorm.DB, rows []SchemaMigration) error {
	known := make(map[string]migration, len(migrations))
	for _, item := range migrations {
		known[item.ID] = item
	}
	var missingChecksum []SchemaMigration
	for _, row := range rows {
		item, ok := known[row.ID]
		if !ok {
			return fmt.Errorf("database contains unknown or future schema migration %q (sequence %d); restore a compatible snapshot before starting", row.ID, row.Sequence)
		}
		expectedSequence := migrationSequence(row.ID)
		if row.Sequence > len(migrations) || row.Sequence > migrationSequence(migrations[len(migrations)-1].ID) {
			return fmt.Errorf("database schema sequence %d is newer than this application supports", row.Sequence)
		}
		if row.Sequence != 0 && row.Sequence != expectedSequence {
			return fmt.Errorf("migration %s has sequence %d, expected %d", row.ID, row.Sequence, expectedSequence)
		}
		if strings.TrimSpace(row.Description) != "" && row.Description != item.Description {
			return fmt.Errorf("historical migration %s description was rewritten; restore a trusted snapshot", row.ID)
		}
		if strings.TrimSpace(row.Checksum) == "" {
			missingChecksum = append(missingChecksum, row)
			continue
		}
		if !strings.EqualFold(row.Checksum, item.Fingerprint) {
			return fmt.Errorf("historical migration %s checksum mismatch; restore a trusted snapshot", row.ID)
		}
	}
	if len(missingChecksum) == 0 {
		return nil
	}
	if len(missingChecksum) != len(rows) {
		return fmt.Errorf("schema_migrations contains a partial checksum history; explicit baseline is required before startup")
	}
	// A checksum-less history is accepted only once as an explicit baseline
	// operation. Every row must be known; subsequent starts require checksums.
	for _, row := range missingChecksum {
		item := known[row.ID]
		updates := map[string]any{"checksum": item.Fingerprint}
		if strings.TrimSpace(row.Description) == "" {
			updates["description"] = item.Description
		}
		if row.Sequence == 0 {
			updates["sequence"] = migrationSequence(row.ID)
		}
		if err := target.Model(&SchemaMigration{}).Where("id = ?", row.ID).Updates(updates).Error; err != nil {
			return fmt.Errorf("backfill trusted migration checksum for %s: %w", row.ID, err)
		}
	}
	return nil
}

func ensureHistoricalDestructiveMigrationReports(target *gorm.DB, rows []SchemaMigration) error {
	for _, row := range rows {
		if row.ID != migration009ID && row.ID != migration015ID {
			continue
		}
		reportPath := filepath.Join(config.DataPath("updates", "migration-reports", row.ID), "report.json")
		if _, err := os.Stat(reportPath); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return err
		}
		report := migrationRepairReport{
			FormatVersion:   1,
			MigrationID:     row.ID,
			DatabaseVersion: CurrentSchemaVersion(target),
			StartedAt:       row.AppliedAt,
			Status:          "already_executed_irreversible",
			Error:           "migration was executed by an earlier release; original data can only be recovered from its pre-migration snapshot",
		}
		if report.StartedAt.IsZero() {
			report.StartedAt = time.Now().UTC()
		}
		now := time.Now().UTC()
		report.CompletedAt = &now
		if err := writeMigrationRepairReport(report); err != nil {
			return fmt.Errorf("write historical migration report %s: %w", row.ID, err)
		}
	}
	return nil
}

func runMigrationRepairPreflight(target *gorm.DB, item migration, snapshot migrationSnapshotManifest) error {
	var report *migrationRepairReport
	var runErr error
	prepare := func(tx *gorm.DB) error {
		switch item.ID {
		case migration009ID:
			report, runErr = prepareBangumiDuplicateRepair(tx, snapshot)
		case migration015ID:
			report, runErr = prepareLocalAnimeIdentityRepair(tx, snapshot)
		default:
			return nil
		}
		return runErr
	}
	switch item.ID {
	case migration009ID:
		if err := target.Transaction(prepare); err != nil {
			if report != nil {
				_ = writeMigrationRepairReport(*report)
			}
			return err
		}
	case migration015ID:
		if err := target.Transaction(prepare); err != nil {
			if report != nil {
				_ = writeMigrationRepairReport(*report)
			}
			return err
		}
	default:
		return nil
	}
	if report != nil {
		if err := writeMigrationRepairReport(*report); err != nil {
			return err
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
	ID                string    `json:"id"`
	FormatVersion     int       `json:"format_version"`
	AppVersion        string    `json:"app_version"`
	OperationType     string    `json:"operation_type"`
	RollbackSupported bool      `json:"rollback_supported"`
	RollbackScope     string    `json:"rollback_scope"`
	DatabaseFormat    int       `json:"database_format"`
	SchemaFormat      int       `json:"schema_format"`
	CreatedAt         time.Time `json:"created_at"`
	DatabasePath      string    `json:"database_path"`
	DatabaseSHA256    string    `json:"database_sha256"`
	SchemaVersion     string    `json:"schema_version,omitempty"`
}

type migrationRunManifest struct {
	FormatVersion   int        `json:"format_version"`
	RunID           string     `json:"run_id"`
	Status          string     `json:"status"`
	StartedAt       time.Time  `json:"started_at"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
	FailedMigration string     `json:"failed_migration,omitempty"`
	LastError       string     `json:"last_error,omitempty"`
	RetryCount      int        `json:"retry_count"`
	SnapshotID      string     `json:"snapshot_id,omitempty"`
	SnapshotSHA256  string     `json:"snapshot_sha256,omitempty"`
}

type migrationRepairMapping struct {
	SourceID   uint `json:"source_id"`
	SurvivorID uint `json:"survivor_id"`
}

type migrationRepairReport struct {
	FormatVersion    int                      `json:"format_version"`
	MigrationID      string                   `json:"migration_id"`
	DatabaseVersion  string                   `json:"database_version"`
	SnapshotID       string                   `json:"snapshot_id,omitempty"`
	SnapshotSHA256   string                   `json:"snapshot_sha256,omitempty"`
	StartedAt        time.Time                `json:"started_at"`
	CompletedAt      *time.Time               `json:"completed_at,omitempty"`
	Status           string                   `json:"status"`
	AffectedRows     int                      `json:"affected_rows"`
	SurvivorCount    int                      `json:"survivor_count"`
	DuplicateCount   int                      `json:"duplicate_count"`
	RelatedRowsMoved map[string]int           `json:"related_rows_moved,omitempty"`
	FieldMergeCounts map[string]int           `json:"field_merge_counts,omitempty"`
	ClearedFields    []string                 `json:"cleared_fields,omitempty"`
	Mappings         []migrationRepairMapping `json:"mappings,omitempty"`
	Error            string                   `json:"error,omitempty"`
}

func createMigrationSnapshotManifest(target *gorm.DB) (migrationSnapshotManifest, error) {
	if CurrentDBPath == "" || CurrentDBPath == sqliteMemoryPath {
		return migrationSnapshotManifest{}, nil
	}
	if _, err := os.Stat(CurrentDBPath); err != nil {
		if os.IsNotExist(err) {
			return migrationSnapshotManifest{}, nil
		}
		return migrationSnapshotManifest{}, err
	}
	root := config.DataPath("updates", "migration-snapshots")
	if err := os.MkdirAll(root, 0700); err != nil {
		return migrationSnapshotManifest{}, err
	}
	now := time.Now().UTC()
	id := now.Format("20060102T150405.000000000Z")
	dir := filepath.Join(root, id)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return migrationSnapshotManifest{}, err
	}
	databasePath := filepath.Join(dir, "database.db")
	if err := target.Exec("VACUUM INTO ?", databasePath).Error; err != nil {
		_ = os.RemoveAll(dir)
		return migrationSnapshotManifest{}, err
	}
	databaseSHA, err := migrationFileSHA256(databasePath)
	if err != nil {
		_ = os.RemoveAll(dir)
		return migrationSnapshotManifest{}, err
	}
	manifest := migrationSnapshotManifest{
		ID:                id,
		FormatVersion:     1,
		AppVersion:        appversion.AppVersion,
		OperationType:     "migration",
		RollbackSupported: false,
		RollbackScope:     "database_only",
		DatabaseFormat:    DatabaseFormat,
		SchemaFormat:      SchemaFormat,
		CreatedAt:         now,
		DatabasePath:      filepath.Base(databasePath),
		DatabaseSHA256:    databaseSHA,
		SchemaVersion:     CurrentSchemaVersion(target),
	}
	payload, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		_ = os.RemoveAll(dir)
		return migrationSnapshotManifest{}, err
	}
	if err := writeMigrationJSON(filepath.Join(dir, "manifest.json"), payload); err != nil {
		_ = os.RemoveAll(dir)
		return migrationSnapshotManifest{}, err
	}
	return manifest, nil
}

func migrationRunManifestPath() string {
	return config.DataPath("updates", "migration-runs", "current.json")
}

func loadMigrationRunManifest() (migrationRunManifest, error) {
	data, err := os.ReadFile(filepath.Clean(migrationRunManifestPath()))
	if err != nil {
		return migrationRunManifest{}, err
	}
	var manifest migrationRunManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return migrationRunManifest{}, err
	}
	return manifest, nil
}

func writeMigrationRunManifest(manifest migrationRunManifest) error {
	if err := os.MkdirAll(filepath.Dir(migrationRunManifestPath()), 0700); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return writeMigrationJSON(migrationRunManifestPath(), payload)
}

func writeMigrationJSON(path string, payload []byte) error {
	tmp := filepath.Clean(path) + ".part"
	if err := os.WriteFile(tmp, append(payload, '\n'), 0600); err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Clean(path)); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func writeMigrationRepairReport(report migrationRepairReport) error {
	root := config.DataPath("updates", "migration-reports", report.MigrationID)
	if err := os.MkdirAll(root, 0700); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := writeMigrationJSON(filepath.Join(root, "report.json"), payload); err != nil {
		return err
	}
	summary := fmt.Sprintf(
		"migration: %s\nstatus: %s\ndatabase_version: %s\nsnapshot_id: %s\nsnapshot_sha256: %s\naffected_rows: %d\nsurvivor_count: %d\nduplicate_count: %d\n",
		report.MigrationID,
		report.Status,
		report.DatabaseVersion,
		report.SnapshotID,
		report.SnapshotSHA256,
		report.AffectedRows,
		report.SurvivorCount,
		report.DuplicateCount,
	)
	if len(report.FieldMergeCounts) > 0 {
		summary += "field_merges:\n"
		keys := make([]string, 0, len(report.FieldMergeCounts))
		for key := range report.FieldMergeCounts {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			summary += fmt.Sprintf("  %s: %d\n", key, report.FieldMergeCounts[key])
		}
	}
	if len(report.ClearedFields) > 0 {
		summary += "cleared_or_unrecoverable_fields:\n"
		for _, key := range report.ClearedFields {
			summary += fmt.Sprintf("  - %s\n", key)
		}
	}
	if report.Error != "" {
		summary += "error: " + report.Error + "\n"
	}
	return os.WriteFile(filepath.Join(root, "summary.txt"), []byte(summary), 0600)
}

func migrationFileSHA256(path string) (string, error) {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	buf := make([]byte, 128*1024)
	for {
		n, readErr := file.Read(buf)
		if n > 0 {
			_, _ = hash.Write(buf[:n])
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return "", readErr
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
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

	seenFingerprints := make(map[string]string, len(migrations))
	for _, item := range migrations {
		fingerprint := strings.TrimSpace(item.Fingerprint)
		if fingerprint == "" {
			return fmt.Errorf("migration %s is missing an immutable fingerprint", item.ID)
		}
		if len(fingerprint) != sha256.Size*2 {
			return fmt.Errorf("migration %s has an invalid fingerprint", item.ID)
		}
		if previousID, ok := seenFingerprints[strings.ToLower(fingerprint)]; ok {
			return fmt.Errorf("migration %s reuses fingerprint from %s", item.ID, previousID)
		}
		seenFingerprints[strings.ToLower(fingerprint)] = item.ID
	}
	return nil
}
