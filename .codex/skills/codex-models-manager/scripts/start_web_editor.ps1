param (
    [int]$Port = 8388,
    [switch]$NoBrowser
)

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$Script = Join-Path $ScriptDir "launch.py"

$ArgsList = @($Script, "--port", $Port)
if ($NoBrowser) {
    $ArgsList += "--no-browser"
}

python @ArgsList
