#!/usr/bin/env pwsh
# probe-az-version.ps1
#
# Sanity check: 'az --version' itself does not write to $HOME/.azure when
# AZURE_CONFIG_DIR is redirected. If 'az --version' touches $HOME/.azure
# regardless, our profile-switch model is broken from step zero.

. $PSScriptRoot/_common.ps1

$probeId    = 'az-version'
$hypothesis = "'az --version' with AZURE_CONFIG_DIR=`$T does not write to `$HOME/.azure"
$expected   = "After 'az --version', mtime of `$HOME/.azure is unchanged within the call window"

if (-not (Test-CommandAvailable 'az')) {
    Write-ProbeResult -Result (New-ProbeResult `
        -Test $probeId -Tool 'az' -Category 'sanity' `
        -Hypothesis $hypothesis -Expected $expected `
        -Skipped $true -SkipReason 'az CLI not found on PATH')
    exit 0
}

$tempDir = New-ProbeTempDir -ProbeId $probeId
$homeAzure = Get-HomeAzureDir
$homeMtimeBefore = Get-DirMtime -Path $homeAzure

$run = Invoke-ProbeCommand -Command 'az' -Arguments @('--version') `
                           -EnvOverrides @{ 'AZURE_CONFIG_DIR' = $tempDir } `
                           -TimeoutSeconds 30

$homeMtimeAfter = Get-DirMtime -Path $homeAzure
$homeUnchanged = $true
if ($null -ne $homeMtimeBefore -and $null -ne $homeMtimeAfter) {
    $homeUnchanged = ($homeMtimeBefore -eq $homeMtimeAfter)
}

$notes = @()
if ($run.timed_out) { $notes += "az timed out after 30s" }

$actual = "azExit=$($run.exit_code); homeAzureMtimeBefore=$homeMtimeBefore; homeAzureMtimeAfter=$homeMtimeAfter; unchanged=$homeUnchanged"

Write-ProbeResult -Result (New-ProbeResult `
    -Test $probeId -Tool 'az' -Category 'sanity' `
    -Hypothesis $hypothesis -Expected $expected `
    -Actual $actual -Isolated $homeUnchanged `
    -ToolVersion (Get-ToolVersion 'az') `
    -Notes $notes -DurationMs $run.duration_ms)

Remove-ProbeTempDir -Path $tempDir
exit 0
