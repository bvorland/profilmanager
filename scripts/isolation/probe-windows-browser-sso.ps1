#!/usr/bin/env pwsh
# probe-windows-browser-sso.ps1
#
# Descriptive (isolated=null). Reports the default browser registration on
# Windows. 'az login' (without --use-device-code) opens the default browser,
# which persists login.microsoftonline.com cookies independent of
# AZURE_CONFIG_DIR.

. $PSScriptRoot/_common.ps1

$probeId    = 'windows-browser-sso'
$hypothesis = 'default-browser cookie state for login.microsoftonline.com persists across az login invocations regardless of AZURE_CONFIG_DIR'
$expected   = 'descriptive — report default browser, flag that browser SSO can auto-select previously-signed-in accounts'

if (-not $IsWindows) {
    Write-ProbeResult -Result (New-ProbeResult `
        -Test $probeId -Tool 'windows' -Category 'browser' `
        -Hypothesis $hypothesis -Expected $expected `
        -Skipped $true -SkipReason 'not running on Windows')
    exit 0
}

$defaultBrowser = '(unknown)'
try {
    $regPath = 'HKCU:\SOFTWARE\Microsoft\Windows\Shell\Associations\UrlAssociations\https\UserChoice'
    if (Test-Path $regPath) {
        $progId = (Get-ItemProperty -Path $regPath -ErrorAction SilentlyContinue).ProgId
        if ($progId) { $defaultBrowser = $progId }
    }
} catch {
    $defaultBrowser = "(read failed: $($_.Exception.Message))"
}

$notes = @(
    "Browser SSO is a known leak vector: two 'az login' invocations with different AZURE_CONFIG_DIRs hit the same OS browser cookie jar.",
    "Mitigation: 'pm' should default 'az login' to '--use-device-code' for profiles that map to distinct identities, to force conscious account selection."
)

$actual = "defaultBrowserProgId='$defaultBrowser'"

Write-ProbeResult -Result (New-ProbeResult `
    -Test $probeId -Tool 'windows' -Category 'browser' `
    -Hypothesis $hypothesis -Expected $expected `
    -Actual $actual -Isolated $null `
    -Notes $notes)

exit 0
