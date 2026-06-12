#!/usr/bin/env pwsh
# probe-azd-config-dir.ps1
#
# Hypothesis: AZD_CONFIG_DIR=$T causes 'azd' to read/write only under $T.

. $PSScriptRoot/_common.ps1

$probeId    = 'azd-config-dir'
$hypothesis = 'AZD_CONFIG_DIR=$T fully isolates azd config writes under $T'
$expected   = "After 'azd config list' with AZD_CONFIG_DIR=`$T, `$T contains azd state and `$HOME/.azd mtime is unchanged"

if (-not (Test-CommandAvailable 'azd')) {
    Write-ProbeResult -Result (New-ProbeResult `
        -Test $probeId -Tool 'azd' -Category 'config-dir' `
        -Hypothesis $hypothesis -Expected $expected `
        -Skipped $true -SkipReason 'azd CLI not found on PATH')
    exit 0
}

$tempDir = New-ProbeTempDir -ProbeId $probeId
$homeAzd = Get-HomeAzdDir
$homeMtimeBefore = Get-DirMtime -Path $homeAzd
$tempBefore = @(Get-DirSnapshot -Path $tempDir)

$run = Invoke-ProbeCommand -Command 'azd' -Arguments @('config','list','--output','json') `
                           -EnvOverrides @{ 'AZD_CONFIG_DIR' = $tempDir } `
                           -TimeoutSeconds 30

$homeMtimeAfter = Get-DirMtime -Path $homeAzd
$tempAfter = @(Get-DirSnapshot -Path $tempDir)
$tempPopulated = ($tempAfter.Count -gt $tempBefore.Count)
$homeUnchanged = $true
if ($null -ne $homeMtimeBefore -and $null -ne $homeMtimeAfter) {
    $homeUnchanged = ($homeMtimeBefore -eq $homeMtimeAfter)
}

$tempFiles = @($tempAfter | ForEach-Object { $_.name }) -join ', '

$notes = @()
if ($run.timed_out) { $notes += 'azd timed out after 30s' }
if ($run.exit_code -ne 0 -and -not $run.timed_out) {
    $notes += "azd exited $($run.exit_code); stderr=$(($run.stderr -split "`n")[0])"
}
if (-not $tempPopulated) {
    $notes += "`$T was not populated by 'azd config list'. 'azd config list' may be read-only on this version; consider 'azd config set core.alpha.fooBar on' to force a write in a follow-up live test."
}

$isolated = $tempPopulated -and $homeUnchanged

$actual = "tempDir='$tempDir'; tempFilesAfter=[$tempFiles]; homeAzdMtimeUnchanged=$homeUnchanged; azdExit=$($run.exit_code)"

Write-ProbeResult -Result (New-ProbeResult `
    -Test $probeId -Tool 'azd' -Category 'config-dir' `
    -Hypothesis $hypothesis -Expected $expected `
    -Actual $actual -Isolated $isolated `
    -ToolVersion (Get-ToolVersion 'azd') `
    -Notes $notes -DurationMs $run.duration_ms)

Remove-ProbeTempDir -Path $tempDir
exit 0
