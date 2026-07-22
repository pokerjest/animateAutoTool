package api

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/safeio"
	"github.com/pokerjest/animateAutoTool/internal/service"
	"gorm.io/gorm"
)

type BackupStats struct {
	SubscriptionCount  int64
	SubscriptionTitles []string
	DownloadLogCount   int64
	LocalAnimeCount    int64
	UserCount          int64
	GlobalConfigCount  int64
	DatabaseSize       string
	LastModified       string
}

func getDBStats(targetDB *gorm.DB, dbPath string) BackupStats {
	var subCount, logCount, localCount, configCount int64
	var titles []string

	// Check if tables exist (handle partial backups)
	if targetDB.Migrator().HasTable(&model.Subscription{}) {
		targetDB.Model(&model.Subscription{}).Count(&subCount)
		targetDB.Model(&model.Subscription{}).Pluck("title", &titles)
	}
	if targetDB.Migrator().HasTable(&model.DownloadLog{}) {
		targetDB.Model(&model.DownloadLog{}).Count(&logCount)
	}
	if targetDB.Migrator().HasTable(&model.SubscriptionRunLog{}) {
		var runLogCount int64
		targetDB.Model(&model.SubscriptionRunLog{}).Count(&runLogCount)
		logCount += runLogCount
	}
	if targetDB.Migrator().HasTable(&model.GlobalConfig{}) {
		targetDB.Model(&model.GlobalConfig{}).Count(&configCount)
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
		GlobalConfigCount:  configCount,
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
		htmlBadRequest(c, "请选择一个备份文件")
		return
	}

	// Save to temp
	tempFile, err := os.CreateTemp("", "restore_analyze_*.db")
	if err != nil {
		htmlServerError(c, "创建临时备份文件", err)
		return
	}
	// No defer remove, kept for Execute

	src, err := file.Open()
	if err != nil {
		htmlServerError(c, "打开上传的备份文件", err)
		return
	}
	defer safeio.Close(src)

	if _, err := io.Copy(tempFile, src); err != nil {
		safeio.Close(tempFile)
		safeio.Remove(tempFile.Name())
		htmlServerError(c, "写入临时备份文件", err)
		return
	}
	if err := tempFile.Close(); err != nil {
		safeio.Remove(tempFile.Name())
		htmlServerError(c, "完成临时备份文件写入", err)
		return
	}
	if !isValidSQLite(tempFile.Name()) {
		safeio.Remove(tempFile.Name())
		htmlBadRequest(c, "无效的数据库备份文件")
		return
	}

	stats, err := service.InspectBackup(tempFile.Name())
	if err != nil {
		safeio.Remove(tempFile.Name())
		htmlBadRequest(c, "无效的数据库备份文件")
		return
	}

	// Return HTML Fragment
	restoreToken := registerRestoreArtifact(tempFile.Name())
	c.HTML(http.StatusOK, "backup_analyze.html", gin.H{
		"Stats":    stats,
		"TempFile": restoreToken,
	})
}

func ExecuteRestoreHandler(c *gin.Context) {
	restoreToken := c.PostForm("temp_file")
	if restoreToken == "" {
		htmlBadRequest(c, "没有可恢复的备份文件")
		return
	}

	tempPath, err := consumeRestoreArtifact(restoreToken)
	if err != nil {
		htmlBadRequest(c, err.Error())
		return
	}
	defer safeio.Remove(tempPath) // Cleanup after attempt

	// Also ensure it's a valid SQLite file before passing to service
	if !isValidSQLite(tempPath) {
		htmlBadRequest(c, "无效的数据库备份文件")
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
		htmlBadRequest(c, "请至少选择一类要恢复的数据")
		return
	}

	// EXECUTE PARALLEL RESTORE
	auditCtx := buildAuditContext(c)
	svc := service.NewRestoreService()
	if err := svc.PerformRestore(tempPath, options); err != nil {
		service.RecordAudit(auditCtx, service.AuditEntry{
			Action:  service.AuditActionBackupRestore,
			Outcome: service.AuditOutcomeFailure,
			Details: map[string]any{"options": options, "error": err.Error()},
		})
		htmlServerError(c, "恢复备份", err)
		return
	}
	service.RecordAudit(auditCtx, service.AuditEntry{
		Action:  service.AuditActionBackupRestore,
		Outcome: service.AuditOutcomeSuccess,
		Details: map[string]any{"options": options},
	})

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
	c.String(http.StatusOK, "备份恢复完成")
}

// Helper duplicated from r2.go if needed, or better export it.
// To avoid duplication, let's keep it here or export in utils.
// For now, implementing locally if r2.go one is private.
func isValidSQLite(path string) bool {
	cleanPath := filepath.Clean(path)
	f, err := os.Open(cleanPath) //nolint:gosec // path is an app-created temporary restore artifact.
	if err != nil {
		return false
	}
	defer safeio.Close(f)

	header := make([]byte, 16)
	if _, err := f.Read(header); err != nil {
		return false
	}
	return string(header) == "SQLite format 3\000"
}

func ExportBackupHandler(c *gin.Context) {
	mode := service.NormalizeBackupMode(c.DefaultQuery("mode", service.BackupModeFull))

	tempFile, err := os.CreateTemp("", "export_*.db")
	if err != nil {
		htmlServerError(c, "创建导出临时文件", err)
		return
	}
	tempPath := tempFile.Name()
	if err := tempFile.Close(); err != nil {
		safeio.Remove(tempPath)
		htmlServerError(c, "完成导出临时文件写入", err)
		return
	}
	defer safeio.Remove(tempPath)

	if err := service.CreateBackupFile(tempPath, mode); err != nil {
		htmlServerError(c, "创建备份文件", err)
		return
	}

	c.FileAttachment(tempPath, service.BackupFilename(mode, time.Now()))
}

func ImportBackupHandler(c *gin.Context) {
	htmlBadRequest(c, "已经禁用直接恢复，请先通过分析/预览流程确认备份内容。")
}
