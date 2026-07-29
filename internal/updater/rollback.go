package updater

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

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
		if runtime.GOOS == "windows" {
			return true, startWindowsRollback(previousBinary, executable, snapshot, logPath, readiness)
		}
		return true, startUnixRollback(previousBinary, executable, snapshot, logPath, readiness)
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
	cmd := exec.Command("/bin/bash", scriptPath, strconv.Itoa(os.Getpid()), previousBinary, targetBinary,
		snapshot.DatabasePath, snapshot.ConfigPath, db.CurrentDBPath, config.ConfigFilePath(), logPath, readiness) //nolint:gosec
	if err := cmd.Start(); err != nil {
		return err
	}
	time.AfterFunc(restartDelay, func() { os.Exit(0) })
	return nil
}

func startWindowsRollback(previousBinary, targetBinary string, snapshot service.SafetySnapshot, logPath, readiness string) error {
	scriptPath := filepath.Join(config.DataDir(), "updates", "rollback_update.bat")
	script := `@echo off
setlocal
set "OLD_PID=%~1"
set "PREVIOUS_BIN=%~2"
set "TARGET_BIN=%~3"
set "SNAPSHOT_DB=%~4"
set "SNAPSHOT_CONFIG=%~5"
set "DATABASE_PATH=%~6"
set "CONFIG_PATH=%~7"
set "LOG_FILE=%~8"
set "READY_URL=%~9"
:waitloop
tasklist /FI "PID eq %OLD_PID%" | find "%OLD_PID%" >nul
if %ERRORLEVEL%==0 (
  timeout /t 1 /nobreak >nul
  goto waitloop
)
copy /Y "%TARGET_BIN%" "%TARGET_BIN%.failed" >nul 2>nul
copy /Y "%PREVIOUS_BIN%" "%TARGET_BIN%.restore" >nul
move /Y "%TARGET_BIN%.restore" "%TARGET_BIN%" >nul
if exist "%DATABASE_PATH%-wal" del /F /Q "%DATABASE_PATH%-wal" >nul 2>nul
if exist "%DATABASE_PATH%-shm" del /F /Q "%DATABASE_PATH%-shm" >nul 2>nul
copy /Y "%SNAPSHOT_DB%" "%DATABASE_PATH%.restore" >nul
move /Y "%DATABASE_PATH%.restore" "%DATABASE_PATH%" >nul
if not "%SNAPSHOT_CONFIG%"=="" if exist "%SNAPSHOT_CONFIG%" (
  copy /Y "%SNAPSHOT_CONFIG%" "%CONFIG_PATH%.restore" >nul
  move /Y "%CONFIG_PATH%.restore" "%CONFIG_PATH%" >nul
)
for /F "delims=" %%P in ('powershell -NoProfile -NonInteractive -Command "$p=Start-Process -FilePath $env:TARGET_BIN -WorkingDirectory (Split-Path -Parent $env:TARGET_BIN) -PassThru; $p.Id"') do set "NEW_PID=%%P"
if not defined NEW_PID (
  echo [%DATE% %TIME%] failed to start rolled back executable >> "%LOG_FILE%"
  exit /b 1
)
for /L %%I in (1,1,60) do (
  curl.exe -fsS --max-time 2 "%READY_URL%" >nul 2>nul
  if not errorlevel 1 (
    echo [%DATE% %TIME%] manual updater rollback completed >> "%LOG_FILE%"
    exit /b 0
  )
  timeout /t 1 /nobreak >nul
)
echo [%DATE% %TIME%] rollback process started but readiness check failed >> "%LOG_FILE%"
exit /b 1
`
	if err := os.WriteFile(scriptPath, []byte(script), 0600); err != nil {
		return err
	}
	cmd := exec.Command("cmd", "/C", scriptPath, strconv.Itoa(os.Getpid()), previousBinary, targetBinary,
		snapshot.DatabasePath, snapshot.ConfigPath, db.CurrentDBPath, config.ConfigFilePath(), logPath, readiness) //nolint:gosec
	if err := cmd.Start(); err != nil {
		return err
	}
	time.AfterFunc(restartDelay, func() { os.Exit(0) })
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
	cmd := exec.Command("/bin/bash", scriptPath, strconv.Itoa(os.Getpid()), previousApp, targetApp,
		snapshot.DatabasePath, snapshot.ConfigPath, db.CurrentDBPath, config.ConfigFilePath(), logPath, readiness) //nolint:gosec
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start rollback helper: %w", err)
	}
	time.AfterFunc(restartDelay, func() { os.Exit(0) })
	return nil
}
