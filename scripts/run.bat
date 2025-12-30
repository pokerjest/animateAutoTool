@echo off
cd /d "%~dp0"
if exist bin\animate-server.exe (
    echo Starting animate-server in foreground...
    bin\animate-server.exe
) else (
    echo Error: bin\animate-server.exe not found!
    pause
)
