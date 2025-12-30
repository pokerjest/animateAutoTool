@echo off
cd /d "%~dp0"
if exist bin\animate-server.exe (
    echo Starting animate-server...
    start /B bin\animate-server.exe > logs\server.log 2>&1
    echo Server started in background. Logs are in logs\server.log
) else (
    echo Error: bin\animate-server.exe not found!
    pause
)
