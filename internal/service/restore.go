package service

import (
	"fmt"
	"log"
	"time"

	"github.com/glebarez/sqlite"
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
func (s *RestoreService) PerformRestore(sourcePath string, options RestoreOptions) error {
	log.Printf("RestoreService: Starting restore from %s", sourcePath)
	start := time.Now()

	descriptor, err := InspectBackup(sourcePath)
	if err != nil {
		return fmt.Errorf("failed to inspect backup file: %v", err)
	}
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
	snapshot, err := CreateSafetySnapshot("backup-restore")
	if err != nil {
		return fmt.Errorf("failed to create pre-restore snapshot: %w", err)
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
		log.Printf("RestoreService: Read error (potentially partial backup): %v", err)
		return err
	}

	log.Printf("RestoreService: Read phase complete.")

	// 3. Transaction Write Phase
	if err := db.DB.Transaction(func(tx *gorm.DB) error {
		if err := s.writeRestoreData(tx, data, options, descriptor); err != nil {
			return err
		}
		log.Printf("RestoreService: Transaction committed successfully in %v", time.Since(start))
		return nil
	}); err != nil {
		return err
	}
	if options.Configs {
		if err := db.ExportGlobalConfigsToConfigFile(); err != nil {
			if snapshot.ID != "" {
				_ = RestoreSafetySnapshot(snapshot.ID)
			}
			return fmt.Errorf("sync restored settings to config.yaml: %w", err)
		}
	}
	return nil
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
