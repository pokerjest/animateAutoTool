@echo off
setlocal
cd /d "%~dp0"
if not exist "%CD%\config.yaml" if exist "%CD%\config.yaml.example" (
    copy /Y "%CD%\config.yaml.example" "%CD%\config.yaml" >nul
)
if not exist "%CD%\config.yaml" (
    echo config.yaml not found.
    pause
    exit /b 1
)
start "" notepad "%CD%\config.yaml"
exit /b 0
