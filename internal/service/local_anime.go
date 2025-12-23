package service

import (
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"gorm.io/gorm"
)

type LocalAnimeService struct{}

func NewLocalAnimeService() *LocalAnimeService {
	return &LocalAnimeService{}
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

	dir := model.LocalAnimeDirectory{
		Path: path,
	}
	return db.DB.Create(&dir).Error
}

// RemoveDirectory 删除目录
func (s *LocalAnimeService) RemoveDirectory(id uint) error {
	return db.DB.Transaction(func(tx *gorm.DB) error {
		// 删除关联的 Anime
		if err := tx.Where("directory_id = ?", id).Delete(&model.LocalAnime{}).Error; err != nil {
			return err
		}
		// 删除目录
		if err := tx.Delete(&model.LocalAnimeDirectory{}, id).Error; err != nil {
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
	log.Printf("Scanning directory: %s", rootPath)

	entries, err := os.ReadDir(rootPath)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		animePath := filepath.Join(rootPath, entry.Name())
		fileCount, totalSize := s.countVideos(animePath)

		if fileCount > 0 {
			// Upsert LocalAnime
			var anime model.LocalAnime
			err := db.DB.Where("directory_id = ? AND path = ?", dirID, animePath).First(&anime).Error
			if err == nil {
				// Update
				anime.FileCount = fileCount
				anime.TotalSize = totalSize
				db.DB.Save(&anime)
			} else {
				// Create
				anime = model.LocalAnime{
					DirectoryID: dirID,
					Title:       entry.Name(),
					Path:        animePath,
					FileCount:   fileCount,
					TotalSize:   totalSize,
				}
				db.DB.Create(&anime)
			}
		}
	}

	return nil
}

func (s *LocalAnimeService) countVideos(path string) (int, int64) {
	count := 0
	var size int64 = 0

	filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			ext := strings.ToLower(filepath.Ext(d.Name()))
			if isVideoExt(ext) {
				count++
				info, _ := d.Info()
				if info != nil {
					size += info.Size()
				}
			}
		}
		return nil
	})

	return count, size
}

func isVideoExt(ext string) bool {
	switch ext {
	case ".mp4", ".mkv", ".avi", ".mov", ".flv", ".wmv":
		return true
	}
	return false
}
