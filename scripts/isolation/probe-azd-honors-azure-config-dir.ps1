#!/usr/bin/env pwsh
# probe-azd-honors-azure-config-dir.ps1
#
# Descriptive probe (isolated=null). Documents whether azd reacts to
# AZURE_CONFIG_DIR being set — many azd flows shell out to az, and if
# they reset AZURE_CONFIG_DIR before the child az call, isolation breaks.
#
# Dry approach: run 'azd config list' with BOTH env vars set; look at what's
# in $T_azure vs $T_azd vs $HOME/.azure vs $HOME/.azd.

. $PSScriptRoot/_common.ps1

$probeId    = 'azd-honors-azure-config-dir'
$hypothesis = 'azd preserves AZURE_CONFIG_DIR when shelling out to az (so per-profile az state stays per-profile during azd flows)'
$expected   = "no writes to `$HOME/.azure or `$HOME/.azd during 'azd config list' when both env vars point at fresh `$T_*"

if (-not (Test-CommandAvailable 'azd')) {
    Write-ProbeResult -Result (New-ProbeResult `
        -Test $probeId -Tool 'azd' -Category 'cross-tool' `
        -Hypothesis $hypothesis -Expected $expected `
        -Skipped $true -SkipReason 'azd CLI not found on PATH')
    exit 0
}

$tempAzure = New-ProbeTempDir -ProbeId "$probeId-azure"
$tempAzd   = New-ProbeTempDir -ProbeId "$probeId-azd"

$homeAzure = Get-HomeAzureDir
$homeAzd = Get-HomeAzdDir
$mAzureBefore = Get-DirMtime -Path $homeAzure
$mAzdBefore   = Get-DirMtime -Path $homeAzd

$run = Invoke-ProbeCommand -Command 'azd' -Arguments @('config','list','--output','json') `
                           -EnvOverrides @{ 'AZURE_CONFIG_DIR' = $tempAzure; 'AZD_CONFIG_DIR' = $tempAzd } `
                           -TimeoutSeconds 30

$mAzureAfter = Get-DirMtime -Path $homeAzure
$mAzdAfter   = Get-DirMtime -Path $homeAzd

$azureUnchanged = $true
$azdUnchanged = $true
if ($null -ne $mAzureBefore -and $null -ne $mAzureAfter) { $azureUnchanged = ($mAzureBefore -eq $mAzureAfter) }
if ($null -ne $mAzdBefore   -and $null -ne $mAzdAfter)   { $azdUnchanged   = ($mAzdBefore   -eq $mAzdAfter) }

$tempAzureFiles = @(Get-DirSnapshot -Path $tempAzure).Count
$tempAzdFiles   = @(Get-DirSnapshot -Path $tempAzd).Count

$notes = @(
    "Descriptive only — observation that azd does/does not touch `$HOME during a config-list call. The real test (azd auth login → az account list) requires live creds; deferred."
)

$actual = "tempAzureFiles=$tempAzureFiles; tempAzdFiles=$tempAzdFiles; homeAzureUnchanged=$azureUnchanged; homeAzdUnchanged=$azdUnchanged; azdExit=$($run.exit_code)"

Write-ProbeResult -Result (New-ProbeResult `
    -Test $probeId -Tool 'azd' -Category 'cross-tool' `
    -Hypothesis $hypothesis -Expected $expected `
    -Actual $actual -Isolated $null `
    -ToolVersion (Get-ToolVersion 'azd') `
    -Notes $notes -DurationMs $run.duration_ms)

Remove-ProbeTempDir -Path $tempAzure
Remove-ProbeTempDir -Path $tempAzd
exit 0
