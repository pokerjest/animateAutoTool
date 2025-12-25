package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	_ "github.com/pokerjest/animateAutoTool/internal/config"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/model"
	"gorm.io/gorm"
)

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

	db.SaveGlobalConfig(model.ConfigKeyR2Endpoint, req.Endpoint)
	db.SaveGlobalConfig(model.ConfigKeyR2AccessKey, req.AccessKey)
	db.SaveGlobalConfig(model.ConfigKeyR2Bucket, req.Bucket)

	// Only update secret key if provided (not empty or masked)
	if req.SecretKey != "" && !isMasked(req.SecretKey) {
		db.SaveGlobalConfig(model.ConfigKeyR2SecretKey, req.SecretKey)
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

func RestoreFromR2Handler(c *gin.Context) {
	key := c.PostForm("key")
	if key == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No backup identifier provided"})
		return
	}

	// Read restore options from form
	restoreConfigs := c.PostForm("restore_configs") == "true"
	restoreMetadata := c.PostForm("restore_metadata") == "true"
	restoreSubscriptions := c.PostForm("restore_subscriptions") == "true"
	restoreLogs := c.PostForm("restore_logs") == "true"
	restoreLocal := c.PostForm("restore_local") == "true"

	// Validate at least one option selected
	if !restoreConfigs && !restoreMetadata && !restoreSubscriptions && !restoreLogs && !restoreLocal {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Please select at least one table to restore"})
		return
	}

	client, bucket, err := getR2Client(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "R2 Config Error: " + err.Error()})
		return
	}

	// Download to temp
	tempFile, err := os.CreateTemp("", "restore_r2_*.db")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create temp file"})
		return
	}
	defer os.Remove(tempFile.Name())

	resp, err := client.GetObject(c.Request.Context(), &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		tempFile.Close()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to download backup: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	if _, err := io.Copy(tempFile, resp.Body); err != nil {
		tempFile.Close()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to write temp file"})
		return
	}
	tempFile.Close()

	// Verify DB file valid (simple check)
	if !isValidSQLite(tempFile.Name()) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Downloaded file is not a valid database"})
		return
	}

	// Open backup database
	backupDB, err := gorm.Open(sqlite.Open(tempFile.Name()), &gorm.Config{})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid backup file: " + err.Error()})
		return
	}
	defer func() {
		sqlDB, _ := backupDB.DB()
		sqlDB.Close()
	}()

	// Perform selective restore
	if restoreConfigs {
		db.DB.Exec("DELETE FROM global_configs")
		var configs []model.GlobalConfig
		if err := backupDB.Find(&configs).Error; err == nil && len(configs) > 0 {
			db.DB.Create(&configs)
		}
	}

	if restoreMetadata {
		db.DB.Exec("DELETE FROM anime_metadata")
		var metadata []model.AnimeMetadata
		if err := backupDB.Find(&metadata).Error; err == nil && len(metadata) > 0 {
			db.DB.Create(&metadata)
		}
	}

	if restoreSubscriptions {
		db.DB.Exec("DELETE FROM subscriptions")
		var subs []model.Subscription
		if err := backupDB.Find(&subs).Error; err == nil && len(subs) > 0 {
			db.DB.Create(&subs)
		}
	}

	if restoreLogs {
		db.DB.Exec("DELETE FROM download_logs")
		var logs []model.DownloadLog
		if err := backupDB.Find(&logs).Error; err == nil && len(logs) > 0 {
			db.DB.Create(&logs)
		}
	}

	if restoreLocal {
		db.DB.Exec("DELETE FROM local_anime_directories")
		db.DB.Exec("DELETE FROM local_animes")
		var dirs []model.LocalAnimeDirectory
		if err := backupDB.Find(&dirs).Error; err == nil && len(dirs) > 0 {
			db.DB.Create(&dirs)
		}
		var animes []model.LocalAnime
		if err := backupDB.Find(&animes).Error; err == nil && len(animes) > 0 {
			db.DB.Create(&animes)
		}
	}

	c.JSON(http.StatusOK, gin.H{"status": "restored"})
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
	f, err := os.OpenFile("server_debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Println("Error opening debug log:", err)
		return
	}
	defer f.Close()
	msg := fmt.Sprintf(format, v...)
	if len(msg) == 0 || msg[len(msg)-1] != '\n' {
		msg += "\n"
	}
	f.WriteString(time.Now().Format("2006/01/02 15:04:05") + " " + msg)
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

	// Try ListObject (limit 1) to verify connectivity and permission
	_, err = client.ListObjectsV2(c.Request.Context(), &s3.ListObjectsV2Input{
		Bucket:  aws.String(req.Bucket),
		MaxKeys: aws.Int32(1), // Minimize data transfer
	})

	if err != nil {
		debugLog("DEBUG: ListObjectsV2 error: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Connection failed: " + err.Error()})
		return
	}

	debugLog("DEBUG: Connection successful")
	c.JSON(http.StatusOK, gin.H{"status": "ok", "message": "Connection successful"})
}

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
