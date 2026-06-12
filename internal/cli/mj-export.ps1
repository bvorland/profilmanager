# scripts/mj-export.ps1
#
# Exports the profile list from Majid Hajian's `mj` PowerShell CLI (profile.ps1)
# as JSON, so the Go `pm import-mj` command can consume it without ever
# parsing PowerShell itself.
#
# Usage:
#   pwsh -NoProfile -File scripts/mj-export.ps1 [-ProfileScript <path>]
#
# Output (stdout, single JSON object):
#   {
#     "source_script": "...",
#     "profiles_dir":  "C:\\Users\\xxx\\PSProfiles",
#     "profiles": [
#       {"name": "...", "label": "...", "color": "..."},
#       ...
#     ]
#   }
#
# Exit codes:
#   0 — success
#   2 — recoverable failure (missing file, regex miss, IEX error).
#       JSON is still emitted with an "error" field so callers can show it.
#
# Design notes:
#   - We DO NOT dot-source the full profile.ps1: it has side-effects
#     (oh-my-posh init, atuin init, alias re-binds) that would pollute
#     the runspace and slow us down.
#   - Instead we regex-locate the `$script:ProfilesList = @(...)` block,
#     walk its balanced parens, and Invoke-Expression ONLY that block.
#     The IEX evaluates plain hashtable literals; no commands run.

[CmdletBinding()]
param(
    [string]$ProfileScript
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

function Write-JsonAndExit {
    param(
        [Parameter(Mandatory)] [object] $Object,
        [int] $ExitCode = 0
    )
    ($Object | ConvertTo-Json -Depth 8 -Compress)
    exit $ExitCode
}

function Resolve-DefaultProfileScript {
    $candidates = @(
        (Join-Path $HOME 'OneDrive - Microsoft\Documents\PowerShell\profile.ps1'),
        (Join-Path $HOME 'OneDrive - Microsoft\Documents\WindowsPowerShell\profile.ps1'),
        (Join-Path $HOME 'Documents\PowerShell\profile.ps1'),
        (Join-Path $HOME 'Documents\WindowsPowerShell\profile.ps1')
    )
    foreach ($c in $candidates) {
        if (Test-Path -LiteralPath $c) { return $c }
    }
    return $candidates[0]
}

function Resolve-ProfilesDir {
    if ($env:HOME) {
        $cand = Join-Path $env:HOME 'PSProfiles'
        if (Test-Path -LiteralPath $cand) { return $cand }
    }
    return (Join-Path $HOME 'PSProfiles')
}

# Walks the source text starting at `$script:ProfilesList = @(` and returns
# the substring that begins at the `$` and ends at the matching `)`,
# correctly skipping comments and string literals so unbalanced parens
# inside a string don't fool us.
function Get-ProfilesListBlock {
    param([Parameter(Mandatory)][string] $Text)

    $rx = [regex]'(?ms)\$script:ProfilesList\s*=\s*@\('
    $m = $rx.Match($Text)
    if (-not $m.Success) { return $null }

    $startIndex = $m.Index
    $i = $m.Index + $m.Length - 1   # index of the opening '('
    $n = $Text.Length
    $depth = 0
    $inDouble = $false
    $inSingle = $false

    while ($i -lt $n) {
        $c = $Text[$i]

        if ($inDouble) {
            if ($c -eq '`' -and ($i + 1) -lt $n) { $i += 2; continue }
            if ($c -eq '"') { $inDouble = $false; $i++; continue }
            $i++; continue
        }

        if ($inSingle) {
            if ($c -eq "'") {
                if (($i + 1) -lt $n -and $Text[$i + 1] -eq "'") { $i += 2; continue }
                $inSingle = $false
            }
            $i++; continue
        }

        if ($c -eq '#') {
            while ($i -lt $n -and $Text[$i] -ne "`n") { $i++ }
            continue
        }
        if ($c -eq '"') { $inDouble = $true;  $i++; continue }
        if ($c -eq "'") { $inSingle = $true;  $i++; continue }
        if ($c -eq '(') { $depth++;            $i++; continue }
        if ($c -eq ')') {
            $depth--
            if ($depth -eq 0) {
                return $Text.Substring($startIndex, $i - $startIndex + 1)
            }
            $i++; continue
        }
        $i++
    }
    return $null
}

if (-not $ProfileScript) {
    $ProfileScript = Resolve-DefaultProfileScript
}

$profilesDir = Resolve-ProfilesDir

if (-not (Test-Path -LiteralPath $ProfileScript)) {
    Write-JsonAndExit -Object ([ordered]@{
        source_script = $ProfileScript
        profiles_dir  = $profilesDir
        profiles      = @()
        error         = "profile script not found: $ProfileScript"
    }) -ExitCode 2
}

try {
    $raw = Get-Content -LiteralPath $ProfileScript -Raw -ErrorAction Stop
} catch {
    Write-JsonAndExit -Object ([ordered]@{
        source_script = $ProfileScript
        profiles_dir  = $profilesDir
        profiles      = @()
        error         = "read failed: $($_.Exception.Message)"
    }) -ExitCode 2
}

$block = Get-ProfilesListBlock -Text $raw
if (-not $block) {
    Write-JsonAndExit -Object ([ordered]@{
        source_script = $ProfileScript
        profiles_dir  = $profilesDir
        profiles      = @()
        error         = '$script:ProfilesList = @(...) block not found in source'
    }) -ExitCode 2
}

# IEX only the assignment we extracted. It declares $script:ProfilesList
# in *this* script's scope; we read it back below.
try {
    Invoke-Expression -Command $block
} catch {
    Write-JsonAndExit -Object ([ordered]@{
        source_script = $ProfileScript
        profiles_dir  = $profilesDir
        profiles      = @()
        error         = "evaluate failed: $($_.Exception.Message)"
    }) -ExitCode 2
}

if (-not (Get-Variable -Name ProfilesList -Scope Script -ErrorAction SilentlyContinue)) {
    Write-JsonAndExit -Object ([ordered]@{
        source_script = $ProfileScript
        profiles_dir  = $profilesDir
        profiles      = @()
        error         = '$script:ProfilesList was not defined after evaluation'
    }) -ExitCode 2
}

$list = $script:ProfilesList
if ($null -eq $list) {
    Write-JsonAndExit -Object ([ordered]@{
        source_script = $ProfileScript
        profiles_dir  = $profilesDir
        profiles      = @()
        error         = '$script:ProfilesList evaluated to $null'
    }) -ExitCode 2
}

$normalized = New-Object System.Collections.Generic.List[object]
foreach ($p in $list) {
    if ($null -eq $p) { continue }
    if ($p -is [System.Collections.IDictionary]) {
        $normalized.Add([ordered]@{
            name  = [string]$p['name']
            label = [string]$p['label']
            color = [string]$p['color']
        }) | Out-Null
    }
}

Write-JsonAndExit -Object ([ordered]@{
    source_script = $ProfileScript
    profiles_dir  = $profilesDir
    profiles      = $normalized.ToArray()
}) -ExitCode 0
