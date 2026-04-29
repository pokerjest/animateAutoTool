@echo off
setlocal
cd /d "%~dp0"
if not exist "%CD%\data" mkdir "%CD%\data" >nul 2>nul
start "" explorer "%CD%\data"
exit /b 0
