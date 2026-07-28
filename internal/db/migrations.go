package db

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/parser"
	"gorm.io/gorm"
)

// SchemaMigration records each explicit schema/data migration that has been
// applied to a database. We keep this separate from app config so future
// releases can safely evolve table layouts and data fixes over time.
type SchemaMigration struct {
	ID          string    `gorm:"primaryKey;size:64"`
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

	for _, m := range migrations {
		if _, ok := applied[m.ID]; ok {
			continue
		}

		if err := target.Transaction(func(tx *gorm.DB) error {
			if err := m.Apply(tx); err != nil {
				return err
			}

			return tx.Create(&SchemaMigration{
				ID:          m.ID,
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
	if err := target.Order("id desc").First(&row).Error; err != nil {
		return ""
	}
	return row.ID
}
