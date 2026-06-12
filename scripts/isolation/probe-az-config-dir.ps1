#!/usr/bin/env pwsh
# probe-az-config-dir.ps1
#
# Hypothesis: setting AZURE_CONFIG_DIR=$T causes 'az' to read/write only under $T,
# and never mutates $HOME/.azure during the call.

. $PSScriptRoot/_common.ps1

$probeId   = 'az-config-dir'
$hypothesis = 'AZURE_CONFIG_DIR=$T fully isolates az config/cache writes under $T'
$expected   = "After 'az config get' with AZURE_CONFIG_DIR=`$T, `$T is populated and `$HOME/.azure mtime is unchanged"

if (-not (Test-CommandAvailable 'az')) {
    Write-ProbeResult -Result (New-ProbeResult `
        -Test $probeId -Tool 'az' -Category 'config-dir' `
        -Hypothesis $hypothesis -Expected $expected `
        -Skipped $true -SkipReason 'az CLI not found on PATH')
    exit 0
}

$tempDir = New-ProbeTempDir -ProbeId $probeId
$homeAzure = Get-HomeAzureDir
$homeAzureBefore = Get-DirMtime -Path $homeAzure
$tempBefore = Get-DirSnapshot -Path $tempDir

$run = Invoke-ProbeCommand -Command 'az' -Arguments @('config','get','--output','json') `
                           -EnvOverrides @{ 'AZURE_CONFIG_DIR' = $tempDir } `
                           -TimeoutSeconds 30

$homeAzureAfter = Get-DirMtime -Path $homeAzure
$tempAfter = @(Get-DirSnapshot -Path $tempDir)
$tempBeforeArr = @($tempBefore)

$tempFiles  = @($tempAfter | ForEach-Object { $_.name }) -join ', '
$tempPopulated = ($tempAfter.Count -gt $tempBeforeArr.Count)
$homeUnchanged = $true
if ($null -ne $homeAzureBefore -and $null -ne $homeAzureAfter) {
    $homeUnchanged = ($homeAzureBefore -eq $homeAzureAfter)
}

$notes = @()
if ($run.timed_out) { $notes += "az timed out after 30s — inspect manually" }
if ($run.exit_code -ne 0 -and -not $run.timed_out) {
    $notes += "az exited $($run.exit_code); stderr=$($run.stderr.Trim())"
}
if (-not $tempPopulated) {
    $notes += "Probe dir was not populated — az may have buffered to a different location, or 'az config get' is read-only on this version"
}

$isolated = $tempPopulated -and $homeUnchanged
$actual = "tempDir='$tempDir'; tempFilesAfter=[$tempFiles]; homeAzureMtimeUnchanged=$homeUnchanged; azExit=$($run.exit_code); stdoutLen=$($run.stdout.Length); stderrLen=$($run.stderr.Length)"

Write-ProbeResult -Result (New-ProbeResult `
    -Test $probeId -Tool 'az' -Category 'config-dir' `
    -Hypothesis $hypothesis -Expected $expected `
    -Actual $actual -Isolated $isolated `
    -ToolVersion (Get-ToolVersion 'az') `
    -Notes $notes -DurationMs $run.duration_ms)

Remove-ProbeTempDir -Path $tempDir
exit 0
