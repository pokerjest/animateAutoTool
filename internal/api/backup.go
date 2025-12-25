package api

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"gorm.io/gorm"
)

type BackupStats struct {
	SubscriptionCount  int64
	SubscriptionTitles []string
	DownloadLogCount   int64
	LocalAnimeCount    int64
	DatabaseSize       string
	LastModified       string
}

func getDBStats(targetDB *gorm.DB, dbPath string) BackupStats {
	var subCount, logCount, localCount int64
	var titles []string

	// Check if tables exist (handle partial backups)
	if targetDB.Migrator().HasTable(&model.Subscription{}) {
		targetDB.Model(&model.Subscription{}).Count(&subCount)
		targetDB.Model(&model.Subscription{}).Pluck("title", &titles)
	}
	if targetDB.Migrator().HasTable(&model.DownloadLog{}) {
		targetDB.Model(&model.DownloadLog{}).Count(&logCount)
	}
	if targetDB.Migrator().HasTable(&model.LocalAnime{}) {
		targetDB.Model(&model.LocalAnime{}).Count(&localCount)
	}

	info, err := os.Stat(dbPath)
	size := "Unknown"
	modTime := "Unknown"
	if err == nil {
		size = fmt.Sprintf("%.2f MB", float64(info.Size())/1024/1024)
		modTime = info.ModTime().Format("2006-01-02 15:04:05")
	}

	return BackupStats{
		SubscriptionCount:  subCount,
		SubscriptionTitles: titles,
		DownloadLogCount:   logCount,
		LocalAnimeCount:    localCount,
		DatabaseSize:       size,
		LastModified:       modTime,
	}
}

func BackupPageHandler(c *gin.Context) {
	skip := isHTMX(c)

	stats := getDBStats(db.DB, db.CurrentDBPath)

	c.HTML(http.StatusOK, "backup.html", gin.H{
		"SkipLayout": skip,
		"Stats":      stats,
	})
}

func AnalyzeBackupHandler(c *gin.Context) {
	file, err := c.FormFile("backup_file")
	if err != nil {
		c.String(http.StatusBadRequest, "Please select a file")
		return
	}

	// Save to temp
	tempFile, err := os.CreateTemp("", "restore_analyze_*.db")
	if err != nil {
		c.String(http.StatusInternalServerError, "Failed to create temp file")
		return
	}
	// No defer remove, kept for Execute

	src, err := file.Open()
	if err != nil {
		c.String(http.StatusInternalServerError, "Failed to open uploaded file")
		return
	}
	defer src.Close()

	if _, err := io.Copy(tempFile, src); err != nil {
		c.String(http.StatusInternalServerError, "Failed to write temp file")
		return
	}
	tempFile.Close()

	// Open Temp DB
	tempDB, err := gorm.Open(sqlite.Open(tempFile.Name()), &gorm.Config{})
	if err != nil {
		os.Remove(tempFile.Name())
		c.String(http.StatusBadRequest, "Invalid Database File")
		return
	}

	// Get Stats
	stats := getDBStats(tempDB, tempFile.Name())

	// Close Temp DB
	sqlDB, _ := tempDB.DB()
	sqlDB.Close()

	// Return HTML Fragment
	c.HTML(http.StatusOK, "backup_analyze.html", gin.H{
		"Stats":    stats,
		"TempFile": tempFile.Name(),
	})
}

func ExecuteRestoreHandler(c *gin.Context) {
	tempPath := c.PostForm("temp_file")
	if tempPath == "" {
		c.String(http.StatusBadRequest, "No restore file specified")
		return
	}
	defer os.Remove(tempPath) // Cleanup after attempt

	// Verify file exists
	if _, err := os.Stat(tempPath); os.IsNotExist(err) {
		c.String(http.StatusBadRequest, "Restore file expired or not found")
		return
	}

	// Read restore options from form
	restoreConfigs := c.PostForm("restore_configs") == "on"
	restoreMetadata := c.PostForm("restore_metadata") == "on"
	restoreSubscriptions := c.PostForm("restore_subscriptions") == "on"
	restoreLogs := c.PostForm("restore_logs") == "on"
	restoreLocal := c.PostForm("restore_local") == "on"

	// Validate at least one option selected
	if !restoreConfigs && !restoreMetadata && !restoreSubscriptions && !restoreLogs && !restoreLocal {
		c.String(http.StatusBadRequest, "Please select at least one table to restore")
		return
	}

	// Open backup database
	backupDB, err := gorm.Open(sqlite.Open(tempPath), &gorm.Config{})
	if err != nil {
		c.String(http.StatusBadRequest, "Invalid backup file: "+err.Error())
		return
	}
	defer func() {
		sqlDB, _ := backupDB.DB()
		sqlDB.Close()
	}()

	// Perform selective restore
	if restoreConfigs {
		// Clear current configs
		if err := db.DB.Exec("DELETE FROM global_configs").Error; err != nil {
			c.String(http.StatusInternalServerError, "Failed to clear configs: "+err.Error())
			return
		}
		// Copy from backup
		var configs []model.GlobalConfig
		if err := backupDB.Find(&configs).Error; err == nil && len(configs) > 0 {
			db.DB.Create(&configs)
		}
	}

	if restoreMetadata {
		// Clear current metadata
		if err := db.DB.Exec("DELETE FROM anime_metadata").Error; err != nil {
			c.String(http.StatusInternalServerError, "Failed to clear metadata: "+err.Error())
			return
		}
		// Copy from backup
		var metadata []model.AnimeMetadata
		if err := backupDB.Find(&metadata).Error; err == nil && len(metadata) > 0 {
			db.DB.Create(&metadata)
		}
	}

	if restoreSubscriptions {
		// Clear current subscriptions
		if err := db.DB.Exec("DELETE FROM subscriptions").Error; err != nil {
			c.String(http.StatusInternalServerError, "Failed to clear subscriptions: "+err.Error())
			return
		}
		// Copy from backup
		var subs []model.Subscription
		if err := backupDB.Find(&subs).Error; err == nil && len(subs) > 0 {
			db.DB.Create(&subs)
		}
	}

	if restoreLogs {
		// Clear current logs
		if err := db.DB.Exec("DELETE FROM download_logs").Error; err != nil {
			c.String(http.StatusInternalServerError, "Failed to clear logs: "+err.Error())
			return
		}
		// Copy from backup
		var logs []model.DownloadLog
		if err := backupDB.Find(&logs).Error; err == nil && len(logs) > 0 {
			db.DB.Create(&logs)
		}
	}

	if restoreLocal {
		// Clear current local anime data
		if err := db.DB.Exec("DELETE FROM local_anime_directories").Error; err != nil {
			c.String(http.StatusInternalServerError, "Failed to clear local directories: "+err.Error())
			return
		}
		if err := db.DB.Exec("DELETE FROM local_animes").Error; err != nil {
			c.String(http.StatusInternalServerError, "Failed to clear local animes: "+err.Error())
			return
		}
		// Copy from backup
		var dirs []model.LocalAnimeDirectory
		if err := backupDB.Find(&dirs).Error; err == nil && len(dirs) > 0 {
			db.DB.Create(&dirs)
		}
		var animes []model.LocalAnime
		if err := backupDB.Find(&animes).Error; err == nil && len(animes) > 0 {
			db.DB.Create(&animes)
		}
	}

	c.Header("HX-Redirect", "/backup")
	c.String(http.StatusOK, "Restore successful")
}

func ExportBackupHandler(c *gin.Context) {
	// Create Filtered Backup (Exclude Local Anime)

	// 1. Create Temp DB
	tempFile, err := os.CreateTemp("", "export_*.db")
	if err != nil {
		c.String(http.StatusInternalServerError, "Failed to create temp export file")
		return
	}
	tempPath := tempFile.Name()
	tempFile.Close() // Close file handle, let gorm open it
	defer os.Remove(tempPath)

	exportDB, err := gorm.Open(sqlite.Open(tempPath), &gorm.Config{})
	if err != nil {
		c.String(http.StatusInternalServerError, "Failed to open export DB: "+err.Error())
		return
	}

	// 2. Migrate All Tables
	err = exportDB.AutoMigrate(
		&model.Subscription{},
		&model.DownloadLog{},
		&model.GlobalConfig{},
		&model.LocalAnimeDirectory{},
		&model.LocalAnime{},
		&model.AnimeMetadata{},
	)
	if err != nil {
		c.String(http.StatusInternalServerError, "Failed to migrate export DB: "+err.Error())
		return
	}

	// 3. Copy Data
	// Subscriptions
	var subs []model.Subscription
	if err := db.DB.Find(&subs).Error; err == nil && len(subs) > 0 {
		exportDB.Create(&subs)
	}

	// Logs
	var logs []model.DownloadLog
	if err := db.DB.Find(&logs).Error; err == nil && len(logs) > 0 {
		exportDB.Create(&logs)
	}

	// Configs
	var configs []model.GlobalConfig
	if err := db.DB.Find(&configs).Error; err == nil && len(configs) > 0 {
		exportDB.Create(&configs)
	}

	// Local Anime Directories
	var dirs []model.LocalAnimeDirectory
	if err := db.DB.Find(&dirs).Error; err == nil && len(dirs) > 0 {
		exportDB.Create(&dirs)
	}

	// Local Animes
	var animes []model.LocalAnime
	if err := db.DB.Find(&animes).Error; err == nil && len(animes) > 0 {
		exportDB.Create(&animes)
	}

	// Anime Metadata (includes poster image BLOBs)
	var metadata []model.AnimeMetadata
	if err := db.DB.Find(&metadata).Error; err == nil && len(metadata) > 0 {
		exportDB.Create(&metadata)
	}

	// Close Export DB
	sqlDB, _ := exportDB.DB()
	sqlDB.Close()

	// 4. Stream File
	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("animateData_filtered_%s.db", timestamp)

	c.FileAttachment(tempPath, filename)
}

func ImportBackupHandler(c *gin.Context) {
	file, err := c.FormFile("backup_file")
	if err != nil {
		c.String(http.StatusBadRequest, "No file uploaded")
		return
	}

	// Save to temp
	tempFile, err := os.CreateTemp("", "restore_*.db")
	if err != nil {
		c.String(http.StatusInternalServerError, "Failed to create temp file")
		return
	}
	defer os.Remove(tempFile.Name())

	src, err := file.Open()
	if err != nil {
		c.String(http.StatusInternalServerError, "Failed to open uploaded file")
		return
	}
	defer src.Close()

	if _, err := io.Copy(tempFile, src); err != nil {
		c.String(http.StatusInternalServerError, "Failed to write temp file")
		return
	}
	tempFile.Close() // Close file handle

	// DANGEROUS ZONE: Close DB and Swap
	if err := db.CloseDB(); err != nil {
		c.String(http.StatusInternalServerError, "Failed to close database: "+err.Error())
		return
	}

	// Backup current DB just in case?
	// Skip for now, user wants restore.

	// Overwrite
	input, err := os.ReadFile(tempFile.Name())
	if err != nil {
		// Try to reopen DB if fail
		db.InitDB(db.CurrentDBPath)
		c.String(http.StatusInternalServerError, "Failed to read temp file during swap")
		return
	}

	if err := os.WriteFile(db.CurrentDBPath, input, 0644); err != nil {
		// Try to reopen DB if fail
		db.InitDB(db.CurrentDBPath)
		c.String(http.StatusInternalServerError, "Failed to write database file: "+err.Error())
		return
	}

	// Re-open DB
	db.InitDB(db.CurrentDBPath)

	c.Header("HX-Redirect", "/backup") // Refresh page
	c.String(http.StatusOK, "Restore successful")
}
