package service

import (
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/event"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/parser"
	"gorm.io/gorm"
)

type ScannerService struct {
}

func NewScannerService() *ScannerService {
	return &ScannerService{}
}

// ScanResult 代表扫描结果 stats
type ScanResult struct {
	DirectoryID uint
	Added       int
	Updated     int
	Deleted     int
}

// ScanAll scans all configured directories
func (s *ScannerService) ScanAll() error {
	var dirs []model.LocalAnimeDirectory
	if err := db.DB.Find(&dirs).Error; err != nil {
		return err
	}

	for _, d := range dirs {
		s.ScanDirectory(&d)
	}
	return nil
}

// ScanDirectory 扫描指定目录并更新数据库
func (s *ScannerService) ScanDirectory(dir *model.LocalAnimeDirectory) (*ScanResult, error) {
	log.Printf("ScannerService: Starting scan for %s", dir.Path)

	event.GlobalBus.Publish(event.EventScanProgress, map[string]interface{}{
		"type": "start",
		"dir":  dir.Path,
	})

	entries, err := os.ReadDir(dir.Path)
	if err != nil {
		log.Printf("ScannerService: ReadDir failed: %v", err)
		return nil, err
	}

	res := &ScanResult{DirectoryID: dir.ID}
	total := len(entries)

	// 1. Iterate Sub-folders (Anime Series)
	for i, entry := range entries {
		// Publish Progress
		event.GlobalBus.Publish(event.EventScanProgress, map[string]interface{}{
			"type":    "progress",
			"current": i + 1,
			"total":   total,
			"dir":     dir.Path,
		})
		if !entry.IsDir() {
			continue
		}
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		animePath := filepath.Join(dir.Path, entry.Name())
		// Count videos
		fileCount, totalSize := s.countVideos(animePath)

		if fileCount > 0 {
			// Check DB
			var anime model.LocalAnime
			if err := db.DB.Where("path = ?", animePath).First(&anime).Error; err == nil {
				// Update
				if anime.FileCount != fileCount || anime.TotalSize != totalSize {
					anime.FileCount = fileCount
					anime.TotalSize = totalSize
					db.DB.Save(&anime)
					res.Updated++
				}
			} else {
				// Insert
				anime = model.LocalAnime{
					DirectoryID: dir.ID,
					Title:       entry.Name(),
					Path:        animePath,
					FileCount:   fileCount,
					TotalSize:   totalSize,
				}
				db.DB.Create(&anime)
				res.Added++

				// Publish New Anime Event (can trigger Metadata Fetch)
				event.GlobalBus.Publish(event.EventMetadataUpdated, map[string]interface{}{
					"type":  "new_anime",
					"id":    anime.ID,
					"title": anime.Title,
				})
			}
		}
	}

	log.Printf("ScannerService: Scan complete. Added: %d, Updated: %d", res.Added, res.Updated)

	event.GlobalBus.Publish(event.EventScanComplete, res)
	return res, nil
}

// CleanupGarbage 清理数据库中的无效数据
func (s *ScannerService) CleanupGarbage() {
	// 1. 删除没有关联剧集的“垃圾”记录
	if err := db.DB.Unscoped().Where("id NOT IN (?)", db.DB.Model(&model.LocalEpisode{}).Select("DISTINCT local_anime_id")).Delete(&model.LocalAnime{}).Error; err != nil {
		log.Printf("Cleanup: Failed to remove empty anime entries: %v", err)
	}

	// 2. 删除孤儿记录 - 当目录被删但番剧没删掉时
	var dirIDs []uint
	db.DB.Model(&model.LocalAnimeDirectory{}).Pluck("id", &dirIDs)
	if len(dirIDs) > 0 {
		db.DB.Unscoped().Where("directory_id NOT IN ?", dirIDs).Delete(&model.LocalAnime{})
	} else {
		db.DB.Unscoped().Where("1 = 1").Delete(&model.LocalAnime{})
	}
}

// AddDirectory 添加一个新的根目录
func (s *ScannerService) AddDirectory(path string) error {
	log.Printf("Adding directory: %s (Skipping strict existence check for cross-platform support)", path)

	// Check if exists (including soft-deleted)
	var existing model.LocalAnimeDirectory
	if err := db.DB.Unscoped().Where("path = ?", path).First(&existing).Error; err == nil {
		if existing.DeletedAt.Valid {
			log.Printf("Removing stale soft-deleted directory to allow fresh add: %s", path)
			if err := db.DB.Unscoped().Delete(&existing).Error; err != nil {
				return err
			}
		} else {
			return nil
		}
	}

	dir := model.LocalAnimeDirectory{
		Path: path,
	}
	return db.DB.Create(&dir).Error
}

// RemoveDirectory 删除目录
func (s *ScannerService) RemoveDirectory(id uint) error {
	return db.DB.Transaction(func(tx *gorm.DB) error {
		// 删除关联的 Anime (Hard Delete)
		if err := tx.Unscoped().Where("directory_id = ?", id).Delete(&model.LocalAnime{}).Error; err != nil {
			return err
		}
		// 删除目录 (Hard Delete)
		if err := tx.Unscoped().Delete(&model.LocalAnimeDirectory{}, id).Error; err != nil {
			return err
		}
		return nil
	})
}

func (s *ScannerService) countVideos(root string) (int, int64) {
	count := 0
	var size int64
	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // ignore error
		}
		if !d.IsDir() {
			if parser.IsVideoFile(path) {
				count++
				info, _ := d.Info()
				size += info.Size()
			}
		}
		return nil
	})
	return count, size
}
