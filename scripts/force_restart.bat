@echo off
cd /d "%~dp0.."
echo Force stopping animate-server.exe...
taskkill /F /IM animate-server.exe
timeout /t 2 >nul
echo Starting...
call scripts\control.bat run
