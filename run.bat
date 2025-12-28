@echo off
if not exist "scripts\control.bat" (
    echo Error: scripts\control.bat not found.
    pause
    exit /b 1
)

call scripts\control.bat run
if %ERRORLEVEL% neq 0 (
    echo Failed to start server.
    pause
    exit /b 1
)
pause
