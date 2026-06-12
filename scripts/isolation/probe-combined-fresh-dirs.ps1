#!/usr/bin/env pwsh
# probe-combined-fresh-dirs.ps1
#
# Both AZURE_CONFIG_DIR and AZD_CONFIG_DIR point at distinct, fresh temp dirs.
# Run a read of each tool; verify both temp dirs receive state and NEITHER
# $HOME/.azure nor $HOME/.azd is mutated.

. $PSScriptRoot/_common.ps1

$probeId    = 'combined-fresh-dirs'
$hypothesis = 'AZURE_CONFIG_DIR and AZD_CONFIG_DIR isolate az and azd state simultaneously into distinct dirs'
$expected   = "after az+azd reads, `$T_azure populated, `$T_azd populated, `$HOME/.azure unchanged, `$HOME/.azd unchanged"

$hasAz  = Test-CommandAvailable 'az'
$hasAzd = Test-CommandAvailable 'azd'

if (-not $hasAz -and -not $hasAzd) {
    Write-ProbeResult -Result (New-ProbeResult `
        -Test $probeId -Tool 'combined' -Category 'isolation' `
        -Hypothesis $hypothesis -Expected $expected `
        -Skipped $true -SkipReason 'neither az nor azd found on PATH')
    exit 0
}

$tempAzure = New-ProbeTempDir -ProbeId "$probeId-azure"
$tempAzd   = New-ProbeTempDir -ProbeId "$probeId-azd"
$homeAzure = Get-HomeAzureDir
$homeAzd   = Get-HomeAzdDir
$mAzureBefore = Get-DirMtime -Path $homeAzure
$mAzdBefore   = Get-DirMtime -Path $homeAzd

$azRun = $null
$azdRun = $null
$envOverrides = @{ 'AZURE_CONFIG_DIR' = $tempAzure; 'AZD_CONFIG_DIR' = $tempAzd }

if ($hasAz) {
    $azRun = Invoke-ProbeCommand -Command 'az' -Arguments @('config','get','--output','json') -EnvOverrides $envOverrides -TimeoutSeconds 30
}
if ($hasAzd) {
    $azdRun = Invoke-ProbeCommand -Command 'azd' -Arguments @('config','list','--output','json') -EnvOverrides $envOverrides -TimeoutSeconds 30
}

$mAzureAfter = Get-DirMtime -Path $homeAzure
$mAzdAfter   = Get-DirMtime -Path $homeAzd
$azureFiles = @(Get-DirSnapshot -Path $tempAzure)
$azdFiles   = @(Get-DirSnapshot -Path $tempAzd)

$azurePopulated = ($azureFiles.Count -gt 0)
$azdPopulated   = ($azdFiles.Count -gt 0)
$azureHomeUnchanged = $true; $azdHomeUnchanged = $true
if ($null -ne $mAzureBefore -and $null -ne $mAzureAfter) { $azureHomeUnchanged = ($mAzureBefore -eq $mAzureAfter) }
if ($null -ne $mAzdBefore   -and $null -ne $mAzdAfter)   { $azdHomeUnchanged   = ($mAzdBefore   -eq $mAzdAfter) }

$isolated = $true
if ($hasAz)  { $isolated = $isolated -and $azurePopulated -and $azureHomeUnchanged }
if ($hasAzd) { $isolated = $isolated -and $azdPopulated   -and $azdHomeUnchanged }

$durations = @()
if ($azRun)  { $durations += $azRun.duration_ms }
if ($azdRun) { $durations += $azdRun.duration_ms }
$totalDuration = ($durations | Measure-Object -Sum).Sum

$notes = @()
if ($hasAz -and -not $azurePopulated) { $notes += "`$T_azure was not populated (az read-only or skipped)" }
if ($hasAzd -and -not $azdPopulated)  { $notes += "`$T_azd was not populated (azd read-only or skipped)" }
if (-not $hasAz)  { $notes += 'az not present — claim partially tested' }
if (-not $hasAzd) { $notes += 'azd not present — claim partially tested' }

$actual = "azPresent=$hasAz; azdPresent=$hasAzd; tempAzureFiles=$($azureFiles.Count); tempAzdFiles=$($azdFiles.Count); homeAzureUnchanged=$azureHomeUnchanged; homeAzdUnchanged=$azdHomeUnchanged"

Write-ProbeResult -Result (New-ProbeResult `
    -Test $probeId -Tool 'combined' -Category 'isolation' `
    -Hypothesis $hypothesis -Expected $expected `
    -Actual $actual -Isolated $isolated `
    -ToolVersion $(if ($hasAz) { Get-ToolVersion 'az' } elseif ($hasAzd) { Get-ToolVersion 'azd' } else { $null }) `
    -Notes $notes -DurationMs $totalDuration)

Remove-ProbeTempDir -Path $tempAzure
Remove-ProbeTempDir -Path $tempAzd
exit 0
