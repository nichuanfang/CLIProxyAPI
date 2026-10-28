param([int]$Port = 8390, [switch]$NoBrowser, [string]$Settings)
$launcherArgs = @((Join-Path $PSScriptRoot 'launch.py'), '--port', "$Port")
if ($NoBrowser) { $launcherArgs += '--no-browser' }
if ($Settings) { $launcherArgs += @('--settings', $Settings) }
python @launcherArgs
exit $LASTEXITCODE
