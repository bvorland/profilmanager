#!/usr/bin/env pwsh
# probe-windows-wam-presence.ps1
#
# Descriptive (isolated=null). Reports whether %LOCALAPPDATA%\.IdentityService
# (the WAM broker cache) exists and how many files it has. WAM is per-user,
# shared across all AZURE_CONFIG_DIR values.

. $PSScriptRoot/_common.ps1

$probeId    = 'windows-wam-presence'
$hypothesis = 'WAM broker cache at %LOCALAPPDATA%\.IdentityService is shared per-user and ignores AZURE_CONFIG_DIR'
$expected   = 'descriptive — report presence and file count, flag as known leak vector if non-empty and az broker enabled'

if (-not $IsWindows) {
    Write-ProbeResult -Result (New-ProbeResult `
        -Test $probeId -Tool 'windows' -Category 'broker' `
        -Hypothesis $hypothesis -Expected $expected `
        -Skipped $true -SkipReason 'not running on Windows')
    exit 0
}

$idSvc = Join-Path $env:LOCALAPPDATA '.IdentityService'
$exists = Test-Path $idSvc
$fileCount = 0
$sample = @()
if ($exists) {
    $files = @(Get-ChildItem $idSvc -Recurse -File -ErrorAction SilentlyContinue)
    $fileCount = $files.Count
    $sample = @($files | Select-Object -First 5 | ForEach-Object { $_.FullName.Substring($idSvc.Length).TrimStart('\') })
}

$notes = @()
if ($exists -and $fileCount -gt 0) {
    $notes += "WAM cache is populated. If az/azd auth uses broker on this machine, tokens are SHARED per-user — AZURE_CONFIG_DIR isolation does NOT cover identity."
    $notes += "Mitigation: 'az config set core.enable_broker_on_windows=false' before 'az login' in each profile's AZURE_CONFIG_DIR."
} elseif ($exists) {
    $notes += 'WAM cache dir exists but is empty.'
} else {
    $notes += 'WAM cache dir does not exist; broker likely never used on this account.'
}

$actual = "path='$idSvc'; exists=$exists; fileCount=$fileCount; sample=[$($sample -join ', ')]"

Write-ProbeResult -Result (New-ProbeResult `
    -Test $probeId -Tool 'windows' -Category 'broker' `
    -Hypothesis $hypothesis -Expected $expected `
    -Actual $actual -Isolated $null `
    -Notes $notes)

exit 0
