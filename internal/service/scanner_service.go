package service

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"

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
	st := localAnimeStore()
	if st == nil {
		return gorm.ErrInvalidDB
	}
	dirs, err := st.ListDirectories()
	if err != nil {
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

	st := localAnimeStore()
	if st == nil {
		return nil, gorm.ErrInvalidDB
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
			anime, findErr := st.FindAnimeByPath(animePath)
			if findErr == nil && anime != nil {
				if anime.FileCount != fileCount || anime.TotalSize != totalSize {
					anime.FileCount = fileCount
					anime.TotalSize = totalSize
					if err := st.SaveAnime(anime); err != nil {
						log.Printf("Scanner: Save anime failed for %s: %v", animePath, err)
					}
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

				newAnime := &model.LocalAnime{
					DirectoryID: dir.ID,
					Title:       title,
					Path:        animePath,
					FileCount:   fileCount,
					TotalSize:   totalSize,
				}
				if err := st.CreateAnime(newAnime); err != nil {
					log.Printf("Scanner: Create anime failed for %s: %v", animePath, err)
					continue
				}
				res.Added++

				// Double check episodes are linked to new anime ID
				s.syncEpisodesForAnime(newAnime)

				// Publish New Anime Event (can trigger Metadata Fetch)
				event.GlobalBus.Publish(event.EventMetadataUpdated, map[string]interface{}{
					"type":  "new_anime",
					"id":    newAnime.ID,
					"title": newAnime.Title,
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
	st := localAnimeStore()
	if st == nil {
		return
	}
	if err := st.CleanupOrphans(); err != nil {
		log.Printf("Cleanup: CleanupOrphans failed: %v", err)
	}
}

// AddDirectory 添加一个新的根目录
func (s *ScannerService) AddDirectory(path string) error {
	log.Printf("Adding directory: %s (Skipping strict existence check for cross-platform support)", path)

	st := localAnimeStore()
	if st == nil {
		return gorm.ErrInvalidDB
	}

	existing, err := st.FindDirectoryByPath(path, true)
	if err == nil && existing != nil {
		if existing.DeletedAt.Valid {
			log.Printf("Removing stale soft-deleted directory to allow fresh add: %s", path)
			if err := st.HardDeleteDirectory(existing); err != nil {
				return err
			}
		} else {
			return nil
		}
	} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	dir := &model.LocalAnimeDirectory{Path: path}
	return st.CreateDirectory(dir)
}

// RemoveDirectory 删除目录
func (s *ScannerService) RemoveDirectory(id uint) error {
	st := localAnimeStore()
	if st == nil {
		return gorm.ErrInvalidDB
	}
	return st.RemoveDirectoryWithAnimes(id)
}

func (s *ScannerService) syncEpisodes(dirID uint, animePath string) (int, int64) {
	st := localAnimeStore()
	var anime *model.LocalAnime
	if st != nil {
		if found, err := st.FindAnimeByPath(animePath); err == nil {
			anime = found
		}
	}
	if anime == nil {
		anime = &model.LocalAnime{Path: animePath}
	}
	// If anime not found, we still return counts but episodes won't be linked yet.
	// We call syncEpisodesForAnime after creation.
	return s.syncEpisodesForAnime(anime)
}

func (s *ScannerService) syncEpisodesForAnime(anime *model.LocalAnime) (int, int64) {
	if anime == nil || anime.Path == "" {
		return 0, 0
	}

	st := localAnimeStore()

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

				if anime.ID != 0 && st != nil {
					ep, findErr := st.FindEpisodeByPath(path)
					if findErr != nil {
						parsed := parser.ParseFilename(path)
						newEp := &model.LocalEpisode{
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
						if err := st.CreateEpisode(newEp); err != nil {
							log.Printf("Scanner: Create episode failed for %s: %v", path, err)
						}
					} else if ep.LocalAnimeID != anime.ID {
						ep.LocalAnimeID = anime.ID
						if err := st.SaveEpisode(ep); err != nil {
							log.Printf("Scanner: Save episode failed for %s: %v", path, err)
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
	if anime.ID != 0 && st != nil {
		if err := st.DeleteEpisodesNotInPaths(anime.ID, foundPaths); err != nil {
			log.Printf("Scanner: cleanup orphan episodes failed for %s: %v", anime.Path, err)
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
