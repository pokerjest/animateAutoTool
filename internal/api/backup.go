package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"github.com/pokerjest/animateAutoTool/internal/safeio"
	"github.com/pokerjest/animateAutoTool/internal/service"
	"gorm.io/gorm"
)

const (
	maxBackupFileBytes    int64 = 8 << 30
	maxBackupRequestBytes int64 = maxBackupFileBytes + 1<<20
)

var errBackupFileTooLarge = errors.New("备份文件超过 8 GiB 限制")

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

type backupExportRequest struct {
	backupPasswordRequest
	Mode string `json:"mode" form:"mode"`
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
	if targetDB.Migrator().HasTable(&model.SubscriptionResource{}) {
		var resourceCount int64
		targetDB.Model(&model.SubscriptionResource{}).Count(&resourceCount)
		logCount += resourceCount
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
	file, err := backupUploadFile(c)
	if err != nil {
		if errors.Is(err, errBackupFileTooLarge) {
			htmlStringError(c, http.StatusRequestEntityTooLarge, err.Error())
			return
		}
		htmlBadRequest(c, "请选择一个备份文件")
		return
	}

	sourcePath, err := saveUploadedBackup(file)
	if err != nil {
		htmlServerError(c, "保存上传的备份文件", err)
		return
	}

	password := ""
	if service.IsBackupArchive(sourcePath) {
		password, err = resolveBackupRestorePassword(backupPasswordRequestFromForm(c))
		if err != nil {
			safeio.Remove(sourcePath)
			htmlBadRequest(c, err.Error())
			return
		}
	}
	stats, databasePath, err := inspectBackupForRestore(sourcePath, password)
	if err != nil {
		htmlBadRequest(c, backupArchiveErrorMessage(err))
		return
	}

	// Return HTML Fragment
	restoreToken := registerRestoreArtifact(databasePath)
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
	options := service.RestoreOptions{
		Configs:       c.PostForm("restore_configs") == "on",
		Metadata:      c.PostForm("restore_metadata") == "on",
		Subscriptions: c.PostForm("restore_subscriptions") == "on",
		Logs:          c.PostForm("restore_logs") == "on",
		Local:         c.PostForm("restore_local") == "on",
		Users:         c.PostForm("restore_users") == "on",
		RegenerateNFO: c.PostForm("restore_nfo") == "on",
	}
	if !options.HasRestoreCategory() {
		htmlBadRequest(c, "请至少选择一类要恢复的数据")
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
		GoBackground(func(ctx context.Context) {
			log.Println("Restore: Triggering NFO regeneration...")
			metaSvc := service.NewMetadataService()
			count, err := metaSvc.RegenerateAllNFOsContext(ctx)
			if err != nil {
				log.Printf("Restore: NFO regeneration failed: %v", err)
			} else {
				log.Printf("Restore: NFO regeneration completed. Processed %d series.", count)
			}
		})
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
	if c.Request.Method != http.MethodPost {
		jsonBadRequest(c, "加密备份需要输入密码，请使用新版备份页面导出")
		return
	}

	var request backupExportRequest
	if strings.Contains(strings.ToLower(c.GetHeader("Content-Type")), "application/json") {
		if err := c.ShouldBindJSON(&request); err != nil {
			jsonBadRequest(c, "备份导出参数格式不正确")
			return
		}
	} else {
		request.Mode = c.PostForm("mode")
		request.backupPasswordRequest = backupPasswordRequestFromForm(c)
	}

	password, err := resolveBackupArchivePassword(c, request.backupPasswordRequest)
	if err != nil {
		jsonBadRequest(c, err.Error())
		return
	}
	mode := service.NormalizeBackupMode(request.Mode)

	tempFile, err := os.CreateTemp("", "export_*.db")
	if err != nil {
		jsonServerError(c, "创建导出临时文件", err)
		return
	}
	databasePath := tempFile.Name()
	if err := tempFile.Close(); err != nil {
		safeio.Remove(databasePath)
		jsonServerError(c, "完成导出临时文件写入", err)
		return
	}
	defer safeio.Remove(databasePath)

	if err := service.CreateBackupFile(databasePath, mode); err != nil {
		jsonServerError(c, "创建备份文件", err)
		return
	}

	archiveFile, err := os.CreateTemp("", "export_*.zip")
	if err != nil {
		jsonServerError(c, "创建导出压缩包", err)
		return
	}
	archivePath := archiveFile.Name()
	if err := archiveFile.Close(); err != nil {
		safeio.Remove(archivePath)
		jsonServerError(c, "完成导出压缩包写入", err)
		return
	}
	defer safeio.Remove(archivePath)

	if err := service.CreateEncryptedBackupArchive(databasePath, archivePath, mode, password); err != nil {
		jsonServerError(c, "压缩并加密备份", err)
		return
	}

	c.Header("Content-Type", "application/zip")
	c.FileAttachment(archivePath, service.BackupFilename(mode, time.Now()))
}

func ImportBackupHandler(c *gin.Context) {
	htmlBadRequest(c, "已经禁用直接恢复，请先通过分析/预览流程确认备份内容。")
}

func backupUploadFile(c *gin.Context) (*multipart.FileHeader, error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBackupRequestBytes)
	file, err := c.FormFile("backup_file")
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return nil, errBackupFileTooLarge
		}
		return nil, err
	}
	if file.Size > maxBackupFileBytes {
		return nil, errBackupFileTooLarge
	}
	return file, nil
}

func saveUploadedBackup(file *multipart.FileHeader) (string, error) {
	if file == nil {
		return "", errors.New("备份文件不能为空")
	}
	if file.Size > maxBackupFileBytes {
		return "", errBackupFileTooLarge
	}
	tempFile, err := os.CreateTemp("", "restore_analyze_*")
	if err != nil {
		return "", err
	}
	sourcePath := tempFile.Name()
	cleanup := true
	defer func() {
		safeio.Close(tempFile)
		if cleanup {
			safeio.Remove(sourcePath)
		}
	}()

	src, err := file.Open()
	if err != nil {
		return "", err
	}
	defer safeio.Close(src)

	written, err := io.Copy(tempFile, io.LimitReader(src, maxBackupFileBytes+1))
	if err != nil {
		return "", err
	}
	if written > maxBackupFileBytes {
		return "", errBackupFileTooLarge
	}
	if err := tempFile.Close(); err != nil {
		return "", err
	}
	cleanup = false
	return sourcePath, nil
}
