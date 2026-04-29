@echo off
setlocal
cd /d "%~dp0"
if not exist "%CD%\logs" mkdir "%CD%\logs" >nul 2>nul
if not exist "%CD%\logs\server.log" (
    echo No server.log yet. Start the app first.
    pause
    exit /b 1
)
start "" notepad "%CD%\logs\server.log"
exit /b 0
