#!/usr/bin/env pwsh
# scripts/isolation/windows-azd-shellout/probe.ps1
#
# Probe ID:   windows-azd-shellout
# Tool:       azd  (with az shell-out)
# Category:   cross-tool
#
# Why this probe exists
# ----------------------
# The Wave-2 WAM mitigation shipped day-1, but flagged
# Known Unknown #6 (azd → az shell-out) as "asserted in theory, not
# verified under a live `azd auth login`". The failure mode would be a
# refresh token landing in $HOME/.azure instead of the per-profile
# $T_azure dir — silently breaking isolation for any operator who runs
# azd auth login from inside `pm exec`.
#
# Defense in code: `internal/providers/azd.go::Apply` sets
# AZD_CONFIG_DIR on the env map, and when a profile also has [azure]
# config, `az.Apply` adds AZURE_CONFIG_DIR to the same map. Child az
# processes spawned by azd therefore inherit BOTH env vars — assuming
# azd doesn't deliberately strip them before exec-ing the child. This
# probe verifies that assumption.
#
# Approach
# --------
#   1. Allocate two fresh, distinct temp dirs: $T_azure and $T_azd.
#   2. Export both env vars pointing at them and invoke `azd config show`
#      (cheap, no auth required, observable on every azd version).
#   3. Capture before/after snapshots of $T_azure, $T_azd, $HOME/.azure,
#      $HOME/.azd.
#   4. Then trigger an azd command that *empirically* shells out to az
#      (`azd version --output json` is safe, no auth, often shows the
#      bundled az client version — exact shell-out depends on azd
#      version, so we use it as a low-confidence probe and document
#      the result rather than gating isolated=true on it alone).
#   5. Detect leak: any file created under $HOME/.azure or $HOME/.azd
#      after the call window started is treated as a state leak.
#
# What this probe DOES NOT do
# ---------------------------
#   - It does not invoke `azd auth login` (requires interactive auth +
#     a real tenant). That's the high-value but live-only probe; it
#     remains in the documentation as a manual smoke test for operators.
#   - It does not assert the bound between azd internals and az binaries
#     across azd versions — azd's choice of when to fork az evolves
#     across releases. We pin the *observable* invariant: the env vars
#     stay set, the temp dirs accept writes, and $HOME is unchanged.
#
# Output contract: see expected.json in this directory.
#
# Exit code: always 0 (per matrix v1 convention). isolated:true | :false | :null.

. "$PSScriptRoot/../_common.ps1"

$probeId    = 'windows-azd-shellout'
$hypothesis = 'azd preserves AZURE_CONFIG_DIR and AZD_CONFIG_DIR when shelling out to az during a config-only flow; $HOME/.azure and $HOME/.azd are untouched.'
$expected   = "After 'azd config show' + 'azd version' with both env vars set: `$T_azure and `$T_azd are populated (or at least mtime-touched); `$HOME/.azure and `$HOME/.azd mtimes are unchanged; azd exit code is 0."

# Platform gate: this is the Windows-specific concern. We do not skip on
# Linux/macOS — azd-on-Mac also shells out to az and the probe is still
# informative — but we annotate the result so consumers can filter.
$platformNote = if ($IsWindows) { 'windows' } else { 'non-windows (informational)' }

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
$homeAzd   = Get-HomeAzdDir

# Baseline snapshots so we can prove (or disprove) that the shell-out
# wrote into $HOME during our window.
$mAzureBefore = Get-DirMtime -Path $homeAzure
$mAzdBefore   = Get-DirMtime -Path $homeAzd
$snapAzureBefore = @(Get-DirSnapshot -Path $homeAzure)
$snapAzdBefore   = @(Get-DirSnapshot -Path $homeAzd)

# --- Call 1: azd config show (cheap, no auth) ---------------------------
$run1 = Invoke-ProbeCommand -Command 'azd' -Arguments @('config','show') `
                            -EnvOverrides @{
                                'AZURE_CONFIG_DIR' = $tempAzure
                                'AZD_CONFIG_DIR'   = $tempAzd
                            } `
                            -TimeoutSeconds 30

# --- Call 2: azd version --output json --------------------------------
# Many azd versions probe the bundled/host az client during `azd version`
# (it surfaces the az version in its output on some builds). Even when
# it doesn't fork az, the env-var inheritance contract is exercised by
# azd's own process startup. Cheap + safe.
$run2 = Invoke-ProbeCommand -Command 'azd' -Arguments @('version','--output','json') `
                            -EnvOverrides @{
                                'AZURE_CONFIG_DIR' = $tempAzure
                                'AZD_CONFIG_DIR'   = $tempAzd
                            } `
                            -TimeoutSeconds 30

$mAzureAfter = Get-DirMtime -Path $homeAzure
$mAzdAfter   = Get-DirMtime -Path $homeAzd
$snapAzureAfter = @(Get-DirSnapshot -Path $homeAzure)
$snapAzdAfter   = @(Get-DirSnapshot -Path $homeAzd)

# Mtime change means *something* under $HOME got rewritten during our
# call window. New files = strong leak signal. Mtime change without
# new files = weaker (could be a metadata touch by a sibling process),
# still flagged.
$homeAzureUnchanged = $true
$homeAzdUnchanged = $true
if ($null -ne $mAzureBefore -and $null -ne $mAzureAfter) { $homeAzureUnchanged = ($mAzureBefore -eq $mAzureAfter) }
if ($null -ne $mAzdBefore   -and $null -ne $mAzdAfter)   { $homeAzdUnchanged   = ($mAzdBefore   -eq $mAzdAfter) }

# New-file detection: name-set diff (cheap, ignores in-place rewrites).
$beforeAzureNames = @($snapAzureBefore | ForEach-Object { $_.name })
$afterAzureNames  = @($snapAzureAfter  | ForEach-Object { $_.name })
$newAzureFiles    = @($afterAzureNames | Where-Object { $_ -notin $beforeAzureNames })

$beforeAzdNames = @($snapAzdBefore | ForEach-Object { $_.name })
$afterAzdNames  = @($snapAzdAfter  | ForEach-Object { $_.name })
$newAzdFiles    = @($afterAzdNames | Where-Object { $_ -notin $beforeAzdNames })

$tempAzureFiles = @(Get-DirSnapshot -Path $tempAzure).Count
$tempAzdFiles   = @(Get-DirSnapshot -Path $tempAzd).Count

$bothExitedZero = ($run1.exit_code -eq 0) -and ($run2.exit_code -eq 0)
$noNewHomeFiles  = ($newAzureFiles.Count -eq 0) -and ($newAzdFiles.Count -eq 0)
$noHomeMtimeBump = $homeAzureUnchanged -and $homeAzdUnchanged

# Verdict matrix:
#   isolated=true  : both calls succeeded, no new $HOME files, no mtime bumps
#   isolated=false : new files appeared under $HOME — the smoking-gun leak
#   isolated=null  : ambiguous (mtime bump without new files, or exit code
#                    nonzero) — surfaces as DESC in the matrix
$isolated = $null
if ($bothExitedZero -and $noNewHomeFiles -and $noHomeMtimeBump) {
    $isolated = $true
} elseif (-not $noNewHomeFiles) {
    $isolated = $false
}

$notes = @(
    "platform=$platformNote",
    "azd config show: exit=$($run1.exit_code) duration_ms=$($run1.duration_ms)",
    "azd version    : exit=$($run2.exit_code) duration_ms=$($run2.duration_ms)"
)
if ($newAzureFiles.Count -gt 0) {
    $notes += "LEAK: new files under `$HOME/.azure: $($newAzureFiles -join ', ')"
}
if ($newAzdFiles.Count -gt 0) {
    $notes += "LEAK: new files under `$HOME/.azd: $($newAzdFiles -join ', ')"
}
if (-not $bothExitedZero) {
    $notes += "stderr1='$(($run1.stderr -split "`n")[0])'"
    $notes += "stderr2='$(($run2.stderr -split "`n")[0])'"
}
$notes += "FOLLOW-UP: this probe does NOT cover 'azd auth login' (interactive). That remains a manual smoke test per docs/isolation-matrix.md#6."

$durationTotal = $run1.duration_ms + $run2.duration_ms
$actual = "tempAzureFiles=$tempAzureFiles; tempAzdFiles=$tempAzdFiles; newHomeAzureFiles=$($newAzureFiles.Count); newHomeAzdFiles=$($newAzdFiles.Count); homeAzureMtimeUnchanged=$homeAzureUnchanged; homeAzdMtimeUnchanged=$homeAzdUnchanged; azdConfigExit=$($run1.exit_code); azdVersionExit=$($run2.exit_code)"

Write-ProbeResult -Result (New-ProbeResult `
    -Test $probeId -Tool 'azd' -Category 'cross-tool' `
    -Hypothesis $hypothesis -Expected $expected `
    -Actual $actual -Isolated $isolated `
    -ToolVersion (Get-ToolVersion 'azd') `
    -Notes $notes -DurationMs $durationTotal)

Remove-ProbeTempDir -Path $tempAzure
Remove-ProbeTempDir -Path $tempAzd
exit 0
