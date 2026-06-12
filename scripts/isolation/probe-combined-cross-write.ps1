#!/usr/bin/env pwsh
# probe-combined-cross-write.ps1
#
# With AZURE_CONFIG_DIR=$T_azure and AZD_CONFIG_DIR=$T_azd (distinct),
# verify that:
#   1. After 'az config get', $T_azure contains recognizable az artifacts
#      (e.g. azureProfile.json) and $T_azd does NOT.
#   2. After 'azd config list', $T_azd contains recognizable azd artifacts
#      (e.g. config.json / state.json) and $T_azure does NOT receive azd state.
#
# If az writes anything into AZD_CONFIG_DIR or vice versa, that's a cross-write leak.

. $PSScriptRoot/_common.ps1

$probeId    = 'combined-cross-write'
$hypothesis = 'az writes only under AZURE_CONFIG_DIR; azd writes only under AZD_CONFIG_DIR; no cross-tool dir contamination'
$expected   = '$T_azure contains az-shaped artifacts only; $T_azd contains azd-shaped artifacts only'

$hasAz  = Test-CommandAvailable 'az'
$hasAzd = Test-CommandAvailable 'azd'

if (-not $hasAz -or -not $hasAzd) {
    Write-ProbeResult -Result (New-ProbeResult `
        -Test $probeId -Tool 'combined' -Category 'cross-write' `
        -Hypothesis $hypothesis -Expected $expected `
        -Skipped $true -SkipReason 'requires BOTH az and azd on PATH')
    exit 0
}

$tempAzure = New-ProbeTempDir -ProbeId "$probeId-azure"
$tempAzd   = New-ProbeTempDir -ProbeId "$probeId-azd"
$envOverrides = @{ 'AZURE_CONFIG_DIR' = $tempAzure; 'AZD_CONFIG_DIR' = $tempAzd }

$azRun = Invoke-ProbeCommand -Command 'az' -Arguments @('config','get','--output','json') -EnvOverrides $envOverrides -TimeoutSeconds 30

$azureAfterAz = @(Get-DirSnapshot -Path $tempAzure | ForEach-Object { $_.name })
$azdAfterAz   = @(Get-DirSnapshot -Path $tempAzd   | ForEach-Object { $_.name })

$azdRun = Invoke-ProbeCommand -Command 'azd' -Arguments @('config','list','--output','json') -EnvOverrides $envOverrides -TimeoutSeconds 30

$azureAfterAzd = @(Get-DirSnapshot -Path $tempAzure | ForEach-Object { $_.name })
$azdAfterAzd   = @(Get-DirSnapshot -Path $tempAzd   | ForEach-Object { $_.name })

# Recognizable az artifacts (DISTINCTIVE — must not appear in azd dirs).
# 'telemetry' and 'config' are excluded because azd creates a 'telemetry' subdir and a 'config.json' file too.
$azMarkers = @('azureProfile.json','commandIndex.json','versionCheck.json','az.sess','az.json')
# Recognizable azd artifacts (DISTINCTIVE — must not appear in az dirs).
$azdMarkers = @('config.json','state.json','auth','machine-id.cache','update-check.json')

function Test-AnyMarker {
    param([string[]]$Names, [string[]]$Markers)
    foreach ($n in $Names) {
        foreach ($m in $Markers) {
            if ($n -ieq $m) { return $true }
        }
    }
    return $false
}

$azurePure = -not (Test-AnyMarker -Names $azureAfterAzd -Markers $azdMarkers)
$azdPure   = -not (Test-AnyMarker -Names $azdAfterAzd   -Markers $azMarkers)

$isolated = $azurePure -and $azdPure

$notes = @()
if (-not $azurePure) { $notes += "az dir contains azd-shaped files after azd ran: $($azureAfterAzd -join ', ')" }
if (-not $azdPure)   { $notes += "azd dir contains az-shaped files after az ran: $($azdAfterAzd -join ', ')" }
if ($azureAfterAz.Count -eq 0) { $notes += 'az did not populate AZURE_CONFIG_DIR; cross-write claim is weakly tested' }
if ($azdAfterAzd.Count -eq 0)  { $notes += 'azd did not populate AZD_CONFIG_DIR; cross-write claim is weakly tested' }

$actual = "azDirAfterAz=[$($azureAfterAz -join ', ')]; azdDirAfterAz=[$($azdAfterAz -join ', ')]; azDirAfterAzd=[$($azureAfterAzd -join ', ')]; azdDirAfterAzd=[$($azdAfterAzd -join ', ')]"

Write-ProbeResult -Result (New-ProbeResult `
    -Test $probeId -Tool 'combined' -Category 'cross-write' `
    -Hypothesis $hypothesis -Expected $expected `
    -Actual $actual -Isolated $isolated `
    -ToolVersion (Get-ToolVersion 'az') `
    -Notes $notes -DurationMs ($azRun.duration_ms + $azdRun.duration_ms))

Remove-ProbeTempDir -Path $tempAzure
Remove-ProbeTempDir -Path $tempAzd
exit 0
