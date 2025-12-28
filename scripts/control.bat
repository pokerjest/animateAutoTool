@echo off
setlocal

:: 配置
set "APP_NAME=animate-server.exe"
set "BIN_DIR=bin"
set "BIN_PATH=%BIN_DIR%\%APP_NAME%"
set "PID_FILE=%BIN_DIR%\server.pid"
set "LOG_FILE=server.log"
set "SRC_PATH=cmd/server/main.go"

if not exist "%BIN_DIR%" mkdir "%BIN_DIR%"

:: 入口处理
if "%1"=="" goto help
if "%1"=="build" goto build
if "%1"=="start" goto start
if "%1"=="stop" goto stop
if "%1"=="restart" goto restart
if "%1"=="status" goto status
goto help

:check_deps
    where go >nul 2>nul
    if %ERRORLEVEL% neq 0 (
        echo Error: 'go' is not installed or not in your PATH.
        echo Please install Go 1.24+ from https://go.dev/dl/
        exit /b 1
    )
    
    :: Simple version check is hard in batch, skipping specific version check for now
    :: Assuming user has a recent enough Go version
    exit /b 0

:build
    call :check_deps
    if %ERRORLEVEL% neq 0 exit /b 1
    
    echo Building %APP_NAME%...
    set CGO_ENABLED=0
    go build -ldflags="-s -w" -o "%BIN_PATH%" "%SRC_PATH%"
    if %ERRORLEVEL% neq 0 (
        echo Build failed!
        echo If you are in China, you may need to set GOPROXY:
        echo set GOPROXY=https://goproxy.cn,direct
        exit /b 1
    )
    echo Build successful.
    exit /b 0

:start
    if exist "%PID_FILE%" (
        set /p PID=<"%PID_FILE%"
        tasklist /FI "PID eq %PID%" | find "%PID%" >nul
        if not errorlevel 1 (
            echo %APP_NAME% is already running (PID: %PID%).
            exit /b 0
        ) else (
            echo Found stale PID file. Removing...
            del "%PID_FILE%"
        )
    )

    call :build
    if %ERRORLEVEL% neq 0 exit /b 1

    echo Starting %APP_NAME%...
    :: Start in background
    start /B "" "%BIN_PATH%" > "%LOG_FILE%" 2>&1
    
    :: Get PID (approximate method for batch)
    :: This is tricky in pure batch. simplified: we just rely on the user or look up by name later.
    :: For now, let's try to find the PID we just started.
    :: A reliable way is hard without external tools.
    :: Alternative: use wmic to get process id of the executable.
    
    timeout /t 1 >nul
    for /f "tokens=2" %%a in ('tasklist /nh /fi "imagename eq %APP_NAME%"') do set PID=%%a
    
    if defined PID (
        echo %PID% > "%PID_FILE%"
        echo Started with PID %PID%. Logs are redirected to %LOG_FILE%.
    ) else (
        echo Started, but could not capture PID. Check %LOG_FILE% for details.
    )
    exit /b 0

:stop
    if not exist "%PID_FILE%" (
        echo %APP_NAME% is not running (PID file not found).
        :: Try killing by name just in case
        taskkill /F /IM "%APP_NAME%" >nul 2>nul
        if not errorlevel 1 echo Stopped instance found by name.
        exit /b 0
    )

    set /p PID=<"%PID_FILE%"
    echo Stopping %APP_NAME% (PID: %PID%)...
    taskkill /F /PID %PID% >nul 2>nul
    if %ERRORLEVEL% equ 0 (
        del "%PID_FILE%"
        echo Stopped.
    ) else (
        echo Process %PID% not found. Removing stale PID file.
        del "%PID_FILE%"
    )
    exit /b 0

:restart
    call :stop
    call :start
    exit /b 0

:status
    if exist "%PID_FILE%" (
        set /p PID=<"%PID_FILE%"
        tasklist /FI "PID eq %PID%" | find "%PID%" >nul
        if not errorlevel 1 (
            echo %APP_NAME% is running (PID: %PID%).
        ) else (
            echo %APP_NAME% is not running (Stale PID file).
        )
    ) else (
        echo No PID file found.
    )
    exit /b 0

:help
    echo Usage: %0 {build|start|stop|restart|status}
    exit /b 1
