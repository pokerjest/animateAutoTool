package service

import (
	"fmt"
	"log"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
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

	// 1. Open Source DB (ReadOnly)
	srcDB, err := gorm.Open(sqlite.Open(sourcePath), &gorm.Config{
		Logger: nil, // Silence logger for performance
	})
	if err != nil {
		return fmt.Errorf("failed to open backup file: %v", err)
	}

	// 2. Read Data
	data, err := s.readBackupData(srcDB, options)
	if err != nil {
		log.Printf("RestoreService: Read error (potentially partial backup): %v", err)
		return err
	}

	log.Printf("RestoreService: Read phase complete.")

	// 3. Transaction Write Phase
	return db.DB.Transaction(func(tx *gorm.DB) error {
		if err := s.writeRestoreData(tx, data, options); err != nil {
			return err
		}
		log.Printf("RestoreService: Transaction committed successfully in %v", time.Since(start))
		return nil
	})
}

type restoreData struct {
	configs  []model.GlobalConfig
	metas    []model.AnimeMetadata
	subs     []model.Subscription
	logs     []model.DownloadLog
	dirs     []model.LocalAnimeDirectory
	animes   []model.LocalAnime
	episodes []model.LocalEpisode
	users    []model.User
}

func (s *RestoreService) readBackupData(srcDB *gorm.DB, options RestoreOptions) (*restoreData, error) {
	d := &restoreData{}
	var eg errgroup.Group

	if options.Configs {
		eg.Go(func() error { return srcDB.Find(&d.configs).Error })
	}
	if options.Metadata {
		eg.Go(func() error { return srcDB.Find(&d.metas).Error })
	}
	if options.Subscriptions {
		eg.Go(func() error { return srcDB.Find(&d.subs).Error })
	}
	if options.Logs {
		eg.Go(func() error { return srcDB.Find(&d.logs).Error })
	}
	if options.Local {
		eg.Go(func() error { return srcDB.Find(&d.dirs).Error })
		eg.Go(func() error { return srcDB.Find(&d.animes).Error })
		eg.Go(func() error { return srcDB.Find(&d.episodes).Error })
	}
	if options.Users {
		eg.Go(func() error { return srcDB.Find(&d.users).Error })
	}

	if err := eg.Wait(); err != nil {
		return nil, err
	}
	return d, nil
}

func (s *RestoreService) writeRestoreData(tx *gorm.DB, d *restoreData, options RestoreOptions) error {
	createBatch := func(data interface{}) error {
		return tx.CreateInBatches(data, s.BatchSize).Error
	}

	if options.Configs {
		tx.Exec("DELETE FROM global_configs")
		if len(d.configs) > 0 {
			if err := createBatch(&d.configs); err != nil {
				return err
			}
		}
	}

	if options.Metadata {
		tx.Exec("DELETE FROM anime_metadata")
		if len(d.metas) > 0 {
			if err := createBatch(&d.metas); err != nil {
				return err
			}
		}
	}

	if options.Subscriptions {
		tx.Exec("DELETE FROM subscriptions")
		if len(d.subs) > 0 {
			if err := createBatch(&d.subs); err != nil {
				return err
			}
		}
	}

	if options.Logs {
		tx.Exec("DELETE FROM download_logs")
		if len(d.logs) > 0 {
			if err := createBatch(&d.logs); err != nil {
				return err
			}
		}
	}

	if options.Local {
		tx.Exec("DELETE FROM local_episodes")
		tx.Exec("DELETE FROM local_animes")
		tx.Exec("DELETE FROM local_anime_directories")

		if len(d.dirs) > 0 {
			if err := createBatch(&d.dirs); err != nil {
				return err
			}
		}
		if len(d.animes) > 0 {
			if err := createBatch(&d.animes); err != nil {
				return err
			}
		}
		if len(d.episodes) > 0 {
			if err := createBatch(&d.episodes); err != nil {
				return err
			}
		}
	}

	if options.Users {
		tx.Exec("DELETE FROM users")
		if len(d.users) > 0 {
			if err := createBatch(&d.users); err != nil {
				return err
			}
		}
	}

	return nil
}
