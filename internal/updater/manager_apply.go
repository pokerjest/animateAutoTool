package updater

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/pokerjest/animateAutoTool/internal/config"
	"github.com/pokerjest/animateAutoTool/internal/safeio"
)

func applyUpdateForPlatform(artifactPath string) error {
	switch {
	case runtime.GOOS == "windows":
		return applyWindowsUpdate(artifactPath)
	case runtime.GOOS == goosDarwin && strings.HasSuffix(strings.ToLower(artifactPath), ".dmg"):
		return applyDarwinUpdate(artifactPath)
	case (runtime.GOOS == goosDarwin || runtime.GOOS == goosLinux) && strings.HasSuffix(strings.ToLower(artifactPath), ".tar.gz"):
		return applyArchiveBinaryUpdate(artifactPath)
	default:
		return fmt.Errorf("platform/artifact combination is not supported for self-update: %s %s", runtime.GOOS, filepath.Base(artifactPath))
	}
}

func applyArchiveBinaryUpdate(downloadedArchive string) error {
	exePath, err := os.Executable()
	if err != nil {
		return err
	}
	if !strings.HasSuffix(strings.ToLower(downloadedArchive), ".tar.gz") {
		return fmt.Errorf("expected .tar.gz artifact, got %s", downloadedArchive)
	}

	updateDir := filepath.Join(config.DataDir(), "updates")
	if err := os.MkdirAll(updateDir, 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(config.LogsDir(), 0755); err != nil {
		return err
	}

	stagedBinaryPath := filepath.Join(updateDir, filepath.Base(exePath)+".new")
	if err := extractBinaryFromTarGz(downloadedArchive, filepath.Base(exePath), stagedBinaryPath); err != nil {
		return err
	}

	scriptPath := filepath.Join(updateDir, "apply_update.sh")
	logPath := filepath.Join(config.LogsDir(), "updater.log")
	script := `#!/bin/bash
set -euo pipefail

OLD_PID="$1"
NEW_BIN="$2"
TARGET_BIN="$3"
LOG_FILE="$4"
TMP_BIN="${TARGET_BIN}.new"
BAK_BIN="${TARGET_BIN}.bak"

while kill -0 "$OLD_PID" >/dev/null 2>&1; do
  sleep 1
done

cp "$NEW_BIN" "$TMP_BIN"
chmod +x "$TMP_BIN"

if [ -f "$BAK_BIN" ]; then
  rm -f "$BAK_BIN"
fi
if [ -f "$TARGET_BIN" ]; then
  mv "$TARGET_BIN" "$BAK_BIN"
fi

if ! mv "$TMP_BIN" "$TARGET_BIN"; then
  echo "[$(date)] promote failed, restoring backup" >> "$LOG_FILE"
  rm -f "$TMP_BIN" || true
  if [ -f "$BAK_BIN" ]; then
    mv "$BAK_BIN" "$TARGET_BIN" || true
  fi
  exit 1
fi

rm -f "$BAK_BIN" || true
nohup "$TARGET_BIN" >> "$LOG_FILE" 2>&1 &
`
	if err := os.WriteFile(scriptPath, []byte(script), 0600); err != nil {
		return err
	}
	if err := os.Chmod(scriptPath, 0700); err != nil {
		return err
	}

	cmd := exec.Command("/bin/bash", scriptPath, strconv.Itoa(os.Getpid()), stagedBinaryPath, exePath, logPath) //nolint:gosec
	if err := cmd.Start(); err != nil {
		return err
	}

	time.AfterFunc(restartDelay, func() { os.Exit(0) })
	return nil
}

func extractBinaryFromTarGz(archivePath, targetBinaryName, destinationPath string) error {
	src, err := os.Open(archivePath) //nolint:gosec
	if err != nil {
		return err
	}
	defer safeio.Close(src)

	gzReader, err := gzip.NewReader(src)
	if err != nil {
		return err
	}
	defer safeio.Close(gzReader)

	tarReader := tar.NewReader(gzReader)
	targetBinaryName = strings.TrimSpace(targetBinaryName)
	if targetBinaryName == "" {
		return errors.New("target binary name is empty")
	}

	tempPath := destinationPath + ".part"
	_ = os.Remove(tempPath)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			_ = os.Remove(tempPath)
			return err
		}
		if header == nil || header.Typeflag != tar.TypeReg {
			continue
		}

		name := filepath.Base(strings.TrimSpace(header.Name))
		if !strings.EqualFold(name, targetBinaryName) {
			continue
		}
		if !strings.Contains(filepath.ToSlash(header.Name), "/bin/") {
			continue
		}

		out, err := os.OpenFile(filepath.Clean(tempPath), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0755) //nolint:gosec
		if err != nil {
			return err
		}
		if _, err := io.CopyN(out, tarReader, header.Size); err != nil { //nolint:gosec
			_ = out.Close()
			_ = os.Remove(tempPath)
			return err
		}
		if err := out.Close(); err != nil {
			_ = os.Remove(tempPath)
			return err
		}
		if err := os.Rename(tempPath, destinationPath); err != nil {
			_ = os.Remove(tempPath)
			return err
		}
		if err := os.Chmod(destinationPath, 0755); err != nil {
			return err
		}
		return nil
	}

	_ = os.Remove(tempPath)
	return fmt.Errorf("binary %s not found in archive %s", targetBinaryName, filepath.Base(archivePath))
}

func applyWindowsUpdate(downloadedExe string) error {
	exePath, err := os.Executable()
	if err != nil {
		return err
	}
	if !strings.EqualFold(filepath.Ext(downloadedExe), ".exe") {
		return fmt.Errorf("expected .exe artifact, got %s", downloadedExe)
	}

	updateDir := filepath.Join(config.DataDir(), "updates")
	if err := os.MkdirAll(updateDir, 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(config.LogsDir(), 0755); err != nil {
		return err
	}

	scriptPath := filepath.Join(updateDir, "apply_update.bat")
	logPath := filepath.Join(config.LogsDir(), "updater.log")
	script := `@echo off
setlocal
set "OLD_PID=%~1"
set "NEW_EXE=%~2"
set "TARGET_EXE=%~3"
set "LOG_FILE=%~4"
set "TMP_EXE=%TARGET_EXE%.new"
set "BAK_EXE=%TARGET_EXE%.bak"

:waitloop
tasklist /FI "PID eq %OLD_PID%" | find "%OLD_PID%" >nul
if %ERRORLEVEL%==0 (
  timeout /t 1 /nobreak >nul
  goto waitloop
)

copy /Y "%NEW_EXE%" "%TMP_EXE%" >nul
if %ERRORLEVEL% neq 0 (
  echo [%DATE% %TIME%] stage copy failed >> "%LOG_FILE%"
  exit /b 1
)

if exist "%BAK_EXE%" del /F /Q "%BAK_EXE%" >nul 2>nul
if exist "%TARGET_EXE%" ren "%TARGET_EXE%" "%~n3%~x3.bak"
if %ERRORLEVEL% neq 0 (
  echo [%DATE% %TIME%] backup rename failed >> "%LOG_FILE%"
  del /F /Q "%TMP_EXE%" >nul 2>nul
  exit /b 1
)

move /Y "%TMP_EXE%" "%TARGET_EXE%" >nul
if %ERRORLEVEL% neq 0 (
  echo [%DATE% %TIME%] promote failed >> "%LOG_FILE%"
  if exist "%BAK_EXE%" move /Y "%BAK_EXE%" "%TARGET_EXE%" >nul 2>nul
  exit /b 1
)

if exist "%BAK_EXE%" del /F /Q "%BAK_EXE%" >nul 2>nul
start "" "%TARGET_EXE%"
`
	if err := os.WriteFile(scriptPath, []byte(script), 0600); err != nil {
		return err
	}

	cmd := exec.Command("cmd", "/C", scriptPath, strconv.Itoa(os.Getpid()), downloadedExe, exePath, logPath) //nolint:gosec
	if err := cmd.Start(); err != nil {
		return err
	}

	time.AfterFunc(restartDelay, func() { os.Exit(0) })
	return nil
}

func applyDarwinUpdate(downloadedDMG string) error {
	exePath, err := os.Executable()
	if err != nil {
		return err
	}
	if !strings.EqualFold(filepath.Ext(downloadedDMG), ".dmg") {
		return fmt.Errorf("expected .dmg artifact, got %s", downloadedDMG)
	}

	bundlePath := currentAppBundlePath(exePath)
	if bundlePath == "" {
		return errors.New("current process is not inside .app bundle; cannot auto-apply dmg")
	}

	updateDir := filepath.Join(config.DataDir(), "updates")
	if err := os.MkdirAll(updateDir, 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(config.LogsDir(), 0755); err != nil {
		return err
	}

	scriptPath := filepath.Join(updateDir, "apply_update.sh")
	logPath := filepath.Join(config.LogsDir(), "updater.log")
	targetDir := filepath.Dir(bundlePath)
	appName := filepath.Base(bundlePath)
	mountPoint, err := os.MkdirTemp("", "animate_update_mount.")
	if err != nil {
		return err
	}

	script := `#!/bin/bash
set -euo pipefail

OLD_PID="$1"
DMG_PATH="$2"
TARGET_DIR="$3"
APP_NAME="$4"
LOG_FILE="$5"
MOUNT_POINT="$6"

while kill -0 "$OLD_PID" >/dev/null 2>&1; do
  sleep 1
done

cleanup() {
  hdiutil detach "$MOUNT_POINT" -quiet >/dev/null 2>&1 || true
  rm -rf "$MOUNT_POINT"
}
trap cleanup EXIT

hdiutil attach "$DMG_PATH" -nobrowse -mountpoint "$MOUNT_POINT" -quiet
SRC_APP="$MOUNT_POINT/$APP_NAME"
if [ ! -d "$SRC_APP" ]; then
  SRC_APP="$(find "$MOUNT_POINT" -maxdepth 1 -name "*.app" | head -n 1)"
fi

if [ -z "$SRC_APP" ] || [ ! -d "$SRC_APP" ]; then
  echo "[$(date)] source app not found in dmg" >> "$LOG_FILE"
  exit 1
fi

TARGET_APP="$TARGET_DIR/$APP_NAME"
STAGE_APP="$TARGET_DIR/.update-$APP_NAME"
BACKUP_APP="$TARGET_DIR/.backup-$APP_NAME"

rm -rf "$STAGE_APP" "$BACKUP_APP" || true
cp -R "$SRC_APP" "$STAGE_APP"

if [ -d "$TARGET_APP" ]; then
  mv "$TARGET_APP" "$BACKUP_APP"
fi

if ! mv "$STAGE_APP" "$TARGET_APP"; then
  echo "[$(date)] promote failed, restoring backup" >> "$LOG_FILE"
  rm -rf "$TARGET_APP" || true
  if [ -d "$BACKUP_APP" ]; then
    mv "$BACKUP_APP" "$TARGET_APP" || true
  fi
  exit 1
fi

rm -rf "$BACKUP_APP" || true
open "$TARGET_APP"
`
	if err := os.WriteFile(scriptPath, []byte(script), 0600); err != nil {
		return err
	}
	if err := os.Chmod(scriptPath, 0700); err != nil {
		return err
	}

	cmd := exec.Command("/bin/bash", scriptPath, strconv.Itoa(os.Getpid()), downloadedDMG, targetDir, appName, logPath, mountPoint) //nolint:gosec
	if err := cmd.Start(); err != nil {
		_ = os.RemoveAll(mountPoint)
		return err
	}

	time.AfterFunc(restartDelay, func() { os.Exit(0) })
	return nil
}

func currentAppBundlePath(executablePath string) string {
	clean := filepath.Clean(executablePath)
	normalized := filepath.ToSlash(clean)
	marker := ".app/Contents/MacOS/"
	idx := strings.Index(normalized, marker)
	if idx < 0 {
		return ""
	}
	return normalized[:idx+4]
}
