package service

import (
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/pokerjest/animateAutoTool/internal/bangumi"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"gorm.io/gorm"
)

type LocalAnimeService struct{}

func NewLocalAnimeService() *LocalAnimeService {
	s := &LocalAnimeService{}
	// s.CleanupGarbage() // Don't run on every request!
	return s
}

// CleanupGarbage 清理数据库中的无效数据
func (s *LocalAnimeService) CleanupGarbage() {
	// 1. 删除没有视频文件的“垃圾”记录
	if err := db.DB.Unscoped().Where("file_count = 0").Delete(&model.LocalAnime{}).Error; err != nil {
		log.Printf("Cleanup: Failed to remove empty anime entries: %v", err)
	} else {
		log.Println("Cleanup: Removed empty anime entries from DB")
	}

	// 2. 删除孤儿记录 - 当目录被删但番剧没删掉时
	var dirIDs []uint
	db.DB.Model(&model.LocalAnimeDirectory{}).Pluck("id", &dirIDs)
	if len(dirIDs) > 0 {
		// Delete anime where directory_id is not in the list of existing directories
		if err := db.DB.Unscoped().Where("directory_id NOT IN ?", dirIDs).Delete(&model.LocalAnime{}).Error; err != nil {
			log.Printf("Cleanup: Failed to remove orphan entries: %v", err)
		} else {
			log.Println("Cleanup: Removed orphan anime entries")
		}
	} else {
		// If no directories exist, all anime are orphans
		db.DB.Unscoped().Where("1 = 1").Delete(&model.LocalAnime{})
		log.Println("Cleanup: No directories found, wiped all local anime")
	}
}

// AddDirectory 添加一个新的根目录
func (s *LocalAnimeService) AddDirectory(path string) error {
	path = filepath.Clean(path)
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return os.ErrInvalid // 不是目录
	}

	// Check if exists (including soft-deleted)
	var existing model.LocalAnimeDirectory
	if err := db.DB.Unscoped().Where("path = ?", path).First(&existing).Error; err == nil {
		// Found existing record
		if existing.DeletedAt.Valid {
			// It was soft-deleted. Instead of restoring, we HARD DELETE it to allow a fresh creation.
			// This avoids issues with GORM updates and clean slate.
			log.Printf("Removing stale soft-deleted directory to allow fresh add: %s", path)
			if err := db.DB.Unscoped().Delete(&existing).Error; err != nil {
				return err
			}
			// Fallthrough to Create new below
		} else {
			// Already exists and active, nothing to do
			return nil
		}
	}

	// Not found, create new
	dir := model.LocalAnimeDirectory{
		Path: path,
	}
	return db.DB.Create(&dir).Error
}

// RemoveDirectory 删除目录
func (s *LocalAnimeService) RemoveDirectory(id uint) error {
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

// ScanAll 扫描所有已配置的目录
func (s *LocalAnimeService) ScanAll() error {
	var dirs []model.LocalAnimeDirectory
	if err := db.DB.Find(&dirs).Error; err != nil {
		return err
	}

	for _, d := range dirs {
		if err := s.ScanDirectory(d.ID, d.Path); err != nil {
			log.Printf("Error scanning directory %s: %v", d.Path, err)
			// 继续扫描下一个，不中断
		}
	}
	return nil
}

// ScanDirectory 扫描指定目录并将结果存入 DB
func (s *LocalAnimeService) ScanDirectory(dirID uint, rootPath string) error {
	log.Printf("DEBUG: ScanDirectory started for: %s (ID: %d)", rootPath, dirID)

	entries, err := os.ReadDir(rootPath)
	if err != nil {
		log.Printf("ERROR: ReadDir failed: %v", err)
		return err
	}
	log.Printf("DEBUG: Found %d entries in %s", len(entries), rootPath)

	// Track found paths to handle deletions
	foundPaths := make(map[string]bool)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		// Check if it's a hidden folder
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		animePath := filepath.Join(rootPath, entry.Name())
		foundPaths[animePath] = true

		fileCount, totalSize := s.countVideos(animePath)
		if fileCount > 0 {
			// Check if exists
			var anime model.LocalAnime
			if err := db.DB.Where("path = ?", animePath).First(&anime).Error; err == nil {
				// Update stats
				anime.FileCount = fileCount
				anime.TotalSize = totalSize
				// Only update DirectoryID if changed (unlikely here but safe)
				anime.DirectoryID = dirID
				db.DB.Save(&anime)
			} else {
				// Create new
				anime = model.LocalAnime{
					DirectoryID: dirID,
					Title:       entry.Name(),
					Path:        animePath,
					FileCount:   fileCount,
					TotalSize:   totalSize,
				}

				// Initial Metadata Fetch
				s.enrichAnimeMetadata(&anime)

				if err := db.DB.Create(&anime).Error; err != nil {
					log.Printf("ERROR: Failed to create anime record: %v", err)
				}
			}
		}
	}

	// Remove stale entries for this directory
	// We need to fetch all existing for this dir first?
	// Or just delete where directory_id = ? AND path NOT IN ?
	// Careful with large IN clauses, but usually local anime count is manageable (<1000 per dir)
	if len(foundPaths) > 0 {
		var paths []string
		for p := range foundPaths {
			paths = append(paths, p)
		}
		db.DB.Where("directory_id = ? AND path NOT IN ?", dirID, paths).Delete(&model.LocalAnime{})
	} else {
		// No valid folders found, wipe all for this dir
		db.DB.Where("directory_id = ?", dirID).Delete(&model.LocalAnime{})
	}

	return nil
}

// enrichAnimeMetadata tries to find Bangumi ID and valid Title/Image
func (s *LocalAnimeService) enrichAnimeMetadata(anime *model.LocalAnime) {
	bgmClient := bangumi.NewClient("", "", "")

	// Clean title logic
	queryTitle := CleanTitle(anime.Title)
	log.Printf("DEBUG: Cleaned title search: '%s' -> '%s'", anime.Title, queryTitle)

	if res, err := bgmClient.SearchSubject(queryTitle); err == nil && res != nil {
		anime.BangumiID = res.ID

		// Default to search result info
		if res.Images.Large != "" {
			anime.Image = res.Images.Large
		} else if res.Images.Common != "" {
			anime.Image = res.Images.Common
		}
		if res.NameCN != "" {
			anime.Title = res.NameCN
		} else if res.Name != "" {
			anime.Title = res.Name
		}

		// Try Fetch details
		if subject, err := bgmClient.GetSubject(res.ID); err == nil {
			if subject.NameCN != "" {
				anime.Title = subject.NameCN
			} else if subject.Name != "" {
				anime.Title = subject.Name
			}

			if subject.Images.Large != "" {
				anime.Image = subject.Images.Large
			} else if subject.Images.Common != "" {
				anime.Image = subject.Images.Common
			}
		}
	}
}

// CleanTitle removes common tags like [Group] or [1080p] to get a search-friendly title
func CleanTitle(raw string) string {
	// 1. Remove leading [...]
	// 2. Remove trailing [...] or (...)
	// 3. Remove date patterns like (2024), [2024], 2024

	// Simple heuristic: Take the "middle" part if wrapped in tags, or just the string
	// [Group] Title [Res] -> Title

	s := raw
	// Remove leading [...]
	s = regexp.MustCompile(`^\[.*?\]\s*`).ReplaceAllString(s, "")
	// Remove trailing [...]
	s = regexp.MustCompile(`\s*\[.*?\]$`).ReplaceAllString(s, "")
	// Remove trailing (...)
	s = regexp.MustCompile(`\s*\(.*?\)$`).ReplaceAllString(s, "")

	return strings.TrimSpace(s)
}

func (s *LocalAnimeService) countVideos(path string) (int, int64) {
	count := 0
	var size int64 = 0

	_ = filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			log.Printf("ERROR: WalkDir error at %s: %v", p, err)
			return nil
		}
		if !d.IsDir() {
			// Skip hidden files/system files
			if strings.HasPrefix(d.Name(), ".") {
				return nil
			}

			ext := strings.ToLower(filepath.Ext(d.Name()))
			if isVideoExt(ext) {
				count++
				info, _ := d.Info()
				if info != nil {
					size += info.Size()
				}
			} else {
				// Only log first few skipped files to avoid spam
				if count == 0 {
					log.Printf("DEBUG: Skipped non-video file: %s", d.Name())
				}
			}
		}
		return nil
	})

	return count, size
}

func isVideoExt(ext string) bool {
	switch ext {
	// Added .!qB, .bc! for partial downloads
	case ".mp4", ".mkv", ".avi", ".mov", ".flv", ".wmv", ".ts", ".rmvb", ".webm", ".m2ts", ".!qb", ".bc!":
		return true
	}
	return false
}
