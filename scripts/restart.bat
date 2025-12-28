@echo off
cd /d "%~dp0.."
echo ==========================================
echo       Animate Auto Tool - Restart
echo ==========================================
echo.
echo Stopping execute...
call scripts\control.bat stop
echo Starting execute...
call scripts\control.bat run
if %ERRORLEVEL% neq 0 (
    echo Restart failed or cancelled.
    pause
    exit /b 1
)
pause
