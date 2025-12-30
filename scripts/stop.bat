@echo off
cd /d "%~dp0"
echo Stopping animate-server...
taskkill /F /IM animate-server.exe
echo Server stopped.
pause
