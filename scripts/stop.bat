@echo off
setlocal EnableExtensions EnableDelayedExpansion
cd /d "%~dp0"
set "PID_FILE=%CD%\bin\server.pid"
set "APP_EXE=%CD%\bin\AnimateAutoTool.exe"

if not exist "%PID_FILE%" goto not_found
set /p APP_PID=<"%PID_FILE%"
if "!APP_PID!"=="" goto stale_pid

set "AAT_STOP_PID=!APP_PID!"
set "AAT_EXPECTED_EXE=%APP_EXE%"
echo Stopping Animate Auto Tool PID=!APP_PID!...
powershell -NoProfile -NonInteractive -ExecutionPolicy Bypass -Command "$id = 0; if (-not [int]::TryParse($env:AAT_STOP_PID, [ref]$id)) { exit 3 }; $process = Get-Process -Id $id -ErrorAction SilentlyContinue; if ($null -eq $process) { exit 2 }; try { $actual = [IO.Path]::GetFullPath($process.Path); $expected = [IO.Path]::GetFullPath($env:AAT_EXPECTED_EXE) } catch { exit 3 }; if (-not [string]::Equals($actual, $expected, [StringComparison]::OrdinalIgnoreCase)) { exit 3 }; try { $event = [Threading.EventWaitHandle]::OpenExisting(('Local\AnimateAutoTool-Shutdown-{0}' -f $id)) } catch { exit 4 }; try { if (-not $event.Set()) { exit 4 } } finally { $event.Dispose() }; exit 0" >nul 2>nul
set "SIGNAL_RESULT=!ERRORLEVEL!"

if "!SIGNAL_RESULT!"=="2" goto stale_pid
if "!SIGNAL_RESULT!"=="3" goto stale_pid
if "!SIGNAL_RESULT!"=="4" goto legacy_force
if not "!SIGNAL_RESULT!"=="0" goto stop_failed

powershell -NoProfile -NonInteractive -Command "$id = [int]$env:AAT_STOP_PID; $deadline = (Get-Date).AddSeconds(60); while ((Get-Process -Id $id -ErrorAction SilentlyContinue) -and (Get-Date) -lt $deadline) { Start-Sleep -Milliseconds 250 }; if (Get-Process -Id $id -ErrorAction SilentlyContinue) { exit 1 }; exit 0" >nul 2>nul
if errorlevel 1 goto timeout_force

del "%PID_FILE%" >nul 2>nul
echo Server stopped gracefully.
exit /b 0

:legacy_force
echo Graceful shutdown control is unavailable; stopping the older process directly...
goto force_stop

:timeout_force
echo Graceful shutdown timed out after 60 seconds; forcing process termination...

:force_stop
powershell -NoProfile -NonInteractive -Command "Stop-Process -Id ([int]$env:AAT_STOP_PID) -Force -ErrorAction Stop" >nul 2>nul
if errorlevel 1 goto stop_failed
del "%PID_FILE%" >nul 2>nul
echo Server stopped.
exit /b 0

:stale_pid
echo Stored PID is empty, invalid, no longer running, or belongs to another executable.
del "%PID_FILE%" >nul 2>nul
echo Cleaned stale PID file.
exit /b 0

:stop_failed
echo Failed to stop PID=!APP_PID!. The PID file was left in place for inspection.
exit /b 1

:not_found
echo No running Animate Auto Tool process was found from %PID_FILE%.
echo If the server was started outside start.bat, stop that process manually.
exit /b 0
