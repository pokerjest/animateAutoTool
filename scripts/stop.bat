@echo off
setlocal
cd /d "%~dp0"
set "PID_FILE=%CD%\bin\server.pid"

if exist "%PID_FILE%" (
    set /p APP_PID=<"%PID_FILE%"
    if not "%APP_PID%"=="" (
        echo Stopping Animate Auto Tool PID=%APP_PID%...
        powershell -NoProfile -NonInteractive -Command "Stop-Process -Id %APP_PID% -Force -ErrorAction Stop" >nul 2>nul
        if %ERRORLEVEL% EQU 0 (
            del "%PID_FILE%" >nul 2>nul
            echo Server stopped.
            exit /b 0
        )
        echo Stored PID was not running. Cleaning stale PID file.
        del "%PID_FILE%" >nul 2>nul
    ) else (
        echo PID file was empty. Cleaning stale PID file.
        del "%PID_FILE%" >nul 2>nul
    )
)

echo No running Animate Auto Tool process was found from %PID_FILE%.
echo If the server was started outside start.bat, stop that process manually.
exit /b 0
