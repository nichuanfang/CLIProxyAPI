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
  # Use anchored matches: stderr paths such as "At C:\...\probe.ps1" contain "OK"-like substrings
  # when matched case-insensitively, and every transport error is a failure regardless of wording.
  $available = $text -cmatch "(?m)^OK[.!]?\s*$"
  $unavailable = $text -match "service info not found" -or $text -match "model not allowed" -or $text -match "(?m)^\s*\+?\s*(At |CategoryInfo|FullyQualifiedErrorId|node\.exe :|node :|\+)" 
  $results += [pscustomobject]@{
    id = $id
    available = ($available -and -not $unavailable)
    unavailable_error = $unavailable
    exit_code = $LASTEXITCODE
    output = $text.Trim()
  }
}
$results | ConvertTo-Json -Depth 3
