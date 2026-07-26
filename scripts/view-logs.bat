@echo off
setlocal
cd /d "%~dp0"
if not exist "%CD%\logs" mkdir "%CD%\logs" >nul 2>nul
set "LATEST_LOG="
for /f "delims=" %%F in ('dir /b /a-d /o-d "%CD%\logs\server-*.log" 2^>nul') do if not defined LATEST_LOG set "LATEST_LOG=%CD%\logs\%%F"
if not defined LATEST_LOG (
    echo No hourly server log yet. Start the app first.
    pause
    exit /b 1
)
start "" notepad "%LATEST_LOG%"
exit /b 0
