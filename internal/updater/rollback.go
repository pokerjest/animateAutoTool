package updater

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/pokerjest/animateAutoTool/internal/appidentity"
	"github.com/pokerjest/animateAutoTool/internal/appshutdown"
	"github.com/pokerjest/animateAutoTool/internal/config"
	"github.com/pokerjest/animateAutoTool/internal/db"
	"github.com/pokerjest/animateAutoTool/internal/service"
)

// RollbackSnapshot restores a local updater snapshot. When the snapshot also
// contains the previous executable/app bundle, replacement is delegated to a
// helper process after this process exits. Data-only snapshots are restored in
// process and require a normal restart by the caller.
func RollbackSnapshot(id string) (bool, error) {
	snapshot, err := service.LoadSafetySnapshot(id)
	if err != nil {
		return false, err
	}
	executable, err := os.Executable()
	if err != nil {
		return false, err
	}
	snapshotDir := filepath.Dir(snapshot.DatabasePath)
	readiness := readinessURL()
	logPath := filepath.Join(config.LogsDir(), "updater.log")

	if runtime.GOOS == goosDarwin {
		if currentApp := currentAppBundlePath(executable); currentApp != "" {
			previousApp := filepath.Join(snapshotDir, "previous-app")
			if info, statErr := os.Stat(previousApp); statErr == nil && info.IsDir() {
				return true, startDarwinRollback(previousApp, currentApp, snapshot, logPath, readiness)
			}
		}
	}

	previousBinary := filepath.Join(snapshotDir, "previous-binary")
	if info, statErr := os.Stat(previousBinary); statErr == nil && !info.IsDir() {
		targetBinary := appidentity.CanonicalExecutablePath(executable, runtime.GOOS)
		if runtime.GOOS == goosWindows {
			return true, startWindowsRollback(previousBinary, targetBinary, snapshot, logPath, readiness)
		}
		return true, startUnixRollback(previousBinary, targetBinary, snapshot, logPath, readiness)
	}

	return false, service.RestoreSafetySnapshot(id)
}

func startUnixRollback(previousBinary, targetBinary string, snapshot service.SafetySnapshot, logPath, readiness string) error {
	scriptPath := filepath.Join(config.DataDir(), "updates", "rollback_update.sh")
	script := `#!/bin/bash
set -euo pipefail
OLD_PID="$1"
PREVIOUS_BIN="$2"
TARGET_BIN="$3"
SNAPSHOT_DB="$4"
SNAPSHOT_CONFIG="$5"
DATABASE_PATH="$6"
CONFIG_PATH="$7"
LOG_FILE="$8"
READY_URL="$9"

while kill -0 "$OLD_PID" >/dev/null 2>&1; do sleep 1; done
cp "$TARGET_BIN" "${TARGET_BIN}.failed" || true
cp "$PREVIOUS_BIN" "${TARGET_BIN}.restore"
chmod +x "${TARGET_BIN}.restore"
mv "${TARGET_BIN}.restore" "$TARGET_BIN"
rm -f "$DATABASE_PATH-wal" "$DATABASE_PATH-shm" || true
cp "$SNAPSHOT_DB" "${DATABASE_PATH}.restore"
mv "${DATABASE_PATH}.restore" "$DATABASE_PATH"
if [ -n "$SNAPSHOT_CONFIG" ] && [ -f "$SNAPSHOT_CONFIG" ]; then
  cp "$SNAPSHOT_CONFIG" "${CONFIG_PATH}.restore"
  mv "${CONFIG_PATH}.restore" "$CONFIG_PATH"
fi
nohup "$TARGET_BIN" >> "$LOG_FILE" 2>&1 &
for i in $(seq 1 60); do
  if curl -fsS --max-time 2 "$READY_URL" >/dev/null 2>&1; then
    echo "[$(date)] manual updater rollback completed" >> "$LOG_FILE"
    exit 0
  fi
  sleep 1
done
echo "[$(date)] rollback process started but readiness check failed" >> "$LOG_FILE"
exit 1
`
	if err := os.WriteFile(scriptPath, []byte(script), 0600); err != nil {
		return err
	}
	if err := os.Chmod(scriptPath, 0700); err != nil {
		return err
	}
	// The helper path is generated inside AnimateTool's data directory and all
	// values are passed as positional arguments rather than interpolated into a
	// command string.
	//nolint:gosec
	cmd := exec.Command("/bin/bash", scriptPath, strconv.Itoa(os.Getpid()), previousBinary, targetBinary,
		snapshot.DatabasePath, snapshot.ConfigPath, db.CurrentDBPath, config.ConfigFilePath(), logPath, readiness)
	if err := cmd.Start(); err != nil {
		return err
	}
	appshutdown.Request("updater-rollback")
	return nil
}

func startWindowsRollback(previousBinary, targetBinary string, snapshot service.SafetySnapshot, logPath, readiness string) error {
	scriptPath := filepath.Join(config.DataDir(), "updates", "rollback_update.ps1")
	script := `param(
  [Parameter(Mandatory=$true)][int]$OldPid,
  [Parameter(Mandatory=$true)][string]$PreviousBinary,
  [Parameter(Mandatory=$true)][string]$TargetBinary,
  [Parameter(Mandatory=$true)][string]$SnapshotDatabase,
  [string]$SnapshotConfig = "",
  [Parameter(Mandatory=$true)][string]$DatabasePath,
  [Parameter(Mandatory=$true)][string]$ConfigPath,
  [Parameter(Mandatory=$true)][string]$LogFile,
  [Parameter(Mandatory=$true)][string]$ReadyUrl
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

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

while (Get-Process -Id $OldPid -ErrorAction SilentlyContinue) {
  Start-Sleep -Seconds 1
}

try {
  Copy-Item -LiteralPath $TargetBinary -Destination "$TargetBinary.failed" -Force -ErrorAction SilentlyContinue
  Copy-Item -LiteralPath $PreviousBinary -Destination "$TargetBinary.restore" -Force
  if (Test-Path -LiteralPath $TargetBinary) {
    Remove-Item -LiteralPath $TargetBinary -Force
  }
  Move-Item -LiteralPath "$TargetBinary.restore" -Destination $TargetBinary -Force

  Remove-Item -LiteralPath "$DatabasePath-wal" -Force -ErrorAction SilentlyContinue
  Remove-Item -LiteralPath "$DatabasePath-shm" -Force -ErrorAction SilentlyContinue
  Copy-Item -LiteralPath $SnapshotDatabase -Destination "$DatabasePath.restore" -Force
  Move-Item -LiteralPath "$DatabasePath.restore" -Destination $DatabasePath -Force
  if ($SnapshotConfig -ne "" -and (Test-Path -LiteralPath $SnapshotConfig)) {
    Copy-Item -LiteralPath $SnapshotConfig -Destination "$ConfigPath.restore" -Force
    Move-Item -LiteralPath "$ConfigPath.restore" -Destination $ConfigPath -Force
  }

  Start-Process -FilePath $TargetBinary -WorkingDirectory (Split-Path -Parent $TargetBinary) -WindowStyle Hidden | Out-Null
  if (Test-Readiness) {
    Write-UpdateLog "manual updater rollback completed"
    exit 0
  }
  Write-UpdateLog "rollback process started but readiness check failed"
  exit 1
} catch {
  Write-UpdateLog ("rollback helper failed: {0}" -f $_.Exception.Message)
  exit 1
}
`
	if err := os.WriteFile(scriptPath, []byte(script), 0600); err != nil {
		return err
	}
	helperArgs := []string{
		"-OldPid", strconv.Itoa(os.Getpid()),
		"-PreviousBinary", previousBinary,
		"-TargetBinary", targetBinary,
		"-SnapshotDatabase", snapshot.DatabasePath,
	}
	if snapshot.ConfigPath != "" {
		helperArgs = append(helperArgs, "-SnapshotConfig", snapshot.ConfigPath)
	}
	helperArgs = append(helperArgs,
		"-DatabasePath", db.CurrentDBPath,
		"-ConfigPath", config.ConfigFilePath(),
		"-LogFile", logPath,
		"-ReadyUrl", readiness,
	)
	cmd := powershellScriptCommand(scriptPath, helperArgs...)
	configureUpdateHelper(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	appshutdown.Request("updater-rollback")
	return nil
}

func startDarwinRollback(previousApp, targetApp string, snapshot service.SafetySnapshot, logPath, readiness string) error {
	if previousApp == "" || targetApp == "" {
		return errors.New("rollback app paths are missing")
	}
	scriptPath := filepath.Join(config.DataDir(), "updates", "rollback_update.sh")
	script := `#!/bin/bash
set -euo pipefail
OLD_PID="$1"
PREVIOUS_APP="$2"
TARGET_APP="$3"
SNAPSHOT_DB="$4"
SNAPSHOT_CONFIG="$5"
DATABASE_PATH="$6"
CONFIG_PATH="$7"
LOG_FILE="$8"
READY_URL="$9"

while kill -0 "$OLD_PID" >/dev/null 2>&1; do sleep 1; done
FAILED_APP="${TARGET_APP}.failed"
STAGE_APP="${TARGET_APP}.restore"
rm -rf "$FAILED_APP" "$STAGE_APP" || true
mv "$TARGET_APP" "$FAILED_APP"
cp -R "$PREVIOUS_APP" "$STAGE_APP"
mv "$STAGE_APP" "$TARGET_APP"
rm -f "$DATABASE_PATH-wal" "$DATABASE_PATH-shm" || true
cp "$SNAPSHOT_DB" "${DATABASE_PATH}.restore"
mv "${DATABASE_PATH}.restore" "$DATABASE_PATH"
if [ -n "$SNAPSHOT_CONFIG" ] && [ -f "$SNAPSHOT_CONFIG" ]; then
  cp "$SNAPSHOT_CONFIG" "${CONFIG_PATH}.restore"
  mv "${CONFIG_PATH}.restore" "$CONFIG_PATH"
fi
open "$TARGET_APP"
for i in $(seq 1 60); do
  if curl -fsS --max-time 2 "$READY_URL" >/dev/null 2>&1; then
    echo "[$(date)] manual updater rollback completed" >> "$LOG_FILE"
    exit 0
  fi
  sleep 1
done
echo "[$(date)] rollback app started but readiness check failed" >> "$LOG_FILE"
exit 1
`
	if err := os.WriteFile(scriptPath, []byte(script), 0600); err != nil {
		return err
	}
	if err := os.Chmod(scriptPath, 0700); err != nil {
		return err
	}
	// The helper path is generated inside AnimateTool's data directory and all
	// values are passed as positional arguments rather than interpolated into a
	// command string.
	//nolint:gosec
	cmd := exec.Command("/bin/bash", scriptPath, strconv.Itoa(os.Getpid()), previousApp, targetApp,
		snapshot.DatabasePath, snapshot.ConfigPath, db.CurrentDBPath, config.ConfigFilePath(), logPath, readiness)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start rollback helper: %w", err)
	}
	appshutdown.Request("updater-rollback")
	return nil
}
