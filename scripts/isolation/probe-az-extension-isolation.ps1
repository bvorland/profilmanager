#!/usr/bin/env pwsh
# probe-az-extension-isolation.ps1
#
# 'az extension list' against a fresh AZURE_CONFIG_DIR should return [].
# If it returns extensions that live in $HOME/.azure/cliextensions, the
# CLI is reading extension state from outside $T.

. $PSScriptRoot/_common.ps1

$probeId    = 'az-extension-isolation'
$hypothesis = 'az extension list against fresh $T returns [] regardless of $HOME/.azure/cliextensions'
$expected   = "'az extension list' under AZURE_CONFIG_DIR=`$T returns an empty JSON array"

if (-not (Test-CommandAvailable 'az')) {
    Write-ProbeResult -Result (New-ProbeResult `
        -Test $probeId -Tool 'az' -Category 'extensions' `
        -Hypothesis $hypothesis -Expected $expected `
        -Skipped $true -SkipReason 'az CLI not found on PATH')
    exit 0
}

$tempDir = New-ProbeTempDir -ProbeId $probeId

$run = Invoke-ProbeCommand -Command 'az' -Arguments @('extension','list','--output','json') `
                           -EnvOverrides @{ 'AZURE_CONFIG_DIR' = $tempDir } `
                           -TimeoutSeconds 30

$isEmpty = $false
$parsedCount = -1
try {
    if ($run.exit_code -eq 0 -and -not [string]::IsNullOrWhiteSpace($run.stdout)) {
        $parsed = $run.stdout | ConvertFrom-Json -ErrorAction Stop
        $parsedArr = @($parsed)
        $parsedCount = $parsedArr.Count
        $isEmpty = ($parsedCount -eq 0)
    }
} catch {
    $parsedCount = -1
}

# Cross-check what's in $HOME/.azure/cliextensions for context.
$homeExtDir = Join-Path (Get-HomeAzureDir) 'cliextensions'
$homeExtCount = 0
if (Test-Path $homeExtDir) {
    $homeExtCount = @(Get-ChildItem $homeExtDir -Directory -ErrorAction SilentlyContinue).Count
}

$notes = @()
if ($homeExtCount -eq 0) {
    $notes += "`$HOME/.azure/cliextensions has no extensions installed; this probe cannot distinguish isolation from 'no extensions exist anywhere'."
}
if ($run.timed_out) { $notes += 'az timed out' }

$isolated = $null
if ($parsedCount -ge 0) {
    if ($homeExtCount -eq 0) {
        # Both empty. Inconclusive — we report isolated=null and add a note.
        $isolated = $null
    } else {
        $isolated = $isEmpty
    }
}

$actual = "freshDirExtensions=$parsedCount; homeAzureExtensions=$homeExtCount; azExit=$($run.exit_code)"

Write-ProbeResult -Result (New-ProbeResult `
    -Test $probeId -Tool 'az' -Category 'extensions' `
    -Hypothesis $hypothesis -Expected $expected `
    -Actual $actual -Isolated $isolated `
    -ToolVersion (Get-ToolVersion 'az') `
    -Notes $notes -DurationMs $run.duration_ms)

Remove-ProbeTempDir -Path $tempDir
exit 0
