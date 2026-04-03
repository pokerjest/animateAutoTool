package service

import (
	"fmt"
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

	if len(dirs) == 0 {
		event.GlobalBus.Publish(event.EventScanRun, GlobalScanStatus.Skip("当前没有已配置目录可扫描"))
		return nil
	}

	event.GlobalBus.Publish(event.EventScanRun, GlobalScanStatus.Begin(len(dirs)))

	for _, d := range dirs {
		res, err := s.ScanDirectory(&d)
		added := 0
		updated := 0
		if res != nil {
			added = res.Added
			updated = res.Updated
		}
		event.GlobalBus.Publish(event.EventScanRun, GlobalScanStatus.Advance(d.Path, added, updated, err))
		if err != nil {
			log.Printf("ScannerService: Failed to scan directory %s: %v", d.Path, err)
		}
	}
	event.GlobalBus.Publish(event.EventScanRun, GlobalScanStatus.Finish())
	event.GlobalBus.Publish(event.EventScanComplete, map[string]interface{}{
		"scope":   "run",
		"summary": GlobalScanStatus.Snapshot().LastSummary,
	})
	return nil
}

// ScanDirectory 扫描指定目录并更新数据库
func (s *ScannerService) ScanDirectory(dir *model.LocalAnimeDirectory) (*ScanResult, error) {
	log.Printf("ScannerService: Starting scan for %s", dir.Path)
	issueKey := "scan:" + filepath.Clean(dir.Path)

	event.GlobalBus.Publish(event.EventScanProgress, map[string]interface{}{
		"type": "start",
		"dir":  dir.Path,
	})

	entries, err := os.ReadDir(dir.Path)
	if err != nil {
		log.Printf("ScannerService: ReadDir failed: %v", err)
		_ = ReportLibraryIssue(LibraryIssueInput{
			IssueKey:      issueKey,
			IssueType:     LibraryIssueTypeScan,
			Title:         filepath.Base(dir.Path),
			DirectoryPath: dir.Path,
			Message:       err.Error(),
			Hint:          "检查目录是否存在，并确认应用对该目录有读取权限。",
		})
		return nil, err
	}
	_ = ResolveLibraryIssue(issueKey)

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
		animePath := filepath.Join(dir.Path, entry.Name())

		if !entry.IsDir() {
			// Check if it's a video file being ignored
			if IsVideoFile(entry.Name()) {
				log.Printf("Scanner: Found video file in root: %s (will treat as standalone)", entry.Name())
			} else {
				continue
			}
		}

		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		// Count videos and Sync Episodes
		fileCount, totalSize := s.syncEpisodes(dir.ID, animePath)

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
				// Trigger Episode Sync for existing anime as well
				s.syncEpisodes(dir.ID, animePath)
			} else {
				// Insert
				title := entry.Name()
				if !entry.IsDir() {
					parsed := parser.ParseFilename(entry.Name())
					if parsed.Title != "" {
						title = parsed.Title
					}
				}

				anime = model.LocalAnime{
					DirectoryID: dir.ID,
					Title:       title,
					Path:        animePath,
					FileCount:   fileCount,
					TotalSize:   totalSize,
				}
				db.DB.Create(&anime)
				res.Added++

				// Double check episodes are linked to new anime ID
				s.syncEpisodesForAnime(&anime)

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

	event.GlobalBus.Publish(event.EventScanComplete, map[string]interface{}{
		"scope":        "directory",
		"directory_id": dir.ID,
		"directory":    dir.Path,
		"added":        res.Added,
		"updated":      res.Updated,
		"deleted":      res.Deleted,
	})
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

func (s *ScannerService) syncEpisodes(dirID uint, animePath string) (int, int64) {
	var anime model.LocalAnime
	if err := db.DB.Where("path = ?", animePath).First(&anime).Error; err != nil {
		// Not found in DB, use temp object for counting
		anime = model.LocalAnime{Path: animePath}
	}
	// If anime not found, we still return counts but episodes won't be linked yet.
	// We call syncEpisodesForAnime after creation.
	return s.syncEpisodesForAnime(&anime)
}

func (s *ScannerService) syncEpisodesForAnime(anime *model.LocalAnime) (int, int64) {
	if anime == nil || anime.Path == "" {
		return 0, 0
	}

	count := 0
	var size int64
	var foundPaths []string

	if err := filepath.WalkDir(anime.Path, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			_ = ReportLibraryIssue(LibraryIssueInput{
				IssueKey:      "scan-entry:" + filepath.Clean(anime.Path),
				IssueType:     LibraryIssueTypeScan,
				Title:         anime.Title,
				DirectoryPath: anime.Path,
				LocalAnimeID:  optionalUint(anime.ID),
				Message:       err.Error(),
				Hint:          "检查文件路径是否仍然存在，或是否有权限读取该目录。",
			})
			return nil
		}
		if !d.IsDir() {
			if IsVideoFile(path) {
				count++
				info, _ := d.Info()
				size += info.Size()
				foundPaths = append(foundPaths, path)

				if anime.ID != 0 {
					// Sync to DB
					var ep model.LocalEpisode
					err := db.DB.Where("path = ?", path).First(&ep).Error
					if err != nil {
						// Create
						parsed := parser.ParseFilename(path)
						ep = model.LocalEpisode{
							LocalAnimeID: anime.ID,
							Title:        parsed.Extension, // Fallback
							EpisodeNum:   parsed.Episode,
							SeasonNum:    parsed.Season,
							Path:         path,
							FileSize:     info.Size(),
							Container:    parsed.Extension,
							ParsedTitle:  parsed.Title,
							ParsedSeason: fmt.Sprintf("S%02d", parsed.Season),
						}
						db.DB.Create(&ep)
					} else {
						// Update if needed (idempotent)
						if ep.LocalAnimeID != anime.ID {
							ep.LocalAnimeID = anime.ID
							db.DB.Save(&ep)
						}
					}
				}
			}
		}
		return nil
	}); err != nil {
		log.Printf("Scanner: WalkDir failed for %s: %v", anime.Path, err)
	}
	_ = ResolveLibraryIssue("scan-entry:" + filepath.Clean(anime.Path))

	// Cleanup episodes no longer on disk
	if anime.ID != 0 {
		if len(foundPaths) > 0 {
			db.DB.Where("local_anime_id = ? AND path NOT IN ?", anime.ID, foundPaths).Delete(&model.LocalEpisode{})
		} else {
			db.DB.Where("local_anime_id = ?", anime.ID).Delete(&model.LocalEpisode{})
		}
	}

	return count, size
}

func optionalUint(v uint) *uint {
	if v == 0 {
		return nil
	}
	return &v
}

func IsVideoFile(path string) bool {
	return parser.IsVideoFile(path)
}
