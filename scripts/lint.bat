@echo off
setlocal

set "ROOT=%~dp0.."
set "LINTER=%ROOT%\.tools\bin\golangci-lint.exe"

if not exist "%LINTER%" (
    echo golangci-lint not found at "%LINTER%"
    echo Install it first with:
    echo   go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.11.4
    exit /b 1
)

set "GOTOOLCHAIN=local"
set "GOTELEMETRY=off"
set "GOMODCACHE=%ROOT%\.gomodcache"
set "GOCACHE=%ROOT%\.gocache"
set "GOLANGCI_LINT_CACHE=%ROOT%\.lintcache"

pushd "%ROOT%" >nul
if "%~1"=="" (
    "%LINTER%" run --timeout=5m
) else (
    "%LINTER%" %*
)
set "EXITCODE=%ERRORLEVEL%"
popd >nul

exit /b %EXITCODE%
