@echo off
setlocal
cd /d "%~dp0"

call stop.bat
echo.
call start.bat
pause
exit /b %ERRORLEVEL%
