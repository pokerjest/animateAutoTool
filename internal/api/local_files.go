package api

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/parser"
)

// FileInfo 简化的文件信息结构
type FileInfo struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Size int64  `json:"size"`
	Ext  string `json:"ext"`
}

// RenamePreviewItem 重命名预览条目
type RenamePreviewItem struct {
	AnimeName string `json:"anime_name"` // 所属番剧名 (for display)
	Original  string `json:"original"`
	New       string `json:"new"`
	Path      string `json:"path"` // 原完整路径 for execution
}

// RenameRequest 重命名请求体
type RenameRequest struct {
	Pattern string `json:"pattern"` // e.g. "{Title} S{Season}E{Ep}"
	Season  string `json:"season"`  // e.g. "01" (Deprecated at dir level? Or global override?)
	// directory rename might need per-anime season, but for now apply same rule or rely on parser?
	// The user likely wants to standardize naming format.
	// We'll keep Season param but note it applies to everything if used,
	// though ideally we parse season from file or existing logic.
	// For "Batch Organize", usually we just want to format title + ep.
	// Let's assume Season is optional or "01" default.
}

// GetLocalAnimeFilesHandler 获取指定本地番剧的文件列表
func GetLocalAnimeFilesHandler(c *gin.Context) {
	id := c.Param("id")
	var anime model.LocalAnime
	if err := db.DB.First(&anime, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "未找到番剧记录"})
		return
	}

	files, err := listAnimeFiles(anime.Path)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, files)
}

// PreviewDirectoryRenameHandler 预览目录下所有番剧的重命名
func PreviewDirectoryRenameHandler(c *gin.Context) {
	id := c.Param("id")
	var req RenameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	// 1. Get Directory
	var dir model.LocalAnimeDirectory
	if err := db.DB.First(&dir, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "未找到目录记录"})
		return
	}

	// 2. Get All Anime in this Directory
	var animeList []model.LocalAnime
	if err := db.DB.Where("directory_id = ?", dir.ID).Find(&animeList).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Query failed"})
		return
	}

	var allPreviews []RenamePreviewItem

	// 3. Loop and Generate
	for _, anime := range animeList {
		files, err := listAnimeFiles(anime.Path)
		if err != nil {
			continue // Skip bad ones
		}

		previews := generateRenamePreview(files, anime, req)
		for i := range previews {
			previews[i].AnimeName = anime.Title
		}
		allPreviews = append(allPreviews, previews...)
	}

	c.JSON(http.StatusOK, allPreviews)
}

// ApplyDirectoryRenameHandler 执行目录级别的批量重命名
func ApplyDirectoryRenameHandler(c *gin.Context) {
	id := c.Param("id")
	var req RenameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	var dir model.LocalAnimeDirectory
	if err := db.DB.First(&dir, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "未找到目录记录"})
		return
	}

	var animeList []model.LocalAnime
	if err := db.DB.Where("directory_id = ?", dir.ID).Find(&animeList).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Query failed"})
		return
	}

	successCount := 0
	failCount := 0

	for _, anime := range animeList {
		files, err := listAnimeFiles(anime.Path)
		if err != nil {
			continue
		}

		previews := generateRenamePreview(files, anime, req)

		for _, item := range previews {
			if item.New == item.Original {
				continue // Skip unchanged
			}

			oldPath := item.Path
			newPath := filepath.Join(filepath.Dir(oldPath), item.New)

			if err := os.Rename(oldPath, newPath); err != nil {
				fmt.Printf("Rename failed: %s -> %s (%v)\n", oldPath, newPath, err)
				failCount++
			} else {
				successCount++
			}
		}
	}

	msg := fmt.Sprintf("批量整理完成: 成功 %d, 失败 %d", successCount, failCount)
	c.JSON(http.StatusOK, gin.H{"message": msg, "success": successCount, "failed": failCount})
}

// Helpers

func listAnimeFiles(rootPath string) ([]FileInfo, error) {
	var files []FileInfo
	err := filepath.WalkDir(rootPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			// Check if video file (reuse logic conceptually or duplicate simple check)
			ext := strings.ToLower(filepath.Ext(d.Name()))
			if isVideoExt(ext) {
				info, _ := d.Info()
				files = append(files, FileInfo{
					Name: d.Name(),
					Path: path,
					Size: info.Size(),
					Ext:  ext,
				})
			}
		}
		return nil
	})

	// Sort by name
	sort.Slice(files, func(i, j int) bool {
		return files[i].Name < files[j].Name
	})

	return files, err
}

func isVideoExt(ext string) bool {
	switch ext {
	case ".mp4", ".mkv", ".avi", ".mov", ".flv", ".wmv", ".ts", ".rmvb", ".webm", ".m2ts":
		return true
	}
	return false
}

func generateRenamePreview(files []FileInfo, anime model.LocalAnime, req RenameRequest) []RenamePreviewItem {
	var results []RenamePreviewItem

	// Default pattern if empty
	pattern := req.Pattern
	if pattern == "" {
		pattern = "{Title} - S{Season}E{Ep}.{Ext}"
	}

	season := req.Season
	if season == "" {
		season = "01"
	}

	// Extract Year from AirDate (e.g., "2024-01-07")
	year := ""
	if len(anime.AirDate) >= 4 {
		year = anime.AirDate[:4]
	}

	for _, f := range files {
		// Parse Episode Info
		ep := parser.ParseTitle(f.Name)

		if ep.EpisodeNum == "" {
			results = append(results, RenamePreviewItem{
				Original: f.Name,
				New:      f.Name, // No change
				Path:     f.Path,
			})
			continue
		}

		newName := pattern
		// Replace variables
		newName = strings.ReplaceAll(newName, "{Title}", anime.Title)
		newName = strings.ReplaceAll(newName, "{Season}", season)
		newName = strings.ReplaceAll(newName, "{Year}", year)

		subGroup := ep.SubGroup

		newName = strings.ReplaceAll(newName, "{SubGroup}", subGroup)

		// Pad Ep to 2 digits if needed
		epNum := ep.EpisodeNum
		if len(epNum) == 1 {
			epNum = "0" + epNum
		}
		newName = strings.ReplaceAll(newName, "{Ep}", epNum)

		// Ext
		ext := strings.TrimPrefix(f.Ext, ".")
		newName = strings.ReplaceAll(newName, "{Ext}", ext)

		// Safety check: ensure extension is present in new name
		if !strings.HasSuffix(newName, "."+ext) {
			newName += "." + ext
		}

		results = append(results, RenamePreviewItem{
			Original: f.Name,
			New:      newName,
			Path:     f.Path,
		})
	}
	return results
}
