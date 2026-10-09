#Requires -Version 5.1
# restart-pm2.ps1 stops the CLIProxyAPI service, rebuilds the native binary, and restarts it under PM2.
[CmdletBinding()]
param(
    [string]$AppName = "cli-proxy-api",
    [string]$EcosystemFile = (Join-Path $PSScriptRoot "ecosystem.config.cjs"),
    [string]$OutputPath = (Join-Path $PSScriptRoot "cli-proxy-api.exe"),
    [string]$ConfigPath = (Join-Path $PSScriptRoot "config.yaml"),
    [string]$GoOS = "",
    [string]$GoArch = "",
    [int]$HealthTimeoutSeconds = 30
)

$ErrorActionPreference = "Stop"
$projectRoot = $PSScriptRoot
$outputName = [System.IO.Path]::GetFileName($OutputPath)
$tempPath = "$OutputPath.tmp"

function Get-ServicePort([string]$ConfigFile) {
    $port = 8317
    if (-not (Test-Path -LiteralPath $ConfigFile)) {
        return $port
    }

    $inServerBlock = $false
    foreach ($line in Get-Content -LiteralPath $ConfigFile) {
        # A new top-level key ends the "server:" block.
        if ($line -match '^[^\s#][^:]*:') {
            $inServerBlock = ($line -match '^server\s*:')
            continue
        }

        if ($inServerBlock -and $line -match '^\s+port\s*:\s*"?([0-9]+)"?\s*(?:#.*)?$') {
            return [int]$Matches[1]
        }
    }
    return $port
}

function Get-EcosystemBinaryName([string]$EcosystemPath) {
    # The ecosystem file derives the binary name from the PM2 host platform.
    $isWindowsHost = $true
    if (Get-Variable -Name IsWindows -Scope Global -ErrorAction SilentlyContinue) {
        $isWindowsHost = [bool]$IsWindows
    } elseif ([System.Environment]::OSVersion.Platform -eq [System.PlatformID]::Unix) {
        $isWindowsHost = $false
    }

    if ($isWindowsHost) {
        return "cli-proxy-api.exe"
    }
    return "cli-proxy-api"
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

function Remove-FileWithRetry([string]$Path, [int]$Attempts = 10) {
    for ($i = 1; $i -le $Attempts; $i++) {
        try {
            Remove-Item -LiteralPath $Path -Force -ErrorAction Stop
            return
        } catch {
            if ($i -eq $Attempts) {
                throw
            }
            Start-Sleep -Milliseconds 300
        }
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

$crossCompiling = -not [string]::IsNullOrWhiteSpace($GoOS) -or -not [string]::IsNullOrWhiteSpace($GoArch)
$ecosystemBinary = Get-EcosystemBinaryName $EcosystemFile
if (-not $crossCompiling -and $outputName -ne $ecosystemBinary) {
    throw "OutputPath '$outputName' does not match the binary '$ecosystemBinary' expected by $EcosystemFile. Pass a matching -OutputPath or cross-compile explicitly with -GoOS/-GoArch."
}

$wasRegistered = Stop-RegisteredProcess $AppName
Stop-StandaloneProcess

if (Test-Path -LiteralPath $tempPath) {
    Remove-FileWithRetry $tempPath
}

$metadata = Get-BuildMetadata
$ldFlags = "-s -w -X main.Version=$($metadata.Version) -X main.Commit=$($metadata.Commit) -X main.BuildDate=$($metadata.BuildDate)"
$env:CGO_ENABLED = "1"

Write-Host "Building $outputName $($metadata.Version) ($($metadata.Commit))"
if (-not [string]::IsNullOrWhiteSpace($GoOS)) { $env:GOOS = $GoOS }
if (-not [string]::IsNullOrWhiteSpace($GoArch)) { $env:GOARCH = $GoArch }
$buildArgs = @("build", "-trimpath", "-ldflags", $ldFlags, "-o", $tempPath, "./cmd/server")
& go @buildArgs
$buildExitCode = $LASTEXITCODE
if ($buildExitCode -ne 0) {
    if ($wasRegistered) {
        Write-Warning "Build failed; restarting the previous PM2 process"
        & pm2 restart $AppName --update-env
    } else {
        Write-Warning "Build failed; no previous PM2 process to restore. The service is now stopped."
    }
    throw "Go build failed with exit code $buildExitCode"
}

# Replace the binary in place. Retry because a just-stopped process may still hold the file briefly.
if (Test-Path -LiteralPath $OutputPath) {
    Remove-FileWithRetry $OutputPath
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
$healthUrl = "http://127.0.0.1:$port/healthz"
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