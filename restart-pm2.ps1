#Requires -Version 5.1
# restart-pm2.ps1 stops the CLIProxyAPI service, rebuilds the Windows binary, and restarts it under PM2.
[CmdletBinding()]
param(
    [string]$AppName = "cli-proxy-api",
    [string]$EcosystemFile = (Join-Path $PSScriptRoot "ecosystem.config.cjs"),
    [string]$OutputPath = (Join-Path $PSScriptRoot "cli-proxy-api.exe"),
    [string]$ConfigPath = (Join-Path $PSScriptRoot "config.yaml"),
    [int]$HealthTimeoutSeconds = 30
)

$ErrorActionPreference = "Stop"
$projectRoot = $PSScriptRoot
$outputName = [System.IO.Path]::GetFileName($OutputPath)
$tempPath = "$OutputPath.tmp"

function Join-PathRoot([string]$Path) {
    return [System.IO.Path]::GetFullPath((Join-Path $projectRoot $Path))
}

function Get-ServicePort([string]$ConfigFile) {
    $port = 8317
    if (Test-Path -LiteralPath $ConfigFile) {
        foreach ($line in Get-Content -LiteralPath $ConfigFile) {
            if ($line -match '^\s*port:\s*([0-9]+)\s*(?:#.*)?$') {
                $port = [int]$Matches[1]
                break
            }
        }
    }
    return $port
}

function Stop-RegisteredProcess([string]$Name) {
    if (-not (Get-Command pm2 -ErrorAction SilentlyContinue)) {
        return $false
    }

    & pm2 describe $Name *> $null
    $registered = ($LASTEXITCODE -eq 0)
    if ($registered) {
        Write-Host "Stopping PM2 process: $Name"
        & pm2 stop $Name
        if ($LASTEXITCODE -ne 0) {
            throw "Failed to stop PM2 process: $Name"
        }
    }
    return $registered
}

function Stop-StandaloneProcess {
    $processName = [System.IO.Path]::GetFileNameWithoutExtension($OutputPath)
    $processes = @(Get-Process -Name $processName -ErrorAction SilentlyContinue)
    foreach ($process in $processes) {
        $processPath = $process.Path
        if ([string]::IsNullOrWhiteSpace($processPath)) {
            continue
        }

        $fullPath = [System.IO.Path]::GetFullPath($processPath)
        $rootPath = [System.IO.Path]::GetFullPath($projectRoot)
        if (-not $fullPath.StartsWith($rootPath, [System.StringComparison]::OrdinalIgnoreCase)) {
            continue
        }

        Write-Host "Stopping standalone process: $fullPath (PID $($process.Id))"
        Stop-Process -Id $process.Id -Force
        Wait-Process -Id $process.Id -ErrorAction SilentlyContinue
    }
}

function Get-BuildMetadata {
    $version = "dev"
    $commit = "none"
    $buildDate = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")

    if (Get-Command git -ErrorAction SilentlyContinue) {
        $versionOutput = & git describe --tags --always 2>$null
        if ($LASTEXITCODE -eq 0 -and $versionOutput) {
            $version = ($versionOutput | Select-Object -First 1).Trim()
        }

        $commitOutput = & git rev-parse --short HEAD 2>$null
        if ($LASTEXITCODE -eq 0 -and $commitOutput) {
            $commit = ($commitOutput | Select-Object -First 1).Trim()
        }
    }

    return @{
        Version = $version
        Commit = $commit
        BuildDate = $buildDate
    }
}

if (-not (Test-Path -LiteralPath $EcosystemFile)) {
    throw "PM2 ecosystem file not found: $EcosystemFile"
}
if (-not (Test-Path -LiteralPath $ConfigPath)) {
    throw "Config file not found: $ConfigPath"
}
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    throw "Go is not available in PATH"
}
if (-not (Get-Command pm2 -ErrorAction SilentlyContinue)) {
    throw "PM2 is not available in PATH"
}

$wasRegistered = Stop-RegisteredProcess $AppName
Stop-StandaloneProcess

if (Test-Path -LiteralPath $tempPath) {
    Remove-Item -LiteralPath $tempPath -Force
}

$metadata = Get-BuildMetadata
$ldFlags = "-s -w -X main.Version=$($metadata.Version) -X main.Commit=$($metadata.Commit) -X main.BuildDate=$($metadata.BuildDate)"
$env:CGO_ENABLED = "1"

Write-Host "Building $outputName $($metadata.Version) ($($metadata.Commit))"
& go build -trimpath -ldflags $ldFlags -o $tempPath ./cmd/server
if ($LASTEXITCODE -ne 0) {
    if ($wasRegistered) {
        Write-Warning "Build failed; restarting the previous PM2 process"
        & pm2 restart $AppName --update-env
    }
    throw "Go build failed with exit code $LASTEXITCODE"
}

Move-Item -LiteralPath $tempPath -Destination $OutputPath -Force

Write-Host "Starting PM2 process: $AppName"
if ($wasRegistered) {
    & pm2 restart $AppName --update-env
} else {
    & pm2 start $EcosystemFile --only $AppName
}
if ($LASTEXITCODE -ne 0) {
    throw "Failed to start PM2 process: $AppName"
}

$port = Get-ServicePort $ConfigPath
$healthUrl = "http://127.0.0.1:$port/"
$deadline = (Get-Date).AddSeconds($HealthTimeoutSeconds)
$healthy = $false
while ((Get-Date) -lt $deadline) {
    try {
        $response = Invoke-WebRequest -Uri $healthUrl -UseBasicParsing -TimeoutSec 3
        if ($response.StatusCode -eq 200) {
            $healthy = $true
            break
        }
    } catch {
        Start-Sleep -Milliseconds 500
    }
}

& pm2 list
if ($healthy) {
    Write-Host "Service is healthy: $healthUrl"
} else {
    Write-Warning "Service did not become healthy within $HealthTimeoutSeconds seconds. Check: pm2 logs $AppName"
    exit 1
}
