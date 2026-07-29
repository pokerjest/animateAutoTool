package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/config"
	"github.com/pokerjest/animateAutoTool/internal/db"
	appversion "github.com/pokerjest/animateAutoTool/internal/version"
)

const (
	safetySnapshotFormatVersion = 1
	safetySnapshotKeepCount     = 5
	safetySnapshotKeepAge       = 30 * 24 * time.Hour
)

// SafetySnapshot describes a point-in-time copy of the runtime database and
// config mirror. It is intentionally independent from user-exported backups:
// snapshots are local recovery artifacts and are never uploaded automatically.
type SafetySnapshot struct {
	ID                string    `json:"id"`
	Reason            string    `json:"reason"`
	OperationType     string    `json:"operation_type,omitempty"`
	AppVersion        string    `json:"app_version"`
	SchemaVersion     string    `json:"schema_version"`
	DatabasePath      string    `json:"database_path"`
	ConfigPath        string    `json:"config_path,omitempty"`
	DatabaseSHA256    string    `json:"database_sha256"`
	ConfigSHA256      string    `json:"config_sha256,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	FormatVersion     int       `json:"format_version"`
	RollbackSupported bool      `json:"rollback_supported"`
}

func snapshotRoot() string {
	return config.DataPath("updates", "snapshots")
}

func CreateSafetySnapshot(reason string) (SafetySnapshot, error) {
	if db.DB == nil || strings.TrimSpace(db.CurrentDBPath) == "" || db.CurrentDBPath == ":memory:" {
		if db.CurrentDBPath == ":memory:" {
			return SafetySnapshot{}, nil
		}
		return SafetySnapshot{}, errors.New("runtime database is not available for snapshot")
	}

	now := time.Now().UTC()
	id := fmt.Sprintf("%s-%d", now.Format("20060102T150405.000000000Z"), now.UnixNano())
	dir := filepath.Join(snapshotRoot(), id)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return SafetySnapshot{}, fmt.Errorf("create snapshot directory: %w", err)
	}

	databasePath := filepath.Join(dir, "database.db")
	if err := db.DB.Exec("VACUUM INTO ?", databasePath).Error; err != nil {
		_ = os.RemoveAll(dir)
		return SafetySnapshot{}, fmt.Errorf("snapshot database: %w", err)
	}

	snapshot := SafetySnapshot{
		ID:                id,
		Reason:            strings.TrimSpace(reason),
		OperationType:     strings.TrimSpace(reason),
		AppVersion:        appversion.AppVersion,
		SchemaVersion:     db.CurrentSchemaVersion(db.DB),
		DatabasePath:      databasePath,
		CreatedAt:         now,
		FormatVersion:     safetySnapshotFormatVersion,
		RollbackSupported: strings.TrimSpace(reason) != "",
	}

	configPath := config.ConfigFilePath()
	if info, err := os.Stat(configPath); err == nil && !info.IsDir() {
		snapshot.ConfigPath = filepath.Join(dir, "config.yaml")
		if err := copyFile(configPath, snapshot.ConfigPath, 0600); err != nil {
			_ = os.RemoveAll(dir)
			return SafetySnapshot{}, fmt.Errorf("snapshot config: %w", err)
		}
		if snapshot.ConfigSHA256, err = fileSHA256(snapshot.ConfigPath); err != nil {
			_ = os.RemoveAll(dir)
			return SafetySnapshot{}, fmt.Errorf("hash snapshot config: %w", err)
		}
	}
	databaseSHA, err := fileSHA256(databasePath)
	if err != nil {
		_ = os.RemoveAll(dir)
		return SafetySnapshot{}, fmt.Errorf("hash snapshot database: %w", err)
	}
	snapshot.DatabaseSHA256 = databaseSHA

	manifestPath := filepath.Join(dir, "manifest.json")
	payload, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		_ = os.RemoveAll(dir)
		return SafetySnapshot{}, fmt.Errorf("encode snapshot manifest: %w", err)
	}
	if err := writeAtomic(manifestPath, append(payload, '\n'), 0600); err != nil {
		_ = os.RemoveAll(dir)
		return SafetySnapshot{}, fmt.Errorf("write snapshot manifest: %w", err)
	}
	CleanupSafetySnapshots()
	return snapshot, nil
}

func LoadSafetySnapshot(id string) (SafetySnapshot, error) {
	id = filepath.Base(strings.TrimSpace(id))
	if id == "" || id == "." || id == string(filepath.Separator) {
		return SafetySnapshot{}, errors.New("snapshot id is required")
	}
	path := filepath.Join(snapshotRoot(), id, "manifest.json")
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return SafetySnapshot{}, fmt.Errorf("read snapshot manifest: %w", err)
	}
	var snapshot SafetySnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return SafetySnapshot{}, fmt.Errorf("decode snapshot manifest: %w", err)
	}
	if snapshot.ID != id || snapshot.FormatVersion != safetySnapshotFormatVersion {
		return SafetySnapshot{}, errors.New("snapshot manifest is incompatible")
	}
	snapshot.DatabasePath = filepath.Join(snapshotRoot(), id, "database.db")
	if snapshot.ConfigPath != "" {
		snapshot.ConfigPath = filepath.Join(snapshotRoot(), id, "config.yaml")
	}
	if !fileMatchesSHA256(snapshot.DatabasePath, snapshot.DatabaseSHA256) {
		return SafetySnapshot{}, errors.New("snapshot database integrity check failed")
	}
	if snapshot.ConfigPath != "" && !fileMatchesSHA256(snapshot.ConfigPath, snapshot.ConfigSHA256) {
		return SafetySnapshot{}, errors.New("snapshot config integrity check failed")
	}
	return snapshot, nil
}

func ListSafetySnapshots() []SafetySnapshot {
	entries, err := os.ReadDir(snapshotRoot())
	if err != nil {
		return []SafetySnapshot{}
	}
	snapshots := make([]SafetySnapshot, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if snapshot, err := LoadSafetySnapshot(entry.Name()); err == nil {
			snapshots = append(snapshots, snapshot)
		}
	}
	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].CreatedAt.After(snapshots[j].CreatedAt)
	})
	return snapshots
}

func RestoreSafetySnapshot(id string) error {
	snapshot, err := LoadSafetySnapshot(id)
	if err != nil {
		return err
	}
	if db.CurrentDBPath == ":memory:" {
		return nil
	}
	if db.DB != nil {
		if sqlDB, dbErr := db.DB.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
		db.DB = nil
	}
	// A SQLite WAL/SHM pair belongs to the database file that was just closed.
	// Leaving it beside the restored snapshot can replay writes from the newer
	// runtime into the restored database on the next open.
	_ = os.Remove(db.CurrentDBPath + "-wal")
	_ = os.Remove(db.CurrentDBPath + "-shm")
	if err := copyFile(snapshot.DatabasePath, db.CurrentDBPath, 0600); err != nil {
		return fmt.Errorf("restore snapshot database: %w", err)
	}
	if snapshot.ConfigPath != "" {
		if err := copyFile(snapshot.ConfigPath, config.ConfigFilePath(), 0600); err != nil {
			return fmt.Errorf("restore snapshot config: %w", err)
		}
	}
	db.InitDB(db.CurrentDBPath)
	return nil
}

func CleanupSafetySnapshots() {
	snapshots := ListSafetySnapshots()
	now := time.Now().UTC()
	for index, snapshot := range snapshots {
		if index < safetySnapshotKeepCount && now.Sub(snapshot.CreatedAt) <= safetySnapshotKeepAge {
			continue
		}
		_ = os.RemoveAll(filepath.Join(snapshotRoot(), snapshot.ID))
	}
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	buf := make([]byte, 128*1024)
	for {
		n, readErr := file.Read(buf)
		if n > 0 {
			_, _ = hash.Write(buf[:n])
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return "", readErr
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func fileMatchesSHA256(path, expected string) bool {
	if strings.TrimSpace(expected) == "" {
		return false
	}
	actual, err := fileSHA256(path)
	return err == nil && strings.EqualFold(actual, strings.TrimSpace(expected))
}

func copyFile(source, destination string, mode os.FileMode) error {
	in, err := os.Open(filepath.Clean(source))
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	tmp := destination + ".part"
	out, err := os.OpenFile(filepath.Clean(tmp), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := out.ReadFrom(in); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Chmod(tmp, info.Mode().Perm()); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, destination)
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".part"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}
