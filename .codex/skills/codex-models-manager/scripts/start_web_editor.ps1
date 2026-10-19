param (
    [int]$Port = 8388,
    [switch]$NoBrowser
)

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$Script = Join-Path $ScriptDir "web_editor.py"

$ArgsList = @($Script, "--port", $Port)
if ($NoBrowser) {
    $ArgsList += "--no-browser"
}

Write-Host "[Codex Model Catalog] Starting Visual Web Editor on port $Port..." -ForegroundColor Cyan
python @ArgsList
