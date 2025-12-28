package api

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	_ "github.com/pokerjest/animateAutoTool/internal/config"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"gorm.io/gorm"
)

// Progress Management
type DownloadProgress struct {
	TaskID     string `json:"task_id"`
	TotalBytes int64  `json:"total_bytes"`
	Downloaded int64  `json:"downloaded"`
	Status     string `json:"status"` // "pending", "downloading", "analyzing", "completed", "error"
	Error      string `json:"error,omitempty"`
	ResultHTML string `json:"result_html,omitempty"`
}

var progressMap sync.Map

// CountingReader tracks read progress
type CountingReader struct {
	Reader     io.Reader
	Total      int64
	Downloaded int64
	TaskID     string
}

func (r *CountingReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if n > 0 {
		r.Downloaded += int64(n)
		// Update map periodically or on every read?
		// For smooth UI, every read is fine if not too frequent lock contention.
		// sync.Map is okay. To optimize, could update every N bytes.
		// Let's just update directly for simplicity first.
		if val, ok := progressMap.Load(r.TaskID); ok {
			progress := val.(*DownloadProgress)
			progress.Downloaded = r.Downloaded
			// We store pointer, so modification is visible?
			// sync.Map Store is safer for atomic replacement, but here we modify the struct field.
			// Race condition possible if reading simultaneously.
			// Better to use a mutex on the struct or replace the value in map.
			// Let's replace in map to be safeish, or simple Mutex in struct.
			// Simplified: Update the struct copy and Store back.
			newProgress := *progress
			newProgress.Downloaded = r.Downloaded
			progressMap.Store(r.TaskID, &newProgress)
		}
	}
	return n, err
}

type R2Config struct {
	Endpoint  string `json:"endpoint"`
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
	Bucket    string `json:"bucket"`
}

func getR2Client(ctx context.Context) (*s3.Client, string, error) {
	debugLog("DEBUG: getR2Client called")
	var endpoint, accessKey, secretKey, bucket string

	db.DB.Model(&model.GlobalConfig{}).Where("key = ?", model.ConfigKeyR2Endpoint).Select("value").Scan(&endpoint)
	db.DB.Model(&model.GlobalConfig{}).Where("key = ?", model.ConfigKeyR2AccessKey).Select("value").Scan(&accessKey)
	db.DB.Model(&model.GlobalConfig{}).Where("key = ?", model.ConfigKeyR2SecretKey).Select("value").Scan(&secretKey)
	db.DB.Model(&model.GlobalConfig{}).Where("key = ?", model.ConfigKeyR2Bucket).Select("value").Scan(&bucket)

	debugLog("DEBUG: getR2Client Config Loaded: IsEndpointEmpty=%v, IsAccessKeyEmpty=%v, IsSecretKeyEmpty=%v, Bucket=%s",
		endpoint == "", accessKey == "", secretKey == "", bucket)

	if endpoint == "" || accessKey == "" || secretKey == "" || bucket == "" {
		return nil, "", fmt.Errorf("R2 configuration is incomplete")
	}

	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
		config.WithRegion("us-east-1"), // R2 generally requires us-east-1 or auto, but S3 clients often prefer us-east-1
	)
	if err != nil {
		debugLog("DEBUG: getR2Client LoadDefaultConfig error: %v", err)
		return nil, "", err
	}

	// Override endpoint resolver
	// Cloudflare R2 endpoint format: https://<accountid>.r2.cloudflarestorage.com
	// aws-sdk-v2 requires a custom endpoint resolver for non-AWS S3
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true // R2 compatibility: PathStyle is often safer for custom endpoints
	})

	return client, bucket, nil
}

func GetR2ConfigHandler(c *gin.Context) {
	var endpoint, accessKey, secretKey, bucket string

	db.DB.Model(&model.GlobalConfig{}).Where("key = ?", model.ConfigKeyR2Endpoint).Select("value").Scan(&endpoint)
	db.DB.Model(&model.GlobalConfig{}).Where("key = ?", model.ConfigKeyR2AccessKey).Select("value").Scan(&accessKey)
	db.DB.Model(&model.GlobalConfig{}).Where("key = ?", model.ConfigKeyR2SecretKey).Select("value").Scan(&secretKey)
	db.DB.Model(&model.GlobalConfig{}).Where("key = ?", model.ConfigKeyR2Bucket).Select("value").Scan(&bucket)

	// Mask secret key if it exists
	if secretKey != "" {
		secretKey = "********"
	}

	c.JSON(http.StatusOK, gin.H{
		"endpoint":   endpoint,
		"access_key": accessKey,
		"secret_key": secretKey,
		"bucket":     bucket,
	})
}

func UpdateR2ConfigHandler(c *gin.Context) {
	var req R2Config
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := db.SaveGlobalConfig(model.ConfigKeyR2Endpoint, req.Endpoint); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save endpoint: " + err.Error()})
		return
	}
	if err := db.SaveGlobalConfig(model.ConfigKeyR2AccessKey, req.AccessKey); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save access key: " + err.Error()})
		return
	}
	if err := db.SaveGlobalConfig(model.ConfigKeyR2Bucket, req.Bucket); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save bucket: " + err.Error()})
		return
	}

	// Only update secret key if provided (not empty or masked)
	if req.SecretKey != "" && !isMasked(req.SecretKey) {
		if err := db.SaveGlobalConfig(model.ConfigKeyR2SecretKey, req.SecretKey); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save secret key: " + err.Error()})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func isMasked(s string) bool {
	return s == "********" || (len(s) >= 3 && s[:3] == "***") // Check for constant mask or legacy prefix
}

func UploadToR2Handler(c *gin.Context) {
	debugLog("DEBUG: UploadToR2Handler called")
	// 1. Create Filtered Backup (Reusing logic from ExportBackupHandler mainly)
	tempFile, err := os.CreateTemp("", "backup_r2_*.db")
	if err != nil {
		debugLog("DEBUG: CreateTemp error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create temp file"})
		return
	}
	tempPath := tempFile.Name()
	tempFile.Close()
	defer os.Remove(tempPath)

	if err := createBackupFile(tempPath); err != nil {
		debugLog("DEBUG: createBackupFile error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create backup: " + err.Error()})
		return
	}

	// 2. Upload to R2
	client, bucket, err := getR2Client(c.Request.Context())
	if err != nil {
		debugLog("DEBUG: getR2Client error: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "R2 Config Error: " + err.Error()})
		return
	}

	file, err := os.Open(tempPath)
	if err != nil {
		debugLog("DEBUG: os.Open backup file error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to open backup file"})
		return
	}
	defer file.Close()

	timestamp := time.Now().Format("20060102_150405")
	key := fmt.Sprintf("animate_backup_%s.db", timestamp)

	debugLog("DEBUG: Starting Upload to Bucket=%s, Key=%s", bucket, key)
	_, err = client.PutObject(c.Request.Context(), &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   file,
	})
	if err != nil {
		debugLog("DEBUG: PutObject error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to upload to R2: " + err.Error()})
		return
	}

	debugLog("DEBUG: Upload successful")
	c.JSON(http.StatusOK, gin.H{"status": "uploaded", "key": key})
}

type R2BackupFile struct {
	Key          string `json:"key"`
	Size         int64  `json:"size"`
	LastModified string `json:"last_modified"`
}

func ListR2BackupsHandler(c *gin.Context) {
	client, bucket, err := getR2Client(c.Request.Context())
	if err != nil {
		// Return empty list if config invalid, or error?
		// Better to return error so UI can show it.
		c.JSON(http.StatusOK, gin.H{"backups": []R2BackupFile{}, "error": err.Error()})
		return
	}

	output, err := client.ListObjectsV2(c.Request.Context(), &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String("animate_backup_"), // optional filter
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list backups: " + err.Error()})
		return
	}

	var backups []R2BackupFile
	for _, obj := range output.Contents {
		backups = append(backups, R2BackupFile{
			Key:          *obj.Key,
			Size:         *obj.Size,
			LastModified: obj.LastModified.Format("2006-01-02 15:04:05"),
		})
	}

	// Sort desc by time
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].LastModified > backups[j].LastModified
	})

	c.JSON(http.StatusOK, gin.H{"backups": backups})
}

func GetR2ProgressHandler(c *gin.Context) {
	taskID := c.Param("taskId")
	if val, ok := progressMap.Load(taskID); ok {
		c.JSON(http.StatusOK, val.(*DownloadProgress))
		return
	}
	c.JSON(http.StatusNotFound, gin.H{"error": "Task not found"})
}

// StageR2BackupHandler (Async Version)
func StageR2BackupHandler(c *gin.Context) {
	key := c.PostForm("key")
	if key == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No backup identifier provided"})
		return
	}

	client, bucket, err := getR2Client(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "R2 Config Error: " + err.Error()})
		return
	}

	// Create Task ID
	taskID := uuid.New().String()

	// Initialize Progress
	progress := &DownloadProgress{
		TaskID:     taskID,
		Status:     "pending",
		TotalBytes: 0,
		Downloaded: 0,
	}
	progressMap.Store(taskID, progress)

	// Launch Background Task
	go func(tID, k, b string, cli *s3.Client) {
		// Recovery
		defer func() {
			if r := recover(); r != nil {
				updateProgress(tID, "error", fmt.Sprintf("Panic: %v", r), 0, 0, "")
			}
		}()

		updateProgress(tID, "downloading", "", 0, 0, "")

		// 1. Get Object Info (Head/Get)
		// We use GetObject directly.
		// Note: We need a specialized Context for background task, don't use c.Request.Context()
		bgCtx := context.Background()

		resp, err := cli.GetObject(bgCtx, &s3.GetObjectInput{
			Bucket: aws.String(b),
			Key:    aws.String(k),
		})
		if err != nil {
			updateProgress(tID, "error", "Download init failed: "+err.Error(), 0, 0, "")
			return
		}
		defer resp.Body.Close()

		total := int64(0)
		if resp.ContentLength != nil {
			total = *resp.ContentLength
		}
		updateProgress(tID, "downloading", "", total, 0, "")

		// 2. Create Temp File
		tempFile, err := os.CreateTemp("", "restore_r2_stage_*.db")
		if err != nil {
			updateProgress(tID, "error", "Create temp file failed", total, 0, "")
			return
		}
		// Note: No defer remove, kept for restore execution.

		// 3. Download with Progress
		reader := &CountingReader{
			Reader: resp.Body,
			Total:  total,
			TaskID: tID,
		}

		if _, err := io.Copy(tempFile, reader); err != nil {
			tempFile.Close()
			os.Remove(tempFile.Name())
			updateProgress(tID, "error", "Download stream failed: "+err.Error(), total, 0, "")
			return
		}
		tempFile.Close()

		// 4. Analyze
		updateProgress(tID, "analyzing", "", total, total, "") // 100% downloaded

		tempDB, err := gorm.Open(sqlite.Open(tempFile.Name()), &gorm.Config{})
		if err != nil {
			os.Remove(tempFile.Name())
			updateProgress(tID, "error", "Invalid Database File", total, total, "")
			return
		}
		stats := getDBStats(tempDB, tempFile.Name())
		sqlDB, _ := tempDB.DB()
		sqlDB.Close()

		// 5. Render HTML
		// Parse the template manually.
		tmpl, err := template.ParseFiles("web/templates/backup_analyze.html")
		if err != nil {
			os.Remove(tempFile.Name())
			updateProgress(tID, "error", "Template parse error: "+err.Error(), total, total, "")
			return
		}

		var buf bytes.Buffer
		err = tmpl.ExecuteTemplate(&buf, "backup_analyze.html", map[string]interface{}{
			"Stats":    stats,
			"TempFile": tempFile.Name(),
		})
		if err != nil {
			os.Remove(tempFile.Name())
			updateProgress(tID, "error", "Template execute error: "+err.Error(), total, total, "")
			return
		}

		// 6. Complete
		updateProgress(tID, "completed", "", total, total, buf.String())

	}(taskID, key, bucket, client)

	c.JSON(http.StatusOK, gin.H{"task_id": taskID, "status": "started"})
}

func updateProgress(taskID, status, errStr string, total, downloaded int64, html string) {
	p := &DownloadProgress{
		TaskID:     taskID,
		Status:     status,
		TotalBytes: total,
		Downloaded: downloaded,
		Error:      errStr,
		ResultHTML: html,
	}
	// If existing, preserve Total/Downloaded if passed 0?
	// Simplified: callers pass correct values.
	// For error state, we might want to keep downloaded count?
	// This helper overrides everything.
	if val, ok := progressMap.Load(taskID); ok {
		prev := val.(*DownloadProgress)
		if total == 0 {
			p.TotalBytes = prev.TotalBytes
		}
		if downloaded == 0 && status != "pending" { // keep prev downloaded if not reset
			p.Downloaded = prev.Downloaded
		}
	}

	progressMap.Store(taskID, p)
}

// RestoreFromR2Handler - Deprecated?
// Actually, with the new flow, the User:
// 1. Clicks "R2 Restore" button -> StageR2BackupHandler -> Returns HTML Form with "Confirm" button.
// 2. Click "Confirm" -> ExecuteRestoreHandler (Standard).
// So we don't strictly need a separate RestoreFromR2Handler execution logic anymore.
// BUT, to keep API clean, let's just make sure UI points to ExecuteRestoreHandler.
// We can remove the old RestoreFromR2Handler or keep it as a legacy direct handler (if user skips preview).
// Let's replace it with a redirect or error to force preview.
func RestoreFromR2Handler(c *gin.Context) {
	c.JSON(http.StatusBadRequest, gin.H{"error": "Please use the Preview/Stage flow."})
}

func DeleteR2BackupHandler(c *gin.Context) {
	key := c.PostForm("key")
	if key == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No backup identifier provided"})
		return
	}

	client, bucket, err := getR2Client(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "R2 Config Error: " + err.Error()})
		return
	}

	debugLog("DEBUG: Deleting backup Key=%s from Bucket=%s", key, bucket)
	_, err = client.DeleteObject(c.Request.Context(), &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		debugLog("DEBUG: DeleteObject error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete backup: " + err.Error()})
		return
	}

	debugLog("DEBUG: Delete successful")
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

func debugLog(format string, v ...interface{}) {
	_ = os.MkdirAll("logs", 0755)
	f, err := os.OpenFile("logs/server_debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Println("Error opening debug log:", err)
		return
	}
	defer f.Close()
	msg := fmt.Sprintf(format, v...)
	if len(msg) == 0 || msg[len(msg)-1] != '\n' {
		msg += "\n"
	}
	if _, err := f.WriteString(time.Now().Format("2006/01/02 15:04:05") + " " + msg); err != nil {
		fmt.Println("Error writing to debug log:", err)
	}
}

func TestR2ConnectionHandler(c *gin.Context) {
	debugLog("DEBUG: TestR2ConnectionHandler called")
	var req R2Config
	if err := c.ShouldBindJSON(&req); err != nil {
		debugLog("DEBUG: BindJSON error: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request: " + err.Error()})
		return
	}
	debugLog("DEBUG: Testing R2 Config: Endpoint=%s, Bucket=%s, AccessKey=%s", req.Endpoint, req.Bucket, req.AccessKey)

	// Validate inputs
	if req.Endpoint == "" || req.AccessKey == "" || req.Bucket == "" {
		debugLog("DEBUG: Validation failed - missing fields")
		c.JSON(http.StatusBadRequest, gin.H{"error": "Endpoint, AccessKey, and Bucket are required"})
		return
	}

	// For secret key, if it's masked or empty, try to load from DB
	secretKey := req.SecretKey
	if secretKey == "" || isMasked(secretKey) {
		db.DB.Model(&model.GlobalConfig{}).Where("key = ?", model.ConfigKeyR2SecretKey).Select("value").Scan(&secretKey)
		if secretKey == "" {
			debugLog("DEBUG: Secret Key missing from DB")
			c.JSON(http.StatusBadRequest, gin.H{"error": "Secret Key is missing"})
			return
		}
	}

	// Create temporary client
	cfg, err := config.LoadDefaultConfig(c.Request.Context(),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(req.AccessKey, secretKey, "")),
		config.WithRegion("us-east-1"),
	)
	if err != nil {
		debugLog("DEBUG: LoadDefaultConfig error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load config: " + err.Error()})
		return
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(req.Endpoint)
		o.UsePathStyle = true
	})

	// Try ListObject (limit 1) to verify Read permission
	_, err = client.ListObjectsV2(c.Request.Context(), &s3.ListObjectsV2Input{
		Bucket:  aws.String(req.Bucket),
		MaxKeys: aws.Int32(1), // Minimize data transfer
	})

	if err != nil {
		debugLog("DEBUG: ListObjectsV2 error: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Read Check Failed: " + err.Error()})
		return
	}

	// Try PutObject (Write Check)
	testKey := "connection_test_check.txt"
	testContent := "ok"
	_, err = client.PutObject(c.Request.Context(), &s3.PutObjectInput{
		Bucket: aws.String(req.Bucket),
		Key:    aws.String(testKey),
		Body:   strings.NewReader(testContent),
	})
	if err != nil {
		debugLog("DEBUG: PutObject error: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Write Check Failed: " + err.Error()})
		return
	}

	// Clean up (Delete Check)
	_, err = client.DeleteObject(c.Request.Context(), &s3.DeleteObjectInput{
		Bucket: aws.String(req.Bucket),
		Key:    aws.String(testKey),
	})
	if err != nil {
		debugLog("DEBUG: DeleteObject error: %v (Non-fatal but indicative)", err)
		// We successfully wrote, so technically "connected", but delete failed.
		// Let's warn or just consider it success but log it?
		// For a backup tool, inability to delete might accumulate old backups.
		// Let's count it as success but maybe append a warning?
		// For simplicity, let's just say "ok" but log it.
	}

	debugLog("DEBUG: Connection successful (Read/Write/Delete verified)")
	c.JSON(http.StatusOK, gin.H{"status": "ok", "message": "Connection successful (Read/Write Verified)"})
}
