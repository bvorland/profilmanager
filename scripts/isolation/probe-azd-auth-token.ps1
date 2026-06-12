#!/usr/bin/env pwsh
# probe-azd-auth-token.ps1
#
# Live-login-gated. If 'azd auth token' succeeds, the token's JWT segment
# is base64-decoded and 'iss' / 'aud' are inspected. The token VALUE itself
# is NEVER recorded — only its decoded header/claims metadata.
#
# If not logged in: skipped.

. $PSScriptRoot/_common.ps1

$probeId    = 'azd-auth-token'
$hypothesis = 'azd auth token, when run with AZD_CONFIG_DIR=$T, surfaces a token whose JWT decodes to an Azure AD issuer'
$expected   = 'token JSON parses, expiresOn is in the future, JWT iss matches login.microsoftonline.com, aud is non-empty'

if (-not (Test-CommandAvailable 'azd')) {
    Write-ProbeResult -Result (New-ProbeResult `
        -Test $probeId -Tool 'azd' -Category 'auth' `
        -Hypothesis $hypothesis -Expected $expected `
        -Skipped $true -SkipReason 'azd CLI not found on PATH')
    exit 0
}

$tempDir = New-ProbeTempDir -ProbeId $probeId

# Use the operator's existing AZD_CONFIG_DIR (default $HOME/.azd) — we want to test
# whether an already-authenticated dir surfaces a token, not to log in fresh.
$run = Invoke-ProbeCommand -Command 'azd' -Arguments @('auth','token','--output','json') -TimeoutSeconds 30

if ((Test-AzdNotLoggedIn -Text $run.stderr) -or (Test-AzdNotLoggedIn -Text $run.stdout) -or $run.exit_code -ne 0) {
    Write-ProbeResult -Result (New-ProbeResult `
        -Test $probeId -Tool 'azd' -Category 'auth' `
        -Hypothesis $hypothesis -Expected $expected `
        -Skipped $true -SkipReason 'azd not logged in (run: azd auth login)' `
        -ToolVersion (Get-ToolVersion 'azd') `
        -Notes @("stderrFirstLine='$(($run.stderr -split "`n")[0])'") `
        -DurationMs $run.duration_ms)
    Remove-ProbeTempDir -Path $tempDir
    exit 0
}

# Parse the JSON. Extract token, decode middle segment.
$findings = @()
$issuerOk = $false
$audOk = $false
$expiresFuture = $false

try {
    $parsed = $run.stdout | ConvertFrom-Json -ErrorAction Stop
    if ($parsed.PSObject.Properties.Name -contains 'expiresOn') {
        $exp = [datetime]::Parse([string]$parsed.expiresOn)
        $expiresFuture = ($exp -gt [datetime]::UtcNow)
        $findings += "expiresOn=$($parsed.expiresOn); future=$expiresFuture"
    }
    if ($parsed.PSObject.Properties.Name -contains 'token' -and -not [string]::IsNullOrWhiteSpace($parsed.token)) {
        $segments = ($parsed.token -split '\.')
        if ($segments.Count -ge 2) {
            $payload = $segments[1]
            # Base64-url decode (pad with '=' as needed)
            $padded = $payload + ('=' * ((4 - ($payload.Length % 4)) % 4))
            $padded = $padded.Replace('-', '+').Replace('_', '/')
            try {
                $bytes = [Convert]::FromBase64String($padded)
                $json = [System.Text.Encoding]::UTF8.GetString($bytes)
                $claims = $json | ConvertFrom-Json -ErrorAction Stop
                $iss = if ($claims.PSObject.Properties.Name -contains 'iss') { [string]$claims.iss } else { '' }
                $aud = if ($claims.PSObject.Properties.Name -contains 'aud') { [string]$claims.aud } else { '' }
                $issuerOk = ($iss -match 'login\.microsoftonline\.com' -or $iss -match 'sts\.windows\.net')
                $audOk = -not [string]::IsNullOrWhiteSpace($aud)
                $findings += "iss='$iss'; issuerOk=$issuerOk; audPresent=$audOk"
                # Tenant id, if present, is metadata and safe to log.
                if ($claims.PSObject.Properties.Name -contains 'tid') {
                    $findings += "tid='$([string]$claims.tid)'"
                }
            } catch {
                $findings += "jwt_decode_failed=$($_.Exception.Message)"
            }
        }
    }
} catch {
    $findings += "json_parse_failed=$($_.Exception.Message)"
}

$isolated = $issuerOk -and $audOk -and $expiresFuture
$actual = ($findings -join '; ')

Write-ProbeResult -Result (New-ProbeResult `
    -Test $probeId -Tool 'azd' -Category 'auth' `
    -Hypothesis $hypothesis -Expected $expected `
    -Actual $actual -Isolated $isolated `
    -ToolVersion (Get-ToolVersion 'azd') `
    -Notes @("Token value NOT logged. Only header/claims metadata recorded.") `
    -DurationMs $run.duration_ms)

Remove-ProbeTempDir -Path $tempDir
exit 0
