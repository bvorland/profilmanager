#!/usr/bin/env pwsh
# probe-az-msal-cache.ps1
#
# Looks for MSAL token cache artifacts that should land under AZURE_CONFIG_DIR
# but historically also appear in %LOCALAPPDATA%\.IdentityService (WAM broker)
# and in Windows Credential Manager. Also reports whether the per-config-dir
# 'core.enable_broker_on_windows' setting is on — that flag is what flips
# az from "file cache under $T" to "OS broker cache (WAM) shared per-user".

. $PSScriptRoot/_common.ps1

$probeId    = 'az-msal-cache'
$hypothesis = 'MSAL token cache files live under AZURE_CONFIG_DIR; WAM broker, if enabled, leaks tokens out of $T to a per-user broker store'
$expected   = "Locate MSAL artifacts; report broker setting; identify any caches outside `$AZURE_CONFIG_DIR"

if (-not (Test-CommandAvailable 'az')) {
    Write-ProbeResult -Result (New-ProbeResult `
        -Test $probeId -Tool 'az' -Category 'token-cache' `
        -Hypothesis $hypothesis -Expected $expected `
        -Skipped $true -SkipReason 'az CLI not found on PATH')
    exit 0
}

$homeAzure = Get-HomeAzureDir
$findings = @()

$cachePatterns = @(
    'msal_token_cache.bin',
    'msal_token_cache.json',
    'msal_extension_cache.json',
    'msal_http_cache.bin',
    'service_principal_entries.json'
)

$inHomeAzure = @()
if (Test-Path $homeAzure) {
    foreach ($p in $cachePatterns) {
        $hits = Get-ChildItem -Path $homeAzure -Recurse -Filter $p -ErrorAction SilentlyContinue
        foreach ($h in $hits) { $inHomeAzure += $h.FullName }
    }
}
$findings += "homeAzureCacheFiles=$(@($inHomeAzure).Count)"

# Windows broker / WAM
$wamPaths = @()
if ($IsWindows) {
    $idSvc = Join-Path $env:LOCALAPPDATA '.IdentityService'
    if (Test-Path $idSvc) {
        $wamPaths += $idSvc
        $findings += "wam_identity_service_present=true"
    } else {
        $findings += "wam_identity_service_present=false"
    }
}

# Broker setting per-config-dir
$brokerEnabled = $null
$runConfig = Invoke-ProbeCommand -Command 'az' -Arguments @('config','get','core.enable_broker_on_windows','--output','json') `
                                 -TimeoutSeconds 20
if ($runConfig.exit_code -eq 0 -and $runConfig.stdout.Trim() -ne '' -and -not (Test-AzNotLoggedIn -Text $runConfig.stderr)) {
    try {
        $parsed = $runConfig.stdout | ConvertFrom-Json -ErrorAction Stop
        $brokerEnabled = [string]$parsed.value
    } catch {
        $brokerEnabled = '(parse-failed)'
    }
}
$findings += "core.enable_broker_on_windows=$brokerEnabled"

$notes = @(
    "If broker is enabled on Windows, tokens flow through WAM (%LOCALAPPDATA%\.IdentityService) and are SHARED per-user across AZURE_CONFIG_DIR values.",
    "To force per-dir token isolation on Windows, set 'core.enable_broker_on_windows=false' before each 'az login' in a profiled shell."
)
if ($null -eq $brokerEnabled) {
    $notes += "Broker setting was not readable; default in az >= 2.61 is ENABLED on Windows. Treat as a leak risk unless verified."
}

# Strict isolation requires broker OFF AND no MSAL caches in $HOME/.azure that escape $T.
$brokerSafe = ($brokerEnabled -eq 'False' -or $brokerEnabled -eq 'false' -or $brokerEnabled -eq '0')
$isolated = $null
if ($IsWindows) {
    # Descriptive on Windows — true isolation requires more than this probe can verify dry.
    $isolated = $null
    $notes += "Windows: this probe is DESCRIPTIVE only. Pair with probe-windows-wam-presence and probe-windows-credman-keys."
} else {
    # On macOS/Linux: brokered auth not a factor; isolated if no caches in $HOME/.azure leak.
    $isolated = ($inHomeAzure.Count -eq 0) -or $brokerSafe
}

$actual = ($findings -join '; ')
if ($wamPaths.Count -gt 0) { $actual += "; wamPaths=[$($wamPaths -join ', ')]" }

Write-ProbeResult -Result (New-ProbeResult `
    -Test $probeId -Tool 'az' -Category 'token-cache' `
    -Hypothesis $hypothesis -Expected $expected `
    -Actual $actual -Isolated $isolated `
    -ToolVersion (Get-ToolVersion 'az') `
    -Notes $notes -DurationMs $runConfig.duration_ms)

exit 0
