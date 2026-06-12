#!/usr/bin/env pwsh
# probe-azd-version.ps1
#
# Sanity check: 'azd version' does not write to $HOME/.azd when AZD_CONFIG_DIR
# is redirected.

. $PSScriptRoot/_common.ps1

$probeId    = 'azd-version'
$hypothesis = "'azd version' with AZD_CONFIG_DIR=`$T does not write to `$HOME/.azd"
$expected   = "mtime of `$HOME/.azd is unchanged within the call window"

if (-not (Test-CommandAvailable 'azd')) {
    Write-ProbeResult -Result (New-ProbeResult `
        -Test $probeId -Tool 'azd' -Category 'sanity' `
        -Hypothesis $hypothesis -Expected $expected `
        -Skipped $true -SkipReason 'azd CLI not found on PATH')
    exit 0
}

$tempDir = New-ProbeTempDir -ProbeId $probeId
$homeAzd = Get-HomeAzdDir
$mtimeBefore = Get-DirMtime -Path $homeAzd

$run = Invoke-ProbeCommand -Command 'azd' -Arguments @('version','--output','json') `
                           -EnvOverrides @{ 'AZD_CONFIG_DIR' = $tempDir } `
                           -TimeoutSeconds 30

$mtimeAfter = Get-DirMtime -Path $homeAzd
$unchanged = $true
if ($null -ne $mtimeBefore -and $null -ne $mtimeAfter) {
    $unchanged = ($mtimeBefore -eq $mtimeAfter)
}

$notes = @()
if ($run.timed_out) { $notes += 'azd timed out' }

$actual = "azdExit=$($run.exit_code); homeAzdMtimeBefore=$mtimeBefore; homeAzdMtimeAfter=$mtimeAfter; unchanged=$unchanged"

Write-ProbeResult -Result (New-ProbeResult `
    -Test $probeId -Tool 'azd' -Category 'sanity' `
    -Hypothesis $hypothesis -Expected $expected `
    -Actual $actual -Isolated $unchanged `
    -ToolVersion (Get-ToolVersion 'azd') `
    -Notes $notes -DurationMs $run.duration_ms)

Remove-ProbeTempDir -Path $tempDir
exit 0
