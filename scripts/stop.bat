@echo off
cd /d "%~dp0.."
if not exist "scripts\control.bat" (
    echo Error: scripts\control.bat not found.
    pause
    exit /b 1
)

echo Stopping Animate Auto Tool...
call scripts\control.bat stop
pause
