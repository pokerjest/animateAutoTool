package api

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/service"
	"gorm.io/gorm"
)

type BackupStats struct {
	SubscriptionCount  int64
	SubscriptionTitles []string
	DownloadLogCount   int64
	LocalAnimeCount    int64
	UserCount          int64
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

	var userCount int64
	if targetDB.Migrator().HasTable(&model.User{}) {
		targetDB.Model(&model.User{}).Count(&userCount)
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
		UserCount:          userCount,
		DatabaseSize:       size,
		LastModified:       modTime,
	}
}

func BackupPageHandler(c *gin.Context) {
	skip := IsHTMX(c)

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
	// Also ensure it's a valid SQLite file before passing to service
	if !isValidSQLite(tempPath) {
		c.String(http.StatusBadRequest, "Invalid Database File")
		return
	}

	// Read restore options from form
	options := service.RestoreOptions{
		Configs:       c.PostForm("restore_configs") == "on",
		Metadata:      c.PostForm("restore_metadata") == "on",
		Subscriptions: c.PostForm("restore_subscriptions") == "on",
		Logs:          c.PostForm("restore_logs") == "on",
		Local:         c.PostForm("restore_local") == "on",
		Users:         c.PostForm("restore_users") == "on",
		RegenerateNFO: c.PostForm("restore_nfo") == "on",
	}

	// Validate at least one option selected
	if !options.Configs && !options.Metadata && !options.Subscriptions && !options.Logs && !options.Local && !options.Users {
		c.String(http.StatusBadRequest, "Please select at least one table to restore")
		return
	}

	// EXECUTE PARALLEL RESTORE
	svc := service.NewRestoreService()
	if err := svc.PerformRestore(tempPath, options); err != nil {
		c.String(http.StatusInternalServerError, fmt.Sprintf("Restore Failed: %v", err))
		return
	}

	// Optional: Regenerate NFOs
	if options.RegenerateNFO {
		go func() {
			log.Println("Restore: Triggering NFO regeneration...")
			metaSvc := service.NewMetadataService()
			count, err := metaSvc.RegenerateAllNFOs()
			if err != nil {
				log.Printf("Restore: NFO regeneration failed: %v", err)
			} else {
				log.Printf("Restore: NFO regeneration completed. Processed %d series.", count)
			}
		}()
	}

	// Success response: Send HTMX trigger or redirect
	c.Header("HX-Redirect", "/backup")
	c.String(http.StatusOK, "Restore completed successfully!")
}

// Helper duplicated from r2.go if needed, or better export it.
// To avoid duplication, let's keep it here or export in utils.
// For now, implementing locally if r2.go one is private.
func isValidSQLite(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	header := make([]byte, 16)
	if _, err := f.Read(header); err != nil {
		return false
	}
	return string(header) == "SQLite format 3\000"
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

	if err := os.WriteFile(db.CurrentDBPath, input, 0600); err != nil {
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

	// For SQLite, the most reliable backup method is simply copying the file
	// This preserves ALL data including blobs, constraints, indexes, etc.
	input, err := os.ReadFile(db.CurrentDBPath)
	if err != nil {
		debugLog("DEBUG: Failed to read source DB: %v", err)
		return fmt.Errorf("failed to read source database: %v", err)
	}

	if err := os.WriteFile(destPath, input, 0600); err != nil {
		debugLog("DEBUG: Failed to write backup file: %v", err)
		return fmt.Errorf("failed to write backup: %v", err)
	}

	// Verify file size
	fi, err := os.Stat(destPath)
	if err == nil {
		debugLog("DEBUG: Final Backup Size: %d bytes (%.2f MB)", fi.Size(), float64(fi.Size())/1024/1024)
	}

	return nil
}
