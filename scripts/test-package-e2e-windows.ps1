[CmdletBinding()]
param(
    [string]$ArtifactPath = $env:PACKAGE_E2E_ARTIFACT,
    [string]$Version = $env:PACKAGE_E2E_VERSION,
    [int]$Port = 18307,
    [switch]$InstallBrowser
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$rootDir = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$frontendDir = Join-Path $rootDir "web\frontend"
$resultsDir = Join-Path $frontendDir "test-results"
$workDir = Join-Path ([IO.Path]::GetTempPath()) ("AnimateAutoTool package e2e " + [guid]::NewGuid().ToString("N"))
$artifactDir = Join-Path $workDir "artifacts"
$unpackDir = Join-Path $workDir "unpacked"
$startOutputPath = Join-Path $workDir "start-output.txt"
$stopOutputPath = Join-Path $workDir "stop-output.txt"
$packageDir = ""
$serverPID = 0
$succeeded = $false

if ([string]::IsNullOrWhiteSpace($Version)) {
    $Version = (Get-Content -LiteralPath (Join-Path $rootDir "VERSION") -Raw).Trim()
}
if (-not $PSBoundParameters.ContainsKey("Port") -and -not [string]::IsNullOrWhiteSpace($env:PACKAGE_E2E_PORT)) {
    $Port = [int]$env:PACKAGE_E2E_PORT
}
if (-not $InstallBrowser -and $env:PACKAGE_E2E_INSTALL_BROWSER -eq "1") {
    $InstallBrowser = $true
}

function Resolve-GitBash {
    $command = Get-Command bash.exe -ErrorAction SilentlyContinue
    if ($null -ne $command) {
        return $command.Source
    }
    $candidate = "C:\Program Files\Git\bin\bash.exe"
    if (Test-Path -LiteralPath $candidate) {
        return $candidate
    }
    throw "Git Bash is required when PACKAGE_E2E_ARTIFACT is not provided."
}

function Build-WindowsArtifact {
    param([string]$OutputDirectory)

    $bashPath = Resolve-GitBash
    $cygpathPath = Join-Path (Split-Path (Split-Path $bashPath -Parent) -Parent) "usr\bin\cygpath.exe"
    if (-not (Test-Path -LiteralPath $cygpathPath)) {
        throw "cygpath.exe was not found next to Git Bash."
    }
    $packageScript = Join-Path $rootDir "scripts\package.sh"
    $packageScriptUnix = (& $cygpathPath -u $packageScript).Trim()
    $outputDirectoryUnix = (& $cygpathPath -u $OutputDirectory).Trim()

    $env:DIST_DIR = $outputDirectoryUnix
    $env:PACKAGE_TARGETS = "windows/amd64"
    $env:PACKAGE_INCLUDE_ARCHIVES = "1"
    $env:PACKAGE_INCLUDE_WINDOWS_STANDALONE = "0"
    $env:PACKAGE_INCLUDE_DMG = "0"
    & $bashPath $packageScriptUnix $Version | ForEach-Object { Write-Host $_ }
    if ($LASTEXITCODE -ne 0) {
        throw "Windows release packaging failed with exit code $LASTEXITCODE."
    }
    return Join-Path $OutputDirectory "AnimateAutoTool_${Version}_windows_amd64.zip"
}

function Save-Diagnostics {
    if (-not (Test-Path -LiteralPath $resultsDir)) {
        New-Item -ItemType Directory -Path $resultsDir -Force | Out-Null
    }
    foreach ($path in @($startOutputPath, $stopOutputPath)) {
        if (Test-Path -LiteralPath $path) {
            Copy-Item -LiteralPath $path -Destination $resultsDir -Force
        }
    }
    if ($packageDir -ne "") {
        $logDir = Join-Path $packageDir "logs"
        if (Test-Path -LiteralPath $logDir) {
            $diagnosticLogDir = Join-Path $resultsDir "package-e2e-windows-logs"
            Copy-Item -LiteralPath $logDir -Destination $diagnosticLogDir -Recurse -Force
        }
    }
}

function Stop-PackagedServer {
    if ($packageDir -eq "") {
        return
    }
    $pidFile = Join-Path $packageDir "bin\server.pid"
    if (-not (Test-Path -LiteralPath $pidFile)) {
        return
    }
    Push-Location $packageDir
    try {
        $stopOutput = @(& cmd.exe /d /c stop.bat 2>&1)
        $stopOutput | Set-Content -LiteralPath $stopOutputPath -Encoding UTF8
        if ($LASTEXITCODE -ne 0) {
            throw "stop.bat failed with exit code $LASTEXITCODE."
        }
    } finally {
        Pop-Location
    }
}

try {
    New-Item -ItemType Directory -Path $artifactDir, $unpackDir -Force | Out-Null

    if ([string]::IsNullOrWhiteSpace($ArtifactPath)) {
        $ArtifactPath = Build-WindowsArtifact -OutputDirectory $artifactDir
    } elseif (-not [IO.Path]::IsPathRooted($ArtifactPath)) {
        $ArtifactPath = Join-Path $rootDir $ArtifactPath
    }
    if (-not (Test-Path -LiteralPath $ArtifactPath)) {
        throw "Packaged archive not found: $ArtifactPath"
    }

    Expand-Archive -LiteralPath $ArtifactPath -DestinationPath $unpackDir -Force
    $packageDirs = @(Get-ChildItem -LiteralPath $unpackDir -Directory | Where-Object { $_.Name -like "AnimateAutoTool_*" })
    if ($packageDirs.Count -ne 1) {
        throw "The archive must contain exactly one AnimateAutoTool_* release directory."
    }
    $packageDir = $packageDirs[0].FullName

    $requiredPaths = @(
        "bin\AnimateAutoTool.exe",
        "bin\animate-server.exe",
        "config.yaml",
        "config.yaml.example",
        "animate-release-manifest.json",
        "start.bat",
        "stop.bat",
        "run.bat",
        "restart.bat",
        "open-ui.bat",
        "open-data.bat",
        "open-config.bat",
        "view-logs.bat",
        "init-config.bat",
        "WINDOWS_QUICKSTART.txt"
    )
    foreach ($relativePath in $requiredPaths) {
        if (-not (Test-Path -LiteralPath (Join-Path $packageDir $relativePath))) {
            throw "The Windows archive is missing $relativePath."
        }
    }

    $env:ANIME_SERVER_PORT = $Port.ToString()
    $env:ANIME_SERVER_HEADLESS = "true"
    $env:ANIME_MANAGED_SERVICES_DOWNLOAD_MISSING = "false"

    Push-Location $packageDir
    try {
        $startOutput = @(& cmd.exe /d /c start.bat 2>&1)
        $startOutput | Set-Content -LiteralPath $startOutputPath -Encoding UTF8
        if ($LASTEXITCODE -ne 0) {
            throw "start.bat failed with exit code $LASTEXITCODE."
        }
    } finally {
        Pop-Location
    }

    $pidFile = Join-Path $packageDir "bin\server.pid"
    if (-not (Test-Path -LiteralPath $pidFile)) {
        throw "start.bat did not create bin\server.pid."
    }
    $serverPID = [int](Get-Content -LiteralPath $pidFile -Raw).Trim()
    $expectedExecutable = [IO.Path]::GetFullPath((Join-Path $packageDir "bin\AnimateAutoTool.exe"))
    $actualExecutable = [IO.Path]::GetFullPath((Get-Process -Id $serverPID -ErrorAction Stop).Path)
    if (-not [string]::Equals($actualExecutable, $expectedExecutable, [StringComparison]::OrdinalIgnoreCase)) {
        throw "PID file points to $actualExecutable instead of $expectedExecutable."
    }

    $baseURL = "http://127.0.0.1:$Port"
    $deadline = (Get-Date).AddSeconds(60)
    $ready = $false
    while ((Get-Date) -lt $deadline) {
        if ($null -eq (Get-Process -Id $serverPID -ErrorAction SilentlyContinue)) {
            break
        }
        try {
            $response = Invoke-WebRequest -UseBasicParsing -Uri "$baseURL/api/v1/session" -TimeoutSec 2
            if ($response.StatusCode -eq 200) {
                $ready = $true
                break
            }
        } catch {}
        Start-Sleep -Milliseconds 250
    }
    if (-not $ready) {
        throw "Packaged Windows server did not become ready at $baseURL."
    }

    if (-not (Test-Path -LiteralPath (Join-Path $packageDir "data\app.db"))) {
        throw "The packaged server did not create its portable database under data\app.db."
    }
    $hourlyLogs = @(Get-ChildItem -LiteralPath (Join-Path $packageDir "logs") -Filter "server-*.log" -File)
    if ($hourlyLogs.Count -eq 0) {
        throw "The packaged server did not create an hourly server log."
    }

    $playwrightCommand = Join-Path $frontendDir "node_modules\.bin\playwright.cmd"
    if (-not (Test-Path -LiteralPath $playwrightCommand)) {
        & npm.cmd --prefix $frontendDir ci
        if ($LASTEXITCODE -ne 0) {
            throw "npm ci failed with exit code $LASTEXITCODE."
        }
    }
    if ($InstallBrowser) {
        & npm.cmd --prefix $frontendDir exec -- playwright install chromium
        if ($LASTEXITCODE -ne 0) {
            throw "Playwright Chromium installation failed with exit code $LASTEXITCODE."
        }
    }

    $env:PACKAGE_E2E_BASE_URL = $baseURL
    $env:PACKAGE_E2E_VERSION = $Version
    Push-Location $frontendDir
    try {
        & npm.cmd exec -- playwright test --config e2e/playwright.config.ts
        if ($LASTEXITCODE -ne 0) {
            throw "Playwright package E2E failed with exit code $LASTEXITCODE."
        }
    } finally {
        Pop-Location
    }

    Stop-PackagedServer
    $stopText = Get-Content -LiteralPath $stopOutputPath -Raw
    if ($stopText -notmatch "Server stopped gracefully\.") {
        throw "stop.bat did not report a graceful shutdown."
    }
    if (Test-Path -LiteralPath $pidFile) {
        throw "stop.bat did not remove bin\server.pid."
    }
    if ($null -ne (Get-Process -Id $serverPID -ErrorAction SilentlyContinue)) {
        throw "The packaged server is still running after stop.bat."
    }

    $runtimeDir = Join-Path $packageDir "data\runtime"
    if (Test-Path -LiteralPath $runtimeDir) {
        $runtimeMarkers = @(Get-ChildItem -LiteralPath $runtimeDir -Filter "*.json" -File)
        if ($runtimeMarkers.Count -ne 0) {
            throw "Graceful shutdown left runtime journal markers: $($runtimeMarkers.Name -join ', ')"
        }
    }

    $serverPID = 0
    $succeeded = $true
    Write-Output "Packaged Windows release passed start.bat, Playwright, and graceful stop.bat E2E validation."
} finally {
    if (-not $succeeded) {
        try {
            Stop-PackagedServer
        } catch {
            Write-Warning $_
        }
        if ($serverPID -gt 0) {
            $process = Get-Process -Id $serverPID -ErrorAction SilentlyContinue
            if ($null -ne $process) {
                Stop-Process -Id $serverPID -Force -ErrorAction SilentlyContinue
            }
        }
        Save-Diagnostics
    }

    if ($env:PACKAGE_E2E_KEEP_WORKDIR -eq "1") {
        Write-Output "Package E2E work directory retained at $workDir"
    } elseif (Test-Path -LiteralPath $workDir) {
        $resolvedWorkDir = [IO.Path]::GetFullPath($workDir)
        $resolvedTempDir = [IO.Path]::GetFullPath([IO.Path]::GetTempPath())
        if ($resolvedWorkDir.StartsWith($resolvedTempDir, [StringComparison]::OrdinalIgnoreCase)) {
            Remove-Item -LiteralPath $resolvedWorkDir -Recurse -Force
        } else {
            Write-Warning "Refusing to remove unexpected E2E work directory: $resolvedWorkDir"
        }
    }
}
