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

	"github.com/pokerjest/animateAutoTool/internal/appidentity"
	"github.com/pokerjest/animateAutoTool/internal/appshutdown"
	"github.com/pokerjest/animateAutoTool/internal/config"
	"github.com/pokerjest/animateAutoTool/internal/safeio"
)

func applyUpdateForPlatform(artifactPath, snapshotID, readiness, databasePath, configPath string) error {
	switch {
	case runtime.GOOS == goosWindows:
		return applyWindowsUpdate(artifactPath, snapshotID, readiness, databasePath, configPath)
	case runtime.GOOS == goosDarwin && strings.HasSuffix(strings.ToLower(artifactPath), ".dmg"):
		return applyDarwinUpdate(artifactPath, snapshotID, readiness, databasePath, configPath)
	case (runtime.GOOS == goosDarwin || runtime.GOOS == goosLinux) && strings.HasSuffix(strings.ToLower(artifactPath), ".tar.gz"):
		return applyArchiveBinaryUpdate(artifactPath, snapshotID, readiness, databasePath, configPath)
	default:
		return fmt.Errorf("platform/artifact combination is not supported for self-update: %s %s", runtime.GOOS, filepath.Base(artifactPath))
	}
}

func applyArchiveBinaryUpdate(downloadedArchive, snapshotID, readiness, databasePath, configPath string) error {
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

	targetExecutablePath := appidentity.CanonicalExecutablePath(exePath, runtime.GOOS)
	stagedBinaryPath := filepath.Join(updateDir, filepath.Base(targetExecutablePath)+".new")
	if err := extractBinaryFromTarGzCandidates(downloadedArchive, appidentity.ExecutableCandidates(runtime.GOOS), stagedBinaryPath); err != nil {
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
SNAPSHOT_DIR="$5"
READY_URL="$6"
DATABASE_PATH="$7"
CONFIG_PATH="$8"
LEGACY_BIN="$9"
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
elif [ -f "$LEGACY_BIN" ]; then
  mv "$LEGACY_BIN" "$BAK_BIN"
fi

if ! mv "$TMP_BIN" "$TARGET_BIN"; then
  echo "[$(date)] promote failed, restoring backup" >> "$LOG_FILE"
  rm -f "$TMP_BIN" || true
  if [ -f "$BAK_BIN" ]; then
    mv "$BAK_BIN" "$TARGET_BIN" || true
  fi
  exit 1
fi

nohup "$TARGET_BIN" >> "$LOG_FILE" 2>&1 &
NEW_PID=$!
READY=0
for i in $(seq 1 60); do
  if curl -fsS --max-time 2 "$READY_URL" >/dev/null 2>&1; then
    READY=1
    break
  fi
  sleep 1
done
if [ "$READY" -ne 1 ]; then
  kill "$NEW_PID" >/dev/null 2>&1 || true
  sleep 1
  rm -f "$DATABASE_PATH-wal" "$DATABASE_PATH-shm" || true
  if [ -f "$SNAPSHOT_DIR/database.db" ]; then
    cp "$SNAPSHOT_DIR/database.db" "$DATABASE_PATH.restore"
    mv "$DATABASE_PATH.restore" "$DATABASE_PATH"
  fi
  if [ -f "$SNAPSHOT_DIR/config.yaml" ]; then
    cp "$SNAPSHOT_DIR/config.yaml" "$CONFIG_PATH.restore"
    mv "$CONFIG_PATH.restore" "$CONFIG_PATH"
  fi
  if [ -f "$BAK_BIN" ]; then
    mv "$BAK_BIN" "$TARGET_BIN" || true
  fi
  nohup "$TARGET_BIN" >> "$LOG_FILE" 2>&1 &
  echo "[$(date)] readiness check failed; previous version restored and restarted" >> "$LOG_FILE"
  exit 1
fi
if [ -f "$BAK_BIN" ]; then
  mv "$BAK_BIN" "$SNAPSHOT_DIR/previous-binary" || rm -f "$BAK_BIN"
fi
`
	if err := os.WriteFile(scriptPath, []byte(script), 0600); err != nil {
		return err
	}
	if err := os.Chmod(scriptPath, 0700); err != nil {
		return err
	}

	snapshotDir := filepath.Join(config.DataDir(), "updates", "snapshots", filepath.Base(snapshotID))
	legacyExecutablePath := filepath.Join(filepath.Dir(exePath), appidentity.LegacyExecutableName(runtime.GOOS))
	cmd := exec.Command("/bin/bash", scriptPath, strconv.Itoa(os.Getpid()), stagedBinaryPath, targetExecutablePath, logPath, snapshotDir, readiness, databasePath, configPath, legacyExecutablePath) //nolint:gosec
	if err := cmd.Start(); err != nil {
		return err
	}

	appshutdown.Request("self-update")
	return nil
}

func extractBinaryFromTarGz(archivePath, targetBinaryName, destinationPath string) error {
	return extractBinaryFromTarGzCandidates(archivePath, []string{targetBinaryName}, destinationPath)
}

func extractBinaryFromTarGzCandidates(archivePath string, targetBinaryNames []string, destinationPath string) error {
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
	candidateRanks := make(map[string]int, len(targetBinaryNames))
	candidateLabels := make([]string, 0, len(targetBinaryNames))
	for _, candidate := range targetBinaryNames {
		candidate = filepath.Base(strings.TrimSpace(candidate))
		if candidate == "" {
			continue
		}
		key := strings.ToLower(candidate)
		if _, exists := candidateRanks[key]; exists {
			continue
		}
		candidateRanks[key] = len(candidateLabels)
		candidateLabels = append(candidateLabels, candidate)
	}
	if len(candidateLabels) == 0 {
		return errors.New("target binary names are empty")
	}

	tempPath := destinationPath + ".part"
	_ = os.Remove(tempPath)
	bestRank := len(candidateLabels)

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
		rank, isCandidate := candidateRanks[strings.ToLower(name)]
		if !isCandidate || rank >= bestRank {
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
		bestRank = rank
		if bestRank == 0 {
			break
		}
	}

	if bestRank == len(candidateLabels) {
		_ = os.Remove(tempPath)
		return fmt.Errorf("binaries %s not found in archive %s", strings.Join(candidateLabels, ", "), filepath.Base(archivePath))
	}
	_ = os.Remove(destinationPath)
	if err := os.Rename(tempPath, destinationPath); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	if err := os.Chmod(destinationPath, 0755); err != nil {
		return err
	}
	return nil
}

func applyWindowsUpdate(downloadedExe, snapshotID, readiness, databasePath, configPath string) error {
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
	targetExecutablePath := appidentity.CanonicalExecutablePath(exePath, runtime.GOOS)

	scriptPath := filepath.Join(updateDir, "apply_update.ps1")
	logPath := filepath.Join(config.LogsDir(), "updater.log")
	script := `param(
  [Parameter(Mandatory=$true)][int]$OldPid,
  [Parameter(Mandatory=$true)][string]$NewExe,
  [Parameter(Mandatory=$true)][string]$TargetExe,
  [Parameter(Mandatory=$true)][string]$LogFile,
  [Parameter(Mandatory=$true)][string]$SnapshotDir,
  [Parameter(Mandatory=$true)][string]$ReadyUrl,
  [Parameter(Mandatory=$true)][string]$DatabasePath,
  [Parameter(Mandatory=$true)][string]$ConfigPath,
  [Parameter(Mandatory=$true)][string]$LegacyExe
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
$TmpExe = "$TargetExe.new"
$BakExe = "$TargetExe.bak"

function Write-UpdateLog([string]$Message) {
  try {
    Add-Content -LiteralPath $LogFile -Value ("[{0}] {1}" -f (Get-Date -Format "s"), $Message)
  } catch {}
}

function Test-Readiness {
  for ($attempt = 0; $attempt -lt 60; $attempt++) {
    try {
      $request = [System.Net.HttpWebRequest]::Create($ReadyUrl)
      $request.Method = "GET"
      $request.Timeout = 2000
      $request.ReadWriteTimeout = 2000
      $response = $request.GetResponse()
      try {
        return $true
      } finally {
        $response.Close()
      }
    } catch {}
    Start-Sleep -Seconds 1
  }
  return $false
}

function Restore-PreviousBinary {
  if (Test-Path -LiteralPath $BakExe) {
    if (Test-Path -LiteralPath $TargetExe) {
      Remove-Item -LiteralPath $TargetExe -Force
    }
    Move-Item -LiteralPath $BakExe -Destination $TargetExe -Force
  }
}

while (Get-Process -Id $OldPid -ErrorAction SilentlyContinue) {
  Start-Sleep -Seconds 1
}

try {
  Copy-Item -LiteralPath $NewExe -Destination $TmpExe -Force
  if (Test-Path -LiteralPath $BakExe) {
    Remove-Item -LiteralPath $BakExe -Force
  }
  if (Test-Path -LiteralPath $TargetExe) {
    Move-Item -LiteralPath $TargetExe -Destination $BakExe -Force
  } elseif (Test-Path -LiteralPath $LegacyExe) {
    Move-Item -LiteralPath $LegacyExe -Destination $BakExe -Force
  }
  Move-Item -LiteralPath $TmpExe -Destination $TargetExe -Force

  $workingDirectory = Split-Path -Parent $TargetExe
  $process = Start-Process -FilePath $TargetExe -WorkingDirectory $workingDirectory -WindowStyle Hidden -PassThru
  if (Test-Readiness) {
    if (Test-Path -LiteralPath $BakExe) {
      try {
        $previousBinary = Join-Path $SnapshotDir "previous-binary"
        if (Test-Path -LiteralPath $previousBinary) {
          Remove-Item -LiteralPath $previousBinary -Force
        }
        Move-Item -LiteralPath $BakExe -Destination $previousBinary -Force
      } catch {
        Write-UpdateLog ("updated version is ready, but preserving the previous binary failed: {0}" -f $_.Exception.Message)
      }
    }
    exit 0
  }

  Stop-Process -Id $process.Id -Force -ErrorAction SilentlyContinue
  Start-Sleep -Seconds 1
  Remove-Item -LiteralPath "$DatabasePath-wal" -Force -ErrorAction SilentlyContinue
  Remove-Item -LiteralPath "$DatabasePath-shm" -Force -ErrorAction SilentlyContinue
  $snapshotDatabase = Join-Path $SnapshotDir "database.db"
  if (Test-Path -LiteralPath $snapshotDatabase) {
    Copy-Item -LiteralPath $snapshotDatabase -Destination "$DatabasePath.restore" -Force
    Move-Item -LiteralPath "$DatabasePath.restore" -Destination $DatabasePath -Force
  }
  $snapshotConfig = Join-Path $SnapshotDir "config.yaml"
  if (Test-Path -LiteralPath $snapshotConfig) {
    Copy-Item -LiteralPath $snapshotConfig -Destination "$ConfigPath.restore" -Force
    Move-Item -LiteralPath "$ConfigPath.restore" -Destination $ConfigPath -Force
  }
  Restore-PreviousBinary
  Start-Process -FilePath $TargetExe -WorkingDirectory $workingDirectory -WindowStyle Hidden | Out-Null
  Write-UpdateLog "readiness check failed; previous version restored and restarted"
  exit 1
} catch {
  Write-UpdateLog ("update helper failed: {0}" -f $_.Exception.Message)
  Remove-Item -LiteralPath $TmpExe -Force -ErrorAction SilentlyContinue
  try {
    Restore-PreviousBinary
    if (Test-Path -LiteralPath $TargetExe) {
      Start-Process -FilePath $TargetExe -WorkingDirectory (Split-Path -Parent $TargetExe) -WindowStyle Hidden | Out-Null
    }
  } catch {
    Write-UpdateLog ("failed to restart previous version: {0}" -f $_.Exception.Message)
  }
  exit 1
}
`
	if err := os.WriteFile(scriptPath, []byte(script), 0600); err != nil {
		return err
	}

	snapshotDir := filepath.Join(config.DataDir(), "updates", "snapshots", filepath.Base(snapshotID))
	legacyExecutablePath := filepath.Join(filepath.Dir(exePath), appidentity.LegacyExecutableName(runtime.GOOS))
	cmd := powershellScriptCommand(scriptPath,
		"-OldPid", strconv.Itoa(os.Getpid()),
		"-NewExe", downloadedExe,
		"-TargetExe", targetExecutablePath,
		"-LogFile", logPath,
		"-SnapshotDir", snapshotDir,
		"-ReadyUrl", readiness,
		"-DatabasePath", databasePath,
		"-ConfigPath", configPath,
		"-LegacyExe", legacyExecutablePath,
	)
	configureUpdateHelper(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}

	appshutdown.Request("self-update")
	return nil
}

func powershellScriptCommand(scriptPath string, args ...string) *exec.Cmd {
	powershellArgs := []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", scriptPath}
	powershellArgs = append(powershellArgs, args...)
	return exec.Command("powershell.exe", powershellArgs...) //nolint:gosec
}

func applyDarwinUpdate(downloadedDMG, snapshotID, readiness, databasePath, configPath string) error {
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
SNAPSHOT_DIR="$7"
READY_URL="$8"
DATABASE_PATH="$9"
CONFIG_PATH="${10}"

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
  rm -f "$DATABASE_PATH-wal" "$DATABASE_PATH-shm" || true
  if [ -f "$SNAPSHOT_DIR/database.db" ]; then
    cp "$SNAPSHOT_DIR/database.db" "$DATABASE_PATH.restore"
    mv "$DATABASE_PATH.restore" "$DATABASE_PATH"
  fi
  if [ -f "$SNAPSHOT_DIR/config.yaml" ]; then
    cp "$SNAPSHOT_DIR/config.yaml" "$CONFIG_PATH.restore"
    mv "$CONFIG_PATH.restore" "$CONFIG_PATH"
  fi
  exit 1
fi

open "$TARGET_APP"
READY=0
for i in $(seq 1 60); do
  if curl -fsS --max-time 2 "$READY_URL" >/dev/null 2>&1; then
    READY=1
    break
  fi
  sleep 1
done
if [ "$READY" -ne 1 ]; then
  pkill -f "$TARGET_APP/Contents/MacOS/" >/dev/null 2>&1 || true
  rm -rf "$TARGET_APP" || true
  if [ -d "$BACKUP_APP" ]; then
    mv "$BACKUP_APP" "$TARGET_APP" || true
  fi
  rm -f "$DATABASE_PATH-wal" "$DATABASE_PATH-shm" || true
  if [ -f "$SNAPSHOT_DIR/database.db" ]; then
    cp "$SNAPSHOT_DIR/database.db" "$DATABASE_PATH.restore"
    mv "$DATABASE_PATH.restore" "$DATABASE_PATH"
  fi
  if [ -f "$SNAPSHOT_DIR/config.yaml" ]; then
    cp "$SNAPSHOT_DIR/config.yaml" "$CONFIG_PATH.restore"
    mv "$CONFIG_PATH.restore" "$CONFIG_PATH"
  fi
  open "$TARGET_APP"
  echo "[$(date)] readiness check failed; previous version restored and restarted" >> "$LOG_FILE"
  exit 1
fi
if [ -d "$BACKUP_APP" ]; then
  mv "$BACKUP_APP" "$SNAPSHOT_DIR/previous-app" || rm -rf "$BACKUP_APP"
fi
`
	if err := os.WriteFile(scriptPath, []byte(script), 0600); err != nil {
		return err
	}
	if err := os.Chmod(scriptPath, 0700); err != nil {
		return err
	}

	snapshotDir := filepath.Join(config.DataDir(), "updates", "snapshots", filepath.Base(snapshotID))
	cmd := exec.Command("/bin/bash", scriptPath, strconv.Itoa(os.Getpid()), downloadedDMG, targetDir, appName, logPath, mountPoint, snapshotDir, readiness, databasePath, configPath) //nolint:gosec
	if err := cmd.Start(); err != nil {
		_ = os.RemoveAll(mountPoint)
		return err
	}

	appshutdown.Request("self-update")
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
