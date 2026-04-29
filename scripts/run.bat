@echo off
setlocal
cd /d "%~dp0"
if exist bin\animate-server.exe (
    echo Starting animate-server in foreground...
    echo Press Ctrl+C to stop.
    bin\animate-server.exe
) else (
    echo Error: bin\animate-server.exe not found!
    echo Tip: build or unpack the release package first.
    pause
    exit /b 1
)
