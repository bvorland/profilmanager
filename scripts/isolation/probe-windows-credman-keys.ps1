#!/usr/bin/env pwsh
# probe-windows-credman-keys.ps1
#
# Descriptive (isolated=null). Lists Windows Credential Manager generic entries
# whose target name matches azure/azd/microsoft patterns. These are per-user,
# not per AZURE_CONFIG_DIR — any entry persisted here is a shared identity surface.
#
# Uses 'cmdkey /list' (built-in, no native interop required). The credential
# values are never read — only target names are recorded.

. $PSScriptRoot/_common.ps1

$probeId    = 'windows-credman-keys'
$hypothesis = 'Windows Credential Manager stores az/azd refresh tokens or device codes under per-user target names, ignoring AZURE_CONFIG_DIR'
$expected   = 'descriptive — enumerate target names that match azure/azd/microsoft patterns'

if (-not $IsWindows) {
    Write-ProbeResult -Result (New-ProbeResult `
        -Test $probeId -Tool 'windows' -Category 'credman' `
        -Hypothesis $hypothesis -Expected $expected `
        -Skipped $true -SkipReason 'not running on Windows')
    exit 0
}

if (-not (Test-CommandAvailable 'cmdkey')) {
    Write-ProbeResult -Result (New-ProbeResult `
        -Test $probeId -Tool 'windows' -Category 'credman' `
        -Hypothesis $hypothesis -Expected $expected `
        -Skipped $true -SkipReason 'cmdkey not found on PATH (unexpected on Windows)')
    exit 0
}

$run = Invoke-ProbeCommand -Command 'cmdkey' -Arguments @('/list') -TimeoutSeconds 15

$targets = @()
$pattern = '(?i)(microsoftazure|azure-cli|msa\.|\.microsoft\.com|azd[-_]|onedrive|live\.com|\.live)'
if ($run.exit_code -eq 0 -and -not [string]::IsNullOrEmpty($run.stdout)) {
    foreach ($line in ($run.stdout -split "`r?`n")) {
        if ($line -match '^\s*Target:\s*(.+)$') {
            $t = $Matches[1].Trim()
            if ($t -match $pattern) {
                $targets += $t
            }
        }
    }
}

$notes = @()
if ($targets.Count -gt 0) {
    $notes += "Credential Manager has $($targets.Count) entries that may be relevant to az/azd identity. These are per-USER, not per-AZURE_CONFIG_DIR."
    $notes += "If pm needs full per-profile token isolation on Windows, it must namespace credential targets per-profile (or wipe them on profile switch)."
} else {
    $notes += 'No az/azd-shaped Credential Manager entries found. Either nothing has authenticated through Credential Manager yet, or the regex missed.'
}

$actual = "matchingTargetCount=$($targets.Count); sample=[$($targets | Select-Object -First 5 | Join-String -Separator ', ')]"

Write-ProbeResult -Result (New-ProbeResult `
    -Test $probeId -Tool 'windows' -Category 'credman' `
    -Hypothesis $hypothesis -Expected $expected `
    -Actual $actual -Isolated $null `
    -Notes $notes -DurationMs $run.duration_ms)

exit 0
