#!/usr/bin/env powershell
# Probe CodeBuddy CLI availability for one or more model IDs.
# Usage: probe_codebuddy_models.ps1 -CodeBuddy <path-to-codebuddy> -Ids id1,id2
param(
  [Parameter(Mandatory = $true)][string]$CodeBuddy,
  [Parameter(Mandatory = $true)][string]$Ids
)

$ErrorActionPreference = "Continue"
$root = if ($CodeBuddy.EndsWith("codebuddy")) { Split-Path -Parent (Split-Path -Parent $CodeBuddy) } else { Split-Path -Parent $CodeBuddy }
$results = @()
foreach ($id in ($Ids -split "," | ForEach-Object { $_.Trim() } | Where-Object { $_ })) {
  $output = & node $CodeBuddy --model $id -p "Say OK only." --output-format text --dangerously-skip-permissions 2>&1
  $text = ($output | Out-String)
  $available = $text -match "(?m)^OK\s*$" -or $text -match "\bOK\b"
  $unavailable = $text -match "service info not found"
  $results += [pscustomobject]@{
    id = $id
    available = ($available -and -not $unavailable)
    unavailable_error = $unavailable
    output = $text.Trim()
  }
}
$results | ConvertTo-Json -Depth 3
