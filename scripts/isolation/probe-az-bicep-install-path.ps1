#!/usr/bin/env pwsh
# probe-az-bicep-install-path.ps1
#
# Hypothesis: 'az bicep' installs the bicep binary under $AZURE_CONFIG_DIR/bin,
# i.e. per-profile, not into a shared $HOME/.azure/bin.
#
# This probe is read-only: it lists $T/bin and $HOME/.azure/bin without
# triggering an install. The operator can manually run 'az bicep version'
# under a probed env later and re-run the probe.

. $PSScriptRoot/_common.ps1

$probeId    = 'az-bicep-install-path'
$hypothesis = "'bicep' installs under `$AZURE_CONFIG_DIR/bin, not a shared `$HOME/.azure/bin"
$expected   = 'After `az bicep version` under a fresh AZURE_CONFIG_DIR, $T/bin contains bicep(.exe) and $HOME/.azure/bin is unchanged'

if (-not (Test-CommandAvailable 'az')) {
    Write-ProbeResult -Result (New-ProbeResult `
        -Test $probeId -Tool 'az' -Category 'bicep' `
        -Hypothesis $hypothesis -Expected $expected `
        -Skipped $true -SkipReason 'az CLI not found on PATH')
    exit 0
}

$homeBin = Join-Path (Get-HomeAzureDir) 'bin'
$homeBicep = @()
if (Test-Path $homeBin) {
    $homeBicep = @(Get-ChildItem $homeBin -Filter 'bicep*' -ErrorAction SilentlyContinue | ForEach-Object { $_.FullName })
}

$tempDir = New-ProbeTempDir -ProbeId $probeId
$run = Invoke-ProbeCommand -Command 'az' -Arguments @('bicep','version','--output','json') `
                           -EnvOverrides @{ 'AZURE_CONFIG_DIR' = $tempDir } `
                           -TimeoutSeconds 60   # bicep may download on first run

$tempBin = Join-Path $tempDir 'bin'
$tempBicep = @()
if (Test-Path $tempBin) {
    $tempBicep = @(Get-ChildItem $tempBin -Filter 'bicep*' -ErrorAction SilentlyContinue | ForEach-Object { $_.FullName })
}

$installedUnderT = ($tempBicep.Count -gt 0)
$homeUnchangedCount = (@(if (Test-Path $homeBin) { Get-ChildItem $homeBin -Filter 'bicep*' -ErrorAction SilentlyContinue | ForEach-Object { $_.FullName } } else { @() })).Count

$notes = @()
if ($run.timed_out) { $notes += 'az bicep version timed out (may need to download bicep on first run; retry once)' }
if (-not $installedUnderT) {
    $notes += "No bicep binary appeared in `$T/bin. Either 'az bicep' uses a shared path on this version, or bicep is already on PATH and the install was skipped, or 'az bicep version' failed before install."
}
if ($homeBicep.Count -ne $homeUnchangedCount) {
    $notes += "BICEP COUNT IN `$HOME/.azure/bin CHANGED during probe — possible leak"
}

# If az bicep itself failed, we cannot draw an isolation conclusion either way.
$isolated = $null
if ($run.exit_code -eq 0 -and -not $run.timed_out) {
    $isolated = $installedUnderT -and ($homeBicep.Count -eq $homeUnchangedCount)
} else {
    $notes += "az bicep exited $($run.exit_code); install path claim is INCONCLUSIVE on this run. Operator should run 'az bicep install' manually with AZURE_CONFIG_DIR set, then re-run this probe."
}

$actual = "tempBinBicep=$($tempBicep.Count); homeBinBicepBefore=$($homeBicep.Count); homeBinBicepAfter=$homeUnchangedCount; azExit=$($run.exit_code)"

Write-ProbeResult -Result (New-ProbeResult `
    -Test $probeId -Tool 'az' -Category 'bicep' `
    -Hypothesis $hypothesis -Expected $expected `
    -Actual $actual -Isolated $isolated `
    -ToolVersion (Get-ToolVersion 'az') `
    -Notes $notes -DurationMs $run.duration_ms)

Remove-ProbeTempDir -Path $tempDir
exit 0
