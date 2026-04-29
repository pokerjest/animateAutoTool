@echo off
setlocal
cd /d "%~dp0"
set "PID_FILE=%CD%\logs\animate-server.pid"

if exist "%PID_FILE%" (
    set /p APP_PID=<"%PID_FILE%"
    if not "%APP_PID%"=="" (
        echo Stopping Animate Auto Tool PID=%APP_PID%...
        powershell -NoProfile -NonInteractive -Command "Stop-Process -Id %APP_PID% -Force -ErrorAction Stop" >nul 2>nul
        if %ERRORLEVEL% EQU 0 (
            del "%PID_FILE%" >nul 2>nul
            echo Server stopped.
            exit /b 0
        )
        echo Stored PID was not running. Cleaning stale PID file.
        del "%PID_FILE%" >nul 2>nul
    ) else (
        echo PID file was empty. Cleaning stale PID file.
        del "%PID_FILE%" >nul 2>nul
    )
)

echo No PID file found. Falling back to image-name stop...
taskkill /F /IM animate-server.exe >nul 2>nul
if %ERRORLEVEL% EQU 0 (
    echo Server stopped.
) else (
    echo No running animate-server.exe process was found.
)
pause
