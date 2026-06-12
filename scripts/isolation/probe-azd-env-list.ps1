#!/usr/bin/env pwsh
# probe-azd-env-list.ps1
#
# 'azd env list' against fresh AZD_CONFIG_DIR should return [] — i.e.
# environments in $HOME/.azd are not visible through a fresh config dir.

. $PSScriptRoot/_common.ps1

$probeId    = 'azd-env-list'
$hypothesis = 'azd env list with fresh AZD_CONFIG_DIR returns []; $HOME/.azd envs are not leaked'
$expected   = "'azd env list --output json' against `$T returns an empty array"

if (-not (Test-CommandAvailable 'azd')) {
    Write-ProbeResult -Result (New-ProbeResult `
        -Test $probeId -Tool 'azd' -Category 'env' `
        -Hypothesis $hypothesis -Expected $expected `
        -Skipped $true -SkipReason 'azd CLI not found on PATH')
    exit 0
}

$tempDir = New-ProbeTempDir -ProbeId $probeId

$run = Invoke-ProbeCommand -Command 'azd' -Arguments @('env','list','--output','json') `
                           -EnvOverrides @{ 'AZD_CONFIG_DIR' = $tempDir } `
                           -TimeoutSeconds 30

$count = -1
try {
    if ($run.exit_code -eq 0 -and -not [string]::IsNullOrWhiteSpace($run.stdout)) {
        $parsed = $run.stdout | ConvertFrom-Json -ErrorAction Stop
        $count = @($parsed).Count
    } elseif ($run.stdout.Trim() -eq '[]') {
        $count = 0
    }
} catch {
    $count = -1
}

# Context: how many envs in $HOME/.azd?
$homeAzd = Get-HomeAzdDir
$homeEnvsDir = Join-Path $homeAzd 'envs'
$homeEnvCount = 0
if (Test-Path $homeEnvsDir) {
    $homeEnvCount = @(Get-ChildItem $homeEnvsDir -Directory -ErrorAction SilentlyContinue).Count
}

$isolated = $null
$notes = @()
if ($count -lt 0) {
    $notes += "Could not parse azd env list output. stderrFirstLine='$(($run.stderr -split "`n")[0])'"
} elseif ($homeEnvCount -eq 0) {
    $isolated = $null
    $notes += "`$HOME/.azd has no envs; probe cannot distinguish isolation from emptiness."
} else {
    $isolated = ($count -eq 0)
}

$actual = "freshDirEnvCount=$count; homeAzdEnvCount=$homeEnvCount; azdExit=$($run.exit_code)"

Write-ProbeResult -Result (New-ProbeResult `
    -Test $probeId -Tool 'azd' -Category 'env' `
    -Hypothesis $hypothesis -Expected $expected `
    -Actual $actual -Isolated $isolated `
    -ToolVersion (Get-ToolVersion 'azd') `
    -Notes $notes -DurationMs $run.duration_ms)

Remove-ProbeTempDir -Path $tempDir
exit 0
