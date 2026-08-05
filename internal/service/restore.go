package service

import (
	"fmt"
	"log"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/pokerjest/animateAutoTool/internal/authsession"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/safeio"
	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"
)

type RestoreOptions struct {
	Configs       bool
	Metadata      bool
	Subscriptions bool
	Logs          bool
	Local         bool
	Users         bool
	RegenerateNFO bool
}

func (o RestoreOptions) HasRestoreCategory() bool {
	return o.Configs || o.Metadata || o.Subscriptions || o.Logs || o.Local || o.Users
}

type RestoreService struct {
	BatchSize int
}

func NewRestoreService() *RestoreService {
	return &RestoreService{BatchSize: 3000}
}

// PerformRestore executes the high-performance parallel read / batch write restore
func (s *RestoreService) PerformRestore(sourcePath string, options RestoreOptions) (retErr error) {
	start := time.Now()
	sourceLabel := filepath.Base(filepath.Clean(sourcePath))
	optionSummary := restoreOptionsSummary(options)
	log.Printf("RestoreService: restore starting source=%s categories=%s", sourceLabel, optionSummary)
	defer func() {
		if recovered := recover(); recovered != nil {
			retErr = fmt.Errorf("restore panic: %v", recovered)
			log.Printf(
				"ERROR: RestoreService: panic source=%s categories=%s recovery_action=transaction_rollback_and_restore_snapshot panic=%v\n%s",
				sourceLabel,
				optionSummary,
				recovered,
				debug.Stack(),
			)
		}
		if retErr != nil {
			log.Printf(
				"ERROR: RestoreService: restore failed source=%s categories=%s duration=%s recovery_action=inspect_pre_restore_snapshot error=%v",
				sourceLabel,
				optionSummary,
				time.Since(start).Round(time.Millisecond),
				retErr,
			)
			return
		}
		log.Printf(
			"RestoreService: restore completed source=%s categories=%s duration=%s schema=%s",
			sourceLabel,
			optionSummary,
			time.Since(start).Round(time.Millisecond),
			db.CurrentSchemaVersion(db.DB),
		)
	}()

	descriptor, err := InspectBackup(sourcePath)
	if err != nil {
		return fmt.Errorf("failed to inspect backup file: %v", err)
	}
	log.Printf(
		"RestoreService: backup inspected source=%s mode=%s app_version=%s schema=%s database_format=%d schema_format=%d contains_secrets=%t",
		sourceLabel,
		descriptor.Mode,
		descriptor.AppVersion,
		descriptor.SchemaVersion,
		descriptor.DatabaseFormat,
		descriptor.SchemaFormat,
		descriptor.ContainsSecrets,
	)
	if err := validateRestoreOptions(descriptor, options); err != nil {
		return err
	}
	if backupSchema := backupSchemaNumber(descriptor.SchemaVersion); backupSchema >= 0 {
		currentSchema := backupSchemaNumber(db.CurrentSchemaVersion(db.DB))
		if currentSchema >= 0 && backupSchema > currentSchema {
			return fmt.Errorf(
				"backup schema %s is newer than the current readable schema %s",
				descriptor.SchemaVersion,
				db.CurrentSchemaVersion(db.DB),
			)
		}
	}
	if descriptor.DatabaseFormat > 0 && descriptor.DatabaseFormat != db.DatabaseFormat {
		return fmt.Errorf("backup database format %d is not supported by the current format %d", descriptor.DatabaseFormat, db.DatabaseFormat)
	}
	if descriptor.SchemaFormat > 0 && descriptor.SchemaFormat != db.SchemaFormat {
		return fmt.Errorf("backup schema format %d is not supported by the current format %d", descriptor.SchemaFormat, db.SchemaFormat)
	}
	snapshot, err := CreateSafetySnapshot("backup-restore")
	if err != nil {
		return fmt.Errorf("failed to create pre-restore snapshot: %w", err)
	}
	if snapshot.ID == "" {
		log.Printf("RestoreService: pre-restore snapshot skipped source=%s reason=non_file_database", sourceLabel)
	} else {
		log.Printf(
			"RestoreService: pre-restore snapshot completed source=%s snapshot_id=%s snapshot_sha256=%s",
			sourceLabel,
			snapshot.ID,
			snapshot.DatabaseSHA256,
		)
	}

	// 1. Open Source DB (ReadOnly)
	srcDB, err := gorm.Open(sqlite.Open(sourcePath), &gorm.Config{
		Logger: nil, // Silence logger for performance
	})
	if err != nil {
		return fmt.Errorf("failed to open backup file: %v", err)
	}
	if sqlDB, err := srcDB.DB(); err == nil {
		defer safeio.Close(sqlDB)
	}

	// 2. Read Data
	data, err := s.readBackupData(srcDB, options)
	if err != nil {
		log.Printf("ERROR: RestoreService: read phase failed source=%s error=%v", sourceLabel, err)
		return err
	}

	log.Printf(
		"RestoreService: read phase completed source=%s configs=%d metadata=%d subscriptions=%d logs=%d resources=%d run_logs=%d directories=%d animes=%d episodes=%d playback=%d users=%d",
		sourceLabel,
		len(data.configs),
		len(data.metas),
		len(data.subs),
		len(data.logs),
		len(data.resources),
		len(data.runLogs),
		len(data.dirs),
		len(data.animes),
		len(data.episodes),
		len(data.playback),
		len(data.users),
	)

	// 3. Transaction Write Phase
	log.Printf("RestoreService: transaction starting source=%s categories=%s", sourceLabel, optionSummary)
	if err := db.DB.Transaction(func(tx *gorm.DB) error {
		log.Printf("RestoreService: dependency validation starting source=%s", sourceLabel)
		if err := validateRestoreDependencies(tx, data, options); err != nil {
			log.Printf("ERROR: RestoreService: dependency validation failed source=%s error=%v", sourceLabel, err)
			return err
		}
		log.Printf("RestoreService: dependency validation completed source=%s", sourceLabel)
		log.Printf("RestoreService: write phase starting source=%s", sourceLabel)
		if err := s.writeRestoreData(tx, data, options, descriptor); err != nil {
			log.Printf("ERROR: RestoreService: write phase failed source=%s error=%v", sourceLabel, err)
			return err
		}
		log.Printf("RestoreService: write phase completed source=%s", sourceLabel)
		log.Printf("RestoreService: post-restore validation starting source=%s", sourceLabel)
		if err := validateRestoredDatabase(tx, data, options); err != nil {
			log.Printf("ERROR: RestoreService: post-restore validation failed source=%s error=%v", sourceLabel, err)
			return err
		}
		log.Printf("RestoreService: post-restore validation completed source=%s", sourceLabel)
		return nil
	}); err != nil {
		return err
	}
	log.Printf("RestoreService: transaction committed source=%s duration=%s", sourceLabel, time.Since(start).Round(time.Millisecond))
	if currentSchema := db.CurrentSchemaVersion(db.DB); currentSchema != db.LatestSchemaVersion() {
		return fmt.Errorf("restore completed but database schema version is %s, expected %s", currentSchema, db.LatestSchemaVersion())
	}
	if options.Configs {
		log.Printf("RestoreService: config mirror export starting source=%s", sourceLabel)
		if err := db.ExportGlobalConfigsToConfigFile(); err != nil {
			if snapshot.ID != "" {
				if restoreErr := RestoreSafetySnapshot(snapshot.ID); restoreErr != nil {
					log.Printf(
						"ERROR: RestoreService: automatic snapshot rollback failed snapshot_id=%s error=%v",
						snapshot.ID,
						restoreErr,
					)
				} else {
					log.Printf("WARN: RestoreService: restored pre-restore snapshot after config export failure snapshot_id=%s", snapshot.ID)
				}
			}
			return fmt.Errorf("sync restored settings to config.yaml: %w", err)
		}
		log.Printf("RestoreService: config mirror export completed source=%s", sourceLabel)
	}
	if options.Users {
		previousGeneration := authsession.Current()
		authsession.InvalidateAll()
		if err := db.SaveGlobalConfig(db.AuthSessionGenerationConfigKey, strconv.FormatUint(authsession.Current(), 10)); err != nil {
			return fmt.Errorf("persist restored-session invalidation generation: %w", err)
		}
		log.Printf(
			"RestoreService: auth sessions invalidated source=%s previous_generation=%d current_generation=%d",
			sourceLabel,
			previousGeneration,
			authsession.Current(),
		)
	}
	return nil
}

func restoreOptionsSummary(options RestoreOptions) string {
	categories := make([]string, 0, 6)
	if options.Configs {
		categories = append(categories, "configs")
	}
	if options.Metadata {
		categories = append(categories, "metadata")
	}
	if options.Subscriptions {
		categories = append(categories, "subscriptions")
	}
	if options.Logs {
		categories = append(categories, "logs")
	}
	if options.Local {
		categories = append(categories, "local")
	}
	if options.Users {
		categories = append(categories, "users")
	}
	if len(categories) == 0 {
		return "none"
	}
	return strings.Join(categories, ",")
}

type restoreData struct {
	configs          []model.GlobalConfig
	metas            []model.AnimeMetadata
	subs             []model.Subscription
	logs             []model.DownloadLog
	resources        []model.SubscriptionResource
	runLogs          []model.SubscriptionRunLog
	dirs             []model.LocalAnimeDirectory
	animes           []model.LocalAnime
	episodes         []model.LocalEpisode
	playback         []model.PlaybackHistory
	users            []model.User
	hasConfigs       bool
	hasMetadata      bool
	hasSubscriptions bool
	hasDownloadLogs  bool
	hasResources     bool
	hasRunLogs       bool
	hasDirs          bool
	hasAnimes        bool
	hasEpisodes      bool
	hasPlayback      bool
	hasUsers         bool
}

func (s *RestoreService) readBackupData(srcDB *gorm.DB, options RestoreOptions) (*restoreData, error) {
	d := &restoreData{}
	var eg errgroup.Group

	if options.Configs {
		if srcDB.Migrator().HasTable(&model.GlobalConfig{}) {
			d.hasConfigs = true
			eg.Go(func() error { return srcDB.Find(&d.configs).Error })
		}
	}
	if options.Metadata {
		if srcDB.Migrator().HasTable(&model.AnimeMetadata{}) {
			d.hasMetadata = true
			eg.Go(func() error { return srcDB.Find(&d.metas).Error })
		}
	}
	if options.Subscriptions {
		if srcDB.Migrator().HasTable(&model.Subscription{}) {
			d.hasSubscriptions = true
			eg.Go(func() error { return srcDB.Find(&d.subs).Error })
		}
	}
	if options.Logs {
		if srcDB.Migrator().HasTable(&model.DownloadLog{}) {
			d.hasDownloadLogs = true
			eg.Go(func() error { return srcDB.Find(&d.logs).Error })
		}
		if srcDB.Migrator().HasTable(&model.SubscriptionResource{}) {
			d.hasResources = true
			eg.Go(func() error { return srcDB.Find(&d.resources).Error })
		}
		eg.Go(func() error {
			if !srcDB.Migrator().HasTable(&model.SubscriptionRunLog{}) {
				return nil
			}
			d.hasRunLogs = true
			return srcDB.Find(&d.runLogs).Error
		})
	}
	if options.Local {
		if srcDB.Migrator().HasTable(&model.LocalAnimeDirectory{}) {
			d.hasDirs = true
			eg.Go(func() error { return srcDB.Find(&d.dirs).Error })
		}
		if srcDB.Migrator().HasTable(&model.LocalAnime{}) {
			d.hasAnimes = true
			// Select only source columns so backups created before scan_key was
			// introduced remain readable by the current model.
			eg.Go(func() error {
				return srcDB.Table("local_animes").
					Where("deleted_at IS NULL").
					Select("*").
					Scan(&d.animes).Error
			})
		}
		if srcDB.Migrator().HasTable(&model.LocalEpisode{}) {
			d.hasEpisodes = true
			eg.Go(func() error { return srcDB.Find(&d.episodes).Error })
		}
		eg.Go(func() error {
			if !srcDB.Migrator().HasTable(&model.PlaybackHistory{}) {
				return nil
			}
			d.hasPlayback = true
			return srcDB.Find(&d.playback).Error
		})
	}
	if options.Users {
		if srcDB.Migrator().HasTable(&model.User{}) {
			d.hasUsers = true
			eg.Go(func() error { return srcDB.Find(&d.users).Error })
		}
	}

	if err := eg.Wait(); err != nil {
		return nil, err
	}
	return d, nil
}

func validateRestoreOptions(desc BackupDescriptor, options RestoreOptions) error {
	switch {
	case !options.HasRestoreCategory():
		return fmt.Errorf("at least one restore category must be selected")
	case options.Configs && !desc.HasConfigs:
		return fmt.Errorf("backup does not contain global configs")
	case options.Metadata && !desc.HasMetadata:
		return fmt.Errorf("backup does not contain metadata")
	case options.Subscriptions && !desc.HasSubscriptions:
		return fmt.Errorf("backup does not contain subscriptions")
	case options.Logs && !desc.HasLogs:
		return fmt.Errorf("backup does not contain download logs")
	case options.Local && !desc.HasLocal:
		return fmt.Errorf("backup does not contain local library data")
	case options.Users && !desc.HasUsers:
		return fmt.Errorf("backup does not contain users")
	default:
		return nil
	}
}

//nolint:gocyclo // selected restore categories require explicit dependency checks.
func validateRestoreDependencies(tx *gorm.DB, d *restoreData, options RestoreOptions) error {
	if tx == nil || d == nil {
		return fmt.Errorf("restore data is unavailable")
	}
	userIDs := make(map[uint]struct{}, len(d.users))
	for _, user := range d.users {
		userIDs[user.ID] = struct{}{}
	}
	animeIDs := make(map[uint]struct{}, len(d.animes))
	for _, anime := range d.animes {
		animeIDs[anime.ID] = struct{}{}
	}
	episodeIDs := make(map[uint]struct{}, len(d.episodes))
	for _, episode := range d.episodes {
		episodeIDs[episode.ID] = struct{}{}
	}
	metadataIDs := make(map[uint]struct{}, len(d.metas))
	for _, metadata := range d.metas {
		metadataIDs[metadata.ID] = struct{}{}
	}
	directoryIDs := make(map[uint]struct{}, len(d.dirs))
	for _, directory := range d.dirs {
		directoryIDs[directory.ID] = struct{}{}
	}
	subscriptionIDs := make(map[uint]struct{}, len(d.subs))
	for _, subscription := range d.subs {
		subscriptionIDs[subscription.ID] = struct{}{}
	}

	exists := func(table any, id uint) (bool, error) {
		if id == 0 {
			return false, nil
		}
		var count int64
		if err := tx.Model(table).Where("id = ?", id).Count(&count).Error; err != nil {
			return false, err
		}
		return count > 0, nil
	}
	hasUser := func(id uint) (bool, error) {
		if options.Users && d.hasUsers {
			_, ok := userIDs[id]
			return ok, nil
		}
		return exists(&model.User{}, id)
	}
	hasAnime := func(id uint) (bool, error) {
		if options.Local && d.hasAnimes {
			_, ok := animeIDs[id]
			return ok, nil
		}
		return exists(&model.LocalAnime{}, id)
	}
	hasEpisode := func(id uint) (bool, error) {
		if options.Local && d.hasEpisodes {
			_, ok := episodeIDs[id]
			return ok, nil
		}
		return exists(&model.LocalEpisode{}, id)
	}
	hasMetadata := func(id uint) (bool, error) {
		if options.Metadata && d.hasMetadata {
			_, ok := metadataIDs[id]
			return ok, nil
		}
		return exists(&model.AnimeMetadata{}, id)
	}
	hasDirectory := func(id uint) (bool, error) {
		if options.Local && d.hasDirs {
			_, ok := directoryIDs[id]
			return ok, nil
		}
		return exists(&model.LocalAnimeDirectory{}, id)
	}
	hasSubscription := func(id uint) (bool, error) {
		if options.Subscriptions && d.hasSubscriptions {
			_, ok := subscriptionIDs[id]
			return ok, nil
		}
		return exists(&model.Subscription{}, id)
	}

	if options.Logs {
		validateSubscriptionID := func(table string, rowID, subscriptionID uint) error {
			if subscriptionID == 0 {
				return nil
			}
			ok, err := hasSubscription(subscriptionID)
			if err != nil {
				return fmt.Errorf("validate %s %d subscription: %w", table, rowID, err)
			}
			if !ok {
				return fmt.Errorf("restore would create orphan %s %d -> subscription %d", table, rowID, subscriptionID)
			}
			return nil
		}
		if d.hasDownloadLogs {
			for _, entry := range d.logs {
				if err := validateSubscriptionID("download_log", entry.ID, entry.SubscriptionID); err != nil {
					return err
				}
			}
		}
		if d.hasResources {
			for _, entry := range d.resources {
				if err := validateSubscriptionID("subscription_resource", entry.ID, entry.SubscriptionID); err != nil {
					return err
				}
			}
		}
		if d.hasRunLogs {
			for _, entry := range d.runLogs {
				if err := validateSubscriptionID("subscription_run_log", entry.ID, entry.SubscriptionID); err != nil {
					return err
				}
			}
		}
	}

	if options.Local && d.hasEpisodes {
		for _, episode := range d.episodes {
			ok, err := hasAnime(episode.LocalAnimeID)
			if err != nil {
				return fmt.Errorf("validate local episode %d parent: %w", episode.ID, err)
			}
			if !ok {
				return fmt.Errorf("restore would create orphan local_episode %d -> local_anime %d", episode.ID, episode.LocalAnimeID)
			}
		}
	}
	if options.Local && d.hasPlayback {
		for _, history := range d.playback {
			ok, err := hasUser(history.UserID)
			if err != nil {
				return fmt.Errorf("validate playback history %d user: %w", history.ID, err)
			}
			if !ok {
				return fmt.Errorf("restore would create orphan playback_history %d -> user %d", history.ID, history.UserID)
			}
			ok, err = hasEpisode(history.LocalEpisodeID)
			if err != nil {
				return fmt.Errorf("validate playback history %d episode: %w", history.ID, err)
			}
			if !ok {
				return fmt.Errorf("restore would create orphan playback_history %d -> local_episode %d", history.ID, history.LocalEpisodeID)
			}
			ok, err = hasAnime(history.LocalAnimeID)
			if err != nil {
				return fmt.Errorf("validate playback history %d anime: %w", history.ID, err)
			}
			if !ok {
				return fmt.Errorf("restore would create orphan playback_history %d -> local_anime %d", history.ID, history.LocalAnimeID)
			}
		}
	}
	if options.Local && d.hasAnimes {
		for _, anime := range d.animes {
			if anime.DirectoryID != 0 {
				ok, err := hasDirectory(anime.DirectoryID)
				if err != nil {
					return fmt.Errorf("validate local anime %d directory: %w", anime.ID, err)
				}
				if !ok {
					return fmt.Errorf("restore would create orphan local_anime %d -> directory %d", anime.ID, anime.DirectoryID)
				}
			}
			if anime.MetadataID == nil || *anime.MetadataID == 0 {
				continue
			}
			ok, err := hasMetadata(*anime.MetadataID)
			if err != nil {
				return fmt.Errorf("validate local anime %d metadata: %w", anime.ID, err)
			}
			if !ok {
				return fmt.Errorf("restore would create orphan local_anime %d -> metadata %d", anime.ID, *anime.MetadataID)
			}
		}
	}
	if options.Subscriptions && d.hasSubscriptions {
		for _, subscription := range d.subs {
			if subscription.MetadataID == nil || *subscription.MetadataID == 0 {
				continue
			}
			ok, err := hasMetadata(*subscription.MetadataID)
			if err != nil {
				return fmt.Errorf("validate subscription %d metadata: %w", subscription.ID, err)
			}
			if !ok {
				return fmt.Errorf("restore would create orphan subscription %d -> metadata %d", subscription.ID, *subscription.MetadataID)
			}
		}
	}
	return nil
}

func validateRestoredDatabase(tx *gorm.DB, d *restoreData, options RestoreOptions) error {
	var quick string
	if err := tx.Raw("PRAGMA quick_check").Scan(&quick).Error; err != nil {
		return fmt.Errorf("sqlite quick_check: %w", err)
	}
	if quick != "" && quick != "ok" {
		return fmt.Errorf("sqlite quick_check failed: %s", quick)
	}
	var foreignKeys []struct {
		Table  string
		RowID  int
		Parent string
		FKID   int
	}
	if err := tx.Raw("PRAGMA foreign_key_check").Scan(&foreignKeys).Error; err != nil {
		return fmt.Errorf("sqlite foreign_key_check: %w", err)
	}
	if len(foreignKeys) > 0 {
		return fmt.Errorf("restore left %d foreign-key violations", len(foreignKeys))
	}
	checkCount := func(table string, expected int, enabled bool) error {
		if !enabled {
			return nil
		}
		var count int64
		if err := tx.Table(table).Count(&count).Error; err != nil {
			return err
		}
		if count != int64(expected) {
			return fmt.Errorf("restored %s count=%d, expected=%d", table, count, expected)
		}
		return nil
	}
	if err := checkCount("anime_metadata", len(d.metas), options.Metadata && d.hasMetadata); err != nil {
		return err
	}
	if err := checkCount("subscriptions", len(d.subs), options.Subscriptions && d.hasSubscriptions); err != nil {
		return err
	}
	if err := checkCount("users", len(d.users), options.Users && d.hasUsers); err != nil {
		return err
	}
	if err := checkCount("local_animes", len(d.animes), options.Local && d.hasAnimes); err != nil {
		return err
	}
	if err := checkCount("local_episodes", len(d.episodes), options.Local && d.hasEpisodes); err != nil {
		return err
	}
	if err := checkCount("playback_histories", len(d.playback), options.Local && d.hasPlayback); err != nil {
		return err
	}
	return nil
}

func (s *RestoreService) writeRestoreData(tx *gorm.DB, d *restoreData, options RestoreOptions, desc BackupDescriptor) error {
	createBatch := func(data any) error {
		return tx.CreateInBatches(data, s.BatchSize).Error
	}

	if options.Configs {
		if err := writeRestoreConfigs(tx, d, desc, createBatch); err != nil {
			return err
		}
	}

	if options.Metadata {
		if err := replaceRestoreRows(
			d.hasMetadata,
			len(d.metas),
			func() error { return tx.Exec("DELETE FROM anime_metadata").Error },
			&d.metas,
			createBatch,
		); err != nil {
			return err
		}
	}

	if options.Subscriptions {
		if err := replaceRestoreRows(
			d.hasSubscriptions,
			len(d.subs),
			func() error { return tx.Exec("DELETE FROM subscriptions").Error },
			&d.subs,
			createBatch,
		); err != nil {
			return err
		}
	}

	if options.Logs {
		if err := writeRestoreLogs(tx, d, createBatch); err != nil {
			return err
		}
	}

	if options.Local {
		if err := writeRestoreLocalLibrary(tx, d, createBatch); err != nil {
			return err
		}
	}

	if options.Users {
		if err := replaceRestoreRows(
			d.hasUsers,
			len(d.users),
			func() error { return tx.Exec("DELETE FROM users").Error },
			&d.users,
			createBatch,
		); err != nil {
			return err
		}
	}

	return nil
}

type restoreBatchWriter func(data any) error

func writeRestoreConfigs(tx *gorm.DB, d *restoreData, desc BackupDescriptor, createBatch restoreBatchWriter) error {
	if BackupConfigMerges(desc.Mode) {
		for _, cfg := range d.configs {
			if err := tx.Where(model.GlobalConfig{Key: cfg.Key}).
				Assign(model.GlobalConfig{Value: cfg.Value}).
				FirstOrCreate(&model.GlobalConfig{}).Error; err != nil {
				return err
			}
		}
		return nil
	}
	if !d.hasConfigs {
		return nil
	}

	var preserved []model.GlobalConfig
	if !desc.ContainsSecrets {
		if err := tx.Where("key LIKE ? OR key LIKE ? OR key LIKE ? OR key LIKE ? OR key LIKE ?",
			"%password%", "%secret%", "%token%", "%key%", "%credential%").
			Find(&preserved).Error; err != nil {
			return err
		}
	}
	if err := tx.Exec("DELETE FROM global_configs").Error; err != nil {
		return err
	}
	if len(d.configs) > 0 {
		if err := createBatch(&d.configs); err != nil {
			return err
		}
	}
	if len(preserved) > 0 {
		return createBatch(&preserved)
	}
	return nil
}

func replaceRestoreRows(hasTable bool, rowCount int, deleteRows func() error, data any, createBatch restoreBatchWriter) error {
	if !hasTable {
		return nil
	}
	if err := deleteRows(); err != nil {
		return err
	}
	if rowCount == 0 {
		return nil
	}
	return createBatch(data)
}

func writeRestoreLogs(tx *gorm.DB, d *restoreData, createBatch restoreBatchWriter) error {
	if d.hasDownloadLogs {
		if err := tx.Exec("DELETE FROM download_logs").Error; err != nil {
			return err
		}
	}
	if err := replaceRestoreRows(
		d.hasResources,
		len(d.resources),
		func() error { return tx.Exec("DELETE FROM subscription_resources").Error },
		&d.resources,
		createBatch,
	); err != nil {
		return err
	}
	if err := replaceRestoreRows(
		d.hasDownloadLogs,
		len(d.logs),
		func() error { return nil },
		&d.logs,
		createBatch,
	); err != nil {
		return err
	}
	return replaceRestoreRows(
		d.hasRunLogs,
		len(d.runLogs),
		func() error { return tx.Exec("DELETE FROM subscription_run_logs").Error },
		&d.runLogs,
		createBatch,
	)
}

func writeRestoreLocalLibrary(tx *gorm.DB, d *restoreData, createBatch restoreBatchWriter) error {
	if d.hasAnimes {
		if err := db.DropLocalAnimeIdentityIndex(tx); err != nil {
			return err
		}
	}
	if d.hasPlayback {
		if err := tx.Exec("DELETE FROM playback_histories").Error; err != nil {
			return err
		}
	}
	if d.hasEpisodes {
		if err := tx.Exec("DELETE FROM local_episodes").Error; err != nil {
			return err
		}
	}
	if d.hasAnimes {
		if err := tx.Exec("DELETE FROM local_animes").Error; err != nil {
			return err
		}
	}
	if d.hasDirs {
		if err := tx.Exec("DELETE FROM local_anime_directories").Error; err != nil {
			return err
		}
	}

	if d.hasDirs && len(d.dirs) > 0 {
		if err := createBatch(&d.dirs); err != nil {
			return err
		}
	}
	if d.hasAnimes && len(d.animes) > 0 {
		if err := createBatch(&d.animes); err != nil {
			return err
		}
	}
	if d.hasEpisodes && len(d.episodes) > 0 {
		if err := createBatch(&d.episodes); err != nil {
			return err
		}
	}
	if d.hasPlayback && len(d.playback) > 0 {
		if err := createBatch(&d.playback); err != nil {
			return err
		}
	}
	if d.hasAnimes {
		return db.RepairLocalAnimeIdentity(tx)
	}
	return nil
}
