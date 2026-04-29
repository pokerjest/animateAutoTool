@echo off
setlocal
cd /d "%~dp0"
set "APP_EXE=%CD%\bin\animate-server.exe"
set "LOG_DIR=%CD%\logs"
set "LOG_FILE=%LOG_DIR%\server.log"
set "PID_FILE=%LOG_DIR%\animate-server.pid"

if not exist "%CD%\config.yaml" if exist "%CD%\config.yaml.example" (
    copy /Y "%CD%\config.yaml.example" "%CD%\config.yaml" >nul
    echo Created config.yaml from config.yaml.example
    echo You can edit it later if needed.
    echo.
)

if not exist "%APP_EXE%" (
    echo Error: %APP_EXE% not found.
    echo Tip: build or unpack the release package first.
    pause
    exit /b 1
)

if not exist "%LOG_DIR%" mkdir "%LOG_DIR%" >nul 2>nul

echo %CD% | find /I "Program Files" >nul
if %ERRORLEVEL% EQU 0 (
    echo Warning: this app is running from Program Files.
    echo On Windows, Program Files is often read-only for normal launches.
    echo Recommend moving the app to a writable folder like D:\Apps\AnimateAutoTool or %%USERPROFILE%%\Apps\AnimateAutoTool.
    echo.
)

if exist "%PID_FILE%" (
    set /p EXISTING_PID=<"%PID_FILE%"
    if not "%EXISTING_PID%"=="" (
        powershell -NoProfile -NonInteractive -Command "if (Get-Process -Id %EXISTING_PID% -ErrorAction SilentlyContinue) { exit 0 } else { exit 1 }"
        if %ERRORLEVEL% EQU 0 (
            echo Animate Auto Tool already appears to be running. PID=%EXISTING_PID%
            echo If the app is unresponsive, run stop.bat first.
            pause
            exit /b 0
        )
    ) else (
        echo Existing PID file was empty. Cleaning stale PID file.
    )
    del "%PID_FILE%" >nul 2>nul
)

echo Starting Animate Auto Tool...
powershell -NoProfile -NonInteractive -Command "$psi = New-Object System.Diagnostics.ProcessStartInfo; $psi.FileName = '%APP_EXE%'; $psi.WorkingDirectory = '%CD%'; $psi.UseShellExecute = $true; $psi.WindowStyle = [System.Diagnostics.ProcessWindowStyle]::Hidden; $p = [System.Diagnostics.Process]::Start($psi); if ($null -eq $p) { exit 1 }; Set-Content -LiteralPath '%PID_FILE%' -Value $p.Id -Encoding ascii"
if %ERRORLEVEL% NEQ 0 (
    echo Failed to start animate-server.exe
    echo Check write permissions for:
    echo   %LOG_DIR%
    pause
    exit /b 1
)

powershell -NoProfile -NonInteractive -Command "$pidValue = (Get-Content -LiteralPath '%PID_FILE%' -ErrorAction SilentlyContinue | Select-Object -First 1).Trim(); Start-Sleep -Seconds 2; if ($pidValue -and (Get-Process -Id $pidValue -ErrorAction SilentlyContinue)) { exit 0 } else { exit 1 }"
if %ERRORLEVEL% NEQ 0 (
    echo Animate Auto Tool exited during startup.
    echo Main log: %LOG_FILE%
    del "%PID_FILE%" >nul 2>nul
    pause
    exit /b 1
)

echo Started in background.
echo Logs: %LOG_FILE%
echo PID file: %PID_FILE%
echo Open UI: run open-ui.bat
exit /b 0
