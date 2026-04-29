@echo off
setlocal
cd /d "%~dp0"
if exist "%CD%\config.yaml" (
    echo config.yaml already exists.
    exit /b 0
)
if not exist "%CD%\config.yaml.example" (
    echo config.yaml.example not found.
    pause
    exit /b 1
)
copy /Y "%CD%\config.yaml.example" "%CD%\config.yaml" >nul
echo Created config.yaml
start "" notepad "%CD%\config.yaml"
exit /b 0
