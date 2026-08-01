@echo off
setlocal EnableExtensions EnableDelayedExpansion
cd /d "%~dp0"

call stop.bat
set "STOP_RESULT=!ERRORLEVEL!"
if not "!STOP_RESULT!"=="0" (
    echo.
    echo Restart aborted because the server could not be stopped safely.
    pause
    exit /b !STOP_RESULT!
)

echo.
call start.bat
set "START_RESULT=!ERRORLEVEL!"
pause
exit /b !START_RESULT!
