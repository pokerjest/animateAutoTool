package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/safeio"
	appversion "github.com/pokerjest/animateAutoTool/internal/version"
	securezip "github.com/yeka/zip"
)

const (
	BackupArchiveFormatVersion = 1
	backupArchiveDatabaseName  = "database.db"
	backupArchiveManifestName  = "manifest.json"
	backupArchiveMaxEntrySize  = int64(8 << 30)
)

var (
	ErrBackupArchivePassword = errors.New("备份压缩包密码不正确")
	ErrBackupArchiveFormat   = errors.New("备份压缩包格式不受支持")
)

// BackupArchiveManifest is encrypted together with the database. Keeping the
// manifest inside the archive lets restore verify that the extracted database
// is complete before it is handed to the existing SQLite restore service.
type BackupArchiveManifest struct {
	FormatVersion  int       `json:"format_version"`
	AppVersion     string    `json:"app_version"`
	SchemaVersion  string    `json:"schema_version"`
	BackupMode     string    `json:"backup_mode"`
	DatabaseSHA256 string    `json:"database_sha256"`
	DatabaseSize   int64     `json:"database_size"`
	CreatedAt      time.Time `json:"created_at"`
	Encryption     string    `json:"encryption"`
}

// BackupArchiveFile reports whether a path is an encrypted backup archive.
// The ZIP central directory is not encrypted by the ZIP format, but all
// entries written by CreateEncryptedBackupArchive are encrypted.
func IsBackupArchive(path string) bool {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return false
	}
	defer safeio.Close(file)

	header := make([]byte, 4)
	if _, err := io.ReadFull(file, header); err != nil {
		return false
	}
	return string(header) == "PK\x03\x04" || string(header) == "PK\x05\x06" || string(header) == "PK\x07\x08"
}

// CreateEncryptedBackupArchive wraps an annotated SQLite backup in an
// AES-256 encrypted ZIP archive. The source database is read in a stream, so
// memory usage stays bounded while compression happens before R2 upload.
func CreateEncryptedBackupArchive(databasePath, archivePath, mode, password string) error {
	if strings.TrimSpace(password) == "" {
		return errors.New("备份压缩包密码不能为空")
	}

	info, err := os.Stat(filepath.Clean(databasePath))
	if err != nil {
		return fmt.Errorf("读取备份数据库: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("备份数据库不是普通文件")
	}

	database, err := os.Open(filepath.Clean(databasePath))
	if err != nil {
		return fmt.Errorf("打开备份数据库: %w", err)
	}
	defer safeio.Close(database)

	archiveFile, err := os.Create(filepath.Clean(archivePath))
	if err != nil {
		return fmt.Errorf("创建加密备份压缩包: %w", err)
	}
	archivePath = archiveFile.Name()
	cleanupArchive := true
	defer func() {
		_ = archiveFile.Close()
		if cleanupArchive {
			_ = os.Remove(archivePath)
		}
	}()

	zipWriter := securezip.NewWriter(archiveFile)
	databaseWriter, err := zipWriter.Encrypt(backupArchiveDatabaseName, password, securezip.AES256Encryption)
	if err != nil {
		_ = zipWriter.Close()
		return fmt.Errorf("创建加密数据库条目: %w", err)
	}
	if _, err := io.Copy(databaseWriter, database); err != nil {
		_ = zipWriter.Close()
		return fmt.Errorf("压缩数据库备份: %w", err)
	}

	manifest := BackupArchiveManifest{
		FormatVersion:  BackupArchiveFormatVersion,
		AppVersion:     currentBackupAppVersion(),
		SchemaVersion:  currentBackupSchemaVersion(),
		BackupMode:     NormalizeBackupMode(mode),
		DatabaseSHA256: sha256File(databasePath),
		DatabaseSize:   info.Size(),
		CreatedAt:      time.Now().UTC(),
		Encryption:     "AES-256",
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		_ = zipWriter.Close()
		return fmt.Errorf("生成备份 manifest: %w", err)
	}
	manifestWriter, err := zipWriter.Encrypt(backupArchiveManifestName, password, securezip.AES256Encryption)
	if err != nil {
		_ = zipWriter.Close()
		return fmt.Errorf("创建加密 manifest 条目: %w", err)
	}
	if _, err := manifestWriter.Write(manifestBytes); err != nil {
		_ = zipWriter.Close()
		return fmt.Errorf("写入备份 manifest: %w", err)
	}
	if err := zipWriter.Close(); err != nil {
		return fmt.Errorf("完成加密备份压缩包: %w", err)
	}
	if err := archiveFile.Close(); err != nil {
		return fmt.Errorf("关闭加密备份压缩包: %w", err)
	}
	cleanupArchive = false
	return nil
}

// ExtractEncryptedBackupArchive decrypts and extracts the database entry to a
// caller-owned path. It verifies the encrypted manifest and database digest
// before returning, so a wrong password or damaged archive cannot reach the
// restore transaction.
func ExtractEncryptedBackupArchive(archivePath, password, databasePath string) (BackupArchiveManifest, error) {
	if strings.TrimSpace(password) == "" {
		return BackupArchiveManifest{}, errors.New("请输入备份压缩包密码")
	}

	reader, err := securezip.OpenReader(filepath.Clean(archivePath))
	if err != nil {
		return BackupArchiveManifest{}, fmt.Errorf("%w: %v", ErrBackupArchiveFormat, err)
	}
	defer reader.Close()

	var databaseEntry, manifestEntry *securezip.File
	for _, file := range reader.File {
		switch filepath.ToSlash(file.Name) {
		case backupArchiveDatabaseName:
			databaseEntry = file
		case backupArchiveManifestName:
			manifestEntry = file
		}
	}
	if databaseEntry == nil || manifestEntry == nil || !databaseEntry.IsEncrypted() || !manifestEntry.IsEncrypted() {
		return BackupArchiveManifest{}, ErrBackupArchiveFormat
	}
	if databaseEntry.UncompressedSize64 > uint64(backupArchiveMaxEntrySize) {
		return BackupArchiveManifest{}, errors.New("备份数据库解压后超过安全大小限制")
	}

	manifest, err := readEncryptedBackupManifest(manifestEntry, password)
	if err != nil {
		return BackupArchiveManifest{}, err
	}
	if manifest.FormatVersion != BackupArchiveFormatVersion {
		return BackupArchiveManifest{}, fmt.Errorf("备份压缩包格式版本 %d 不受当前版本支持", manifest.FormatVersion)
	}
	if manifest.Encryption != "AES-256" {
		return BackupArchiveManifest{}, fmt.Errorf("%w: 不支持的加密方式 %q", ErrBackupArchiveFormat, manifest.Encryption)
	}
	if manifest.DatabaseSize < 0 || manifest.DatabaseSize > backupArchiveMaxEntrySize {
		return BackupArchiveManifest{}, errors.New("备份 manifest 中的数据库大小无效")
	}
	if manifest.DatabaseSize != 0 && uint64(manifest.DatabaseSize) != databaseEntry.UncompressedSize64 {
		return BackupArchiveManifest{}, errors.New("备份压缩包校验失败，数据库大小与 manifest 不一致")
	}

	output, err := os.OpenFile(filepath.Clean(databasePath), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return BackupArchiveManifest{}, fmt.Errorf("创建解包数据库: %w", err)
	}
	cleanupOutput := true
	defer func() {
		_ = output.Close()
		if cleanupOutput {
			_ = os.Remove(databasePath)
		}
	}()

	databaseEntry.SetPassword(password)
	entryReader, err := databaseEntry.Open()
	if err != nil {
		if errors.Is(err, securezip.ErrPassword) {
			return BackupArchiveManifest{}, ErrBackupArchivePassword
		}
		return BackupArchiveManifest{}, fmt.Errorf("打开加密数据库条目: %w", err)
	}
	_, copyErr := io.Copy(output, io.LimitReader(entryReader, backupArchiveMaxEntrySize+1))
	safeio.Close(entryReader)
	if copyErr != nil {
		if errors.Is(copyErr, securezip.ErrPassword) {
			return BackupArchiveManifest{}, ErrBackupArchivePassword
		}
		return BackupArchiveManifest{}, fmt.Errorf("解包数据库备份: %w", copyErr)
	}
	if err := output.Close(); err != nil {
		return BackupArchiveManifest{}, fmt.Errorf("关闭解包数据库: %w", err)
	}
	if info, err := os.Stat(filepath.Clean(databasePath)); err != nil {
		return BackupArchiveManifest{}, fmt.Errorf("读取解包数据库: %w", err)
	} else if info.Size() > backupArchiveMaxEntrySize {
		return BackupArchiveManifest{}, errors.New("备份数据库解压后超过安全大小限制")
	} else if manifest.DatabaseSize != 0 && info.Size() != manifest.DatabaseSize {
		return BackupArchiveManifest{}, errors.New("备份压缩包校验失败，解包数据库大小不一致")
	}
	if manifest.DatabaseSHA256 != "" && !strings.EqualFold(manifest.DatabaseSHA256, sha256File(databasePath)) {
		return BackupArchiveManifest{}, errors.New("备份压缩包校验失败，数据库内容可能已损坏")
	}
	if !isSQLiteFile(databasePath) {
		return BackupArchiveManifest{}, errors.New("备份压缩包中的数据库无效")
	}
	cleanupOutput = false
	return manifest, nil
}

func readEncryptedBackupManifest(file *securezip.File, password string) (BackupArchiveManifest, error) {
	file.SetPassword(password)
	reader, err := file.Open()
	if err != nil {
		if errors.Is(err, securezip.ErrPassword) {
			return BackupArchiveManifest{}, ErrBackupArchivePassword
		}
		return BackupArchiveManifest{}, fmt.Errorf("打开加密 manifest: %w", err)
	}
	defer safeio.Close(reader)

	var manifest BackupArchiveManifest
	if err := json.NewDecoder(io.LimitReader(reader, 1<<20)).Decode(&manifest); err != nil {
		if errors.Is(err, securezip.ErrPassword) {
			return BackupArchiveManifest{}, ErrBackupArchivePassword
		}
		return BackupArchiveManifest{}, fmt.Errorf("读取备份 manifest: %w", err)
	}
	return manifest, nil
}

func isSQLiteFile(path string) bool {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return false
	}
	defer safeio.Close(file)
	header := make([]byte, 16)
	if _, err := io.ReadFull(file, header); err != nil {
		return false
	}
	return string(header) == "SQLite format 3\x00"
}

func currentBackupAppVersion() string {
	return appversion.AppVersion
}

func currentBackupSchemaVersion() string {
	if db.DB == nil {
		return ""
	}
	return db.CurrentSchemaVersion(db.DB)
}
