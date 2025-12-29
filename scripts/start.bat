@echo off
cd /d "%~dp0.."
if not exist "bin" mkdir "bin"
if not exist "logs" mkdir "logs"

echo Building...
go build -ldflags="-s -w" -o "bin\animate-server.exe" "cmd/server/main.go"
if %ERRORLEVEL% neq 0 (
    echo Build failed.
    pause
    exit /b 1
)

echo Starting background process...
echo Logs will be written to logs\server.log
start /B "" "bin\animate-server.exe" > "logs\server.log" 2>&1

timeout /t 2 >nul
echo Done.
pause
