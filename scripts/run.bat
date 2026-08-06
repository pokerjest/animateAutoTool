@echo off
setlocal
cd /d "%~dp0"
if exist bin\AnimateAutoTool.exe (
    echo Starting AnimateAutoTool in foreground...
    echo Press Ctrl+C to stop.
    bin\AnimateAutoTool.exe
) else (
    echo Error: bin\AnimateAutoTool.exe not found!
    echo Tip: build or unpack the release package first.
    pause
    exit /b 1
)
