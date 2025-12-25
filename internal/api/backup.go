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

	// DANGEROUS ZONE: Close DB and Swap
	if err := db.CloseDB(); err != nil {
		c.String(http.StatusInternalServerError, "Failed to close database: "+err.Error())
		return
	}

	// Overwrite
	input, err := os.ReadFile(tempPath)
	if err != nil {
		db.InitDB(db.CurrentDBPath)
		c.String(http.StatusInternalServerError, "Failed to read restore file")
		return
	}

	if err := os.WriteFile(db.CurrentDBPath, input, 0644); err != nil {
		db.InitDB(db.CurrentDBPath)
		c.String(http.StatusInternalServerError, "Failed to write database file: "+err.Error())
		return
	}

	// Re-open DB
	db.InitDB(db.CurrentDBPath)

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

	// 2. Generate Backup
	if err := createBackupFile(tempPath); err != nil {
		c.String(http.StatusInternalServerError, "Failed to create backup: "+err.Error())
		return
	}

	// 3. Stream File

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
	db.InitDB(db.CurrentDBPath)
	c.Header("HX-Redirect", "/backup") // Refresh page
	c.String(http.StatusOK, "Restore successful")
}

// createBackupFile generates a standard backup database at destPath
func createBackupFile(destPath string) error {
	debugLog("DEBUG: Creating backup at %s using Source DB: %s", destPath, db.CurrentDBPath)

	exportDB, err := gorm.Open(sqlite.Open(destPath), &gorm.Config{})
	if err != nil {
		debugLog("DEBUG: Failed to open export DB: %v", err)
		return err
	}

	// Migrate All Tables
	if err := exportDB.AutoMigrate(
		&model.Subscription{},
		&model.DownloadLog{},
		&model.GlobalConfig{},
		&model.LocalAnimeDirectory{},
		&model.LocalAnime{},
		&model.AnimeMetadata{},
	); err != nil {
		debugLog("DEBUG: AutoMigrate failed: %v", err)
		return err
	}

	// Copy Data
	// Subscriptions
	var subs []model.Subscription
	if err := db.DB.Find(&subs).Error; err == nil && len(subs) > 0 {
		result := exportDB.Create(&subs)
		debugLog("DEBUG: Exported %d Subscriptions (Error: %v)", result.RowsAffected, result.Error)
	} else {
		debugLog("DEBUG: No Subscriptions found or error: %v", err)
	}

	// Logs
	var logs []model.DownloadLog
	if err := db.DB.Find(&logs).Error; err == nil && len(logs) > 0 {
		result := exportDB.Create(&logs)
		debugLog("DEBUG: Exported %d DownloadLogs (Error: %v)", result.RowsAffected, result.Error)
	} else {
		debugLog("DEBUG: No DownloadLogs found or error: %v", err)
	}

	// Configs
	var configs []model.GlobalConfig
	if err := db.DB.Find(&configs).Error; err == nil && len(configs) > 0 {
		result := exportDB.Create(&configs)
		debugLog("DEBUG: Exported %d GlobalConfigs (Error: %v)", result.RowsAffected, result.Error)
	} else {
		debugLog("DEBUG: No GlobalConfigs found or error: %v", err)
	}

	// LocalAnimeDirectory
	var dirs []model.LocalAnimeDirectory
	if err := db.DB.Find(&dirs).Error; err == nil && len(dirs) > 0 {
		result := exportDB.Create(&dirs)
		debugLog("DEBUG: Exported %d LocalAnimeDirectories (Error: %v)", result.RowsAffected, result.Error)
	}

	// LocalAnime
	var localAnimes []model.LocalAnime
	if err := db.DB.Find(&localAnimes).Error; err == nil && len(localAnimes) > 0 {
		result := exportDB.Create(&localAnimes)
		debugLog("DEBUG: Exported %d LocalAnimes (Error: %v)", result.RowsAffected, result.Error)
	}

	// AnimeMetadata
	var metadatas []model.AnimeMetadata
	if err := db.DB.Find(&metadatas).Error; err == nil && len(metadatas) > 0 {
		result := exportDB.Create(&metadatas)
		debugLog("DEBUG: Exported %d AnimeMetadatas (Error: %v)", result.RowsAffected, result.Error)
	}

	sqlDB, _ := exportDB.DB()
	sqlDB.Close()

	// Verify file size
	fi, err := os.Stat(destPath)
	if err == nil {
		debugLog("DEBUG: Final Backup Size: %d bytes", fi.Size())
	}

	return nil
}
