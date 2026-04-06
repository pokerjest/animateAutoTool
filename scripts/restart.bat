@echo off
cd /d "%~dp0.."

echo Stopping animate-server.exe...
taskkill /F /IM animate-server.exe >nul 2>nul
if exist "bin\server.pid" del "bin\server.pid"

echo Cleaning bin directory...
if exist "bin\animate-server.exe" del "bin\animate-server.exe"

echo.
echo Rebuilding...
call :build
if %ERRORLEVEL% neq 0 (
    echo Build failed.
    pause
    exit /b 1
)

echo.
echo Starting background process...
start /B "" "bin\animate-server.exe" > "logs\server.log" 2>&1

timeout /t 5 >nul
echo Done. Logs at logs\server.log
pause
exit /b 0

:build
    set "BUILD_VERSION=dev"
    if exist "VERSION" (
        set /p BUILD_VERSION=<VERSION
    )
    go build -ldflags="-s -w -X github.com/pokerjest/animateAutoTool/internal/version.AppVersion=%BUILD_VERSION%" -o "bin\animate-server.exe" "cmd/server/main.go"
    exit /b %ERRORLEVEL%
