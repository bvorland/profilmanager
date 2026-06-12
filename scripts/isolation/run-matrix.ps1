#!/usr/bin/env pwsh
# scripts/isolation/run-matrix.ps1
#
# Discovers every probe-*.ps1 in this directory, runs each in its own pwsh
# process, aggregates the JSON envelopes into a single report, prints a
# human-readable summary, and always exits 0 in v1.
#
# Usage:
#   pwsh -NoProfile -File scripts/isolation/run-matrix.ps1
#   pwsh -NoProfile -File scripts/isolation/run-matrix.ps1 -OutputFile isolation-report.json
#   pwsh -NoProfile -File scripts/isolation/run-matrix.ps1 -Quiet           # JSON only
#
# Future:
#   -Strict        exit non-zero if any probe reports isolated:false (deferred)
#   -AllowLive     opt-in to live-cloud probes (currently no probe gates on this)

[CmdletBinding()]
param(
    [string]$OutputFile,
    [switch]$Quiet
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

. $PSScriptRoot/_common.ps1

# Discovery: flat 'probe-*.ps1' files at $PSScriptRoot AND any sub-directory
# whose layout is 'subdir/probe.ps1' (package-style probes — used for probes
# that need siblings like expected.json or fixture data). Both naming styles
# are first-class for v1; we sort by display name for stable matrix order.
$flatProbes    = @(Get-ChildItem -Path $PSScriptRoot -Filter 'probe-*.ps1' -File)
$packageProbes = @(Get-ChildItem -Path $PSScriptRoot -Directory | ForEach-Object {
    $candidate = Join-Path $_.FullName 'probe.ps1'
    if (Test-Path $candidate -PathType Leaf) { Get-Item $candidate }
})
$probes = @($flatProbes + $packageProbes) | Sort-Object FullName

if (-not $Quiet) {
    Write-Host ""
    Write-Host "Running $($probes.Count) probes from $PSScriptRoot" -ForegroundColor Cyan
    Write-Host ("=" * 60)
}

$results = @()
$start = [datetime]::UtcNow

foreach ($probe in $probes) {
    # Derive a stable, collision-free key that works for both flat
    # 'probe-foo.ps1' and package 'foo/probe.ps1' layouts. Used for temp
    # filenames and fallback envelope test-name.
    if ($probe.Name -eq 'probe.ps1') {
        $probeKey = (Split-Path -Parent $probe.FullName | Split-Path -Leaf)
    } else {
        $probeKey = $probe.BaseName -replace '^probe-',''
    }
    if (-not $Quiet) { Write-Host ("• {0}" -f $probeKey) -NoNewline }
    $proc = Start-Process -FilePath 'pwsh' `
                          -ArgumentList @('-NoProfile','-File',$probe.FullName) `
                          -NoNewWindow -RedirectStandardOutput "$env:TEMP\pm-iso-out.$PID.$probeKey.txt" `
                          -RedirectStandardError  "$env:TEMP\pm-iso-err.$PID.$probeKey.txt" `
                          -PassThru -Wait
    $stdoutPath = "$env:TEMP\pm-iso-out.$PID.$probeKey.txt"
    $stderrPath = "$env:TEMP\pm-iso-err.$PID.$probeKey.txt"
    $stdout = if (Test-Path $stdoutPath) { Get-Content $stdoutPath -Raw } else { '' }
    $stderr = if (Test-Path $stderrPath) { Get-Content $stderrPath -Raw } else { '' }
    Remove-Item $stdoutPath, $stderrPath -ErrorAction SilentlyContinue

    $parsed = $null
    try {
        if (-not [string]::IsNullOrWhiteSpace($stdout)) {
            $parsed = $stdout | ConvertFrom-Json -ErrorAction Stop
        }
    } catch {
        $parsed = $null
    }

    if ($null -eq $parsed) {
        # Synthesize an "errored" probe envelope so the aggregate is still complete.
        $parsed = [pscustomobject]@{
            test          = $probeKey
            tool          = 'unknown'
            category      = 'harness-error'
            hypothesis    = 'probe should emit a valid JSON envelope on stdout'
            expected      = 'one JSON object on stdout, exit 0'
            actual        = "probeExit=$($proc.ExitCode); stdoutPreview='$(($stdout.Substring(0,[math]::Min(200,$stdout.Length))).Trim())'; stderrFirstLine='$(($stderr -split "`n")[0])'"
            isolated      = $null
            skipped       = $false
            skip_reason   = $null
            duration_ms   = 0
            notes         = @('probe failed to emit valid JSON; treated as harness error')
            host          = Get-HostInfo
            tool_version  = $null
            probe_version = 'unknown'
            generated_at  = (Get-IsoTimestamp)
            _harness_error = $true
        }
    }

    $results += $parsed

    if (-not $Quiet) {
        $status = if ($parsed.PSObject.Properties.Name -contains '_harness_error' -and $parsed._harness_error) {
            'ERROR'
        } elseif ($parsed.skipped) {
            'SKIP'
        } elseif ($null -eq $parsed.isolated) {
            'DESC'
        } elseif ($parsed.isolated) {
            'PASS'
        } else {
            'LEAK'
        }
        $color = switch ($status) {
            'PASS'  { 'Green' }
            'LEAK'  { 'Red' }
            'ERROR' { 'Red' }
            'SKIP'  { 'DarkGray' }
            default { 'Yellow' }
        }
        Write-Host (" [{0}]" -f $status) -ForegroundColor $color
    }
}

# Tallies.
$total = $results.Count
$isolated = (@($results | Where-Object { -not $_.skipped -and $_.isolated -eq $true })).Count
$leaked = (@($results | Where-Object { -not $_.skipped -and $_.isolated -eq $false })).Count
$skipped = (@($results | Where-Object { $_.skipped })).Count
$descriptive = (@($results | Where-Object { -not $_.skipped -and $null -eq $_.isolated -and -not ($_.PSObject.Properties.Name -contains '_harness_error' -and $_._harness_error) })).Count
$errors = (@($results | Where-Object { $_.PSObject.Properties.Name -contains '_harness_error' -and $_._harness_error })).Count

$report = [pscustomobject]@{
    schema       = 'isolation-matrix/v1'
    generated_at = (Get-IsoTimestamp)
    host         = Get-HostInfo
    summary      = [pscustomobject]@{
        total       = $total
        isolated    = $isolated
        leaked      = $leaked
        skipped     = $skipped
        descriptive = $descriptive
        errors      = $errors
    }
    probes       = $results
}

$json = $report | ConvertTo-Json -Depth 12

if ($OutputFile) {
    $reportDir = Split-Path -Parent $OutputFile
    if ($reportDir -and -not (Test-Path $reportDir)) {
        New-Item -ItemType Directory -Path $reportDir -Force | Out-Null
    }
    Set-Content -Path $OutputFile -Value $json -Encoding UTF8
}

if (-not $Quiet) {
    Write-Host ""
    Write-Host ("=" * 60)
    Write-Host "Summary" -ForegroundColor Cyan
    Write-Host ("  total       : {0}" -f $total)
    Write-Host ("  isolated    : {0}" -f $isolated) -ForegroundColor Green
    Write-Host ("  leaked      : {0}" -f $leaked) -ForegroundColor $(if ($leaked -gt 0) { 'Red' } else { 'Gray' })
    Write-Host ("  skipped     : {0}" -f $skipped) -ForegroundColor DarkGray
    Write-Host ("  descriptive : {0}" -f $descriptive) -ForegroundColor Yellow
    Write-Host ("  errors      : {0}" -f $errors) -ForegroundColor $(if ($errors -gt 0) { 'Red' } else { 'Gray' })
    if ($OutputFile) {
        Write-Host ""
        Write-Host "Full report: $OutputFile"
    }
    Write-Host ""
    Write-Host "Exit code: 0 (v1 always exits 0 — use --Strict in a future version to fail on leaks)"
} else {
    Write-Output $json
}

exit 0
