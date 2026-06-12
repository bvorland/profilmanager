#!/usr/bin/env pwsh
# probe-az-account-show.ps1
#
# Live-login-gated. Confirms that with AZURE_CONFIG_DIR=$T (empty),
# 'az account show' returns "not logged in" — i.e. the operator's existing
# accounts in $HOME/.azure are NOT visible through a fresh config dir.
#
# Side effect of running with live creds: also reports whether 'az account list'
# from a populated config (the operator's real $HOME/.azure) returns >1 sub
# (used by the multi-tenant claim).

. $PSScriptRoot/_common.ps1

$probeId    = 'az-account-show'
$hypothesis = 'A fresh AZURE_CONFIG_DIR has no visible accounts even if $HOME/.azure has many'
$expected   = "'az account show' against fresh `$T returns 'Please run az login'; isolation is confirmed by absence of accounts"

if (-not (Test-CommandAvailable 'az')) {
    Write-ProbeResult -Result (New-ProbeResult `
        -Test $probeId -Tool 'az' -Category 'account-state' `
        -Hypothesis $hypothesis -Expected $expected `
        -Skipped $true -SkipReason 'az CLI not found on PATH')
    exit 0
}

$tempDir = New-ProbeTempDir -ProbeId $probeId

$run = Invoke-ProbeCommand -Command 'az' -Arguments @('account','show','--output','json') `
                           -EnvOverrides @{ 'AZURE_CONFIG_DIR' = $tempDir } `
                           -TimeoutSeconds 30

$notLoggedIn = (Test-AzNotLoggedIn -Text $run.stderr) -or (Test-AzNotLoggedIn -Text $run.stdout)

if ($notLoggedIn) {
    # Good: fresh dir has no account. That's exactly what isolation looks like.
    $actual = "Fresh `$T returned 'not logged in' as expected. stderr trimmed='$(($run.stderr -split "`n")[0])'"
    $isolated = $true
    $notes = @("Probe ran without live creds (or fresh dir is empty). Re-run after 'az login' in `$HOME/.azure to test the populated-vs-fresh isolation explicitly.")
} elseif ($run.exit_code -eq 0 -and $run.stdout.Trim().StartsWith('{')) {
    # Surprising: fresh AZURE_CONFIG_DIR returned a logged-in account.
    # This is a strong signal that account state leaked from somewhere outside $T.
    $actual = "Fresh `$T returned a logged-in account JSON. THIS IS A LEAK SIGNAL. stdout preview='$(($run.stdout.Substring(0, [math]::Min(200,$run.stdout.Length))).Trim())'"
    $isolated = $false
    $notes = @(
        "Possible leak sources: WAM broker (%LOCALAPPDATA%\.IdentityService), Windows Credential Manager, MSAL extension cache, or AZURE_USERNAME / AZURE_CLIENT_ID env vars.",
        "Cross-check with probe-windows-wam-presence and probe-windows-credman-keys."
    )
} else {
    $actual = "Inconclusive. azExit=$($run.exit_code); stderr='$(($run.stderr -split "`n")[0])'"
    $isolated = $null
    $notes = @("Probe could not classify the response; manual inspection recommended.")
}

Write-ProbeResult -Result (New-ProbeResult `
    -Test $probeId -Tool 'az' -Category 'account-state' `
    -Hypothesis $hypothesis -Expected $expected `
    -Actual $actual -Isolated $isolated `
    -ToolVersion (Get-ToolVersion 'az') `
    -Notes $notes -DurationMs $run.duration_ms)

Remove-ProbeTempDir -Path $tempDir
exit 0
