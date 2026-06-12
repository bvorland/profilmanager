# scripts/isolation/_common.ps1
#
# Shared helpers for all probe-*.ps1 scripts.
# Dot-source from each probe: . $PSScriptRoot/_common.ps1
#
# Contract:
#   - Every probe emits exactly one JSON object to stdout.
#   - Every probe exits 0.
#   - No writes outside $env:TEMP\pm-isolation-<probeId>-<pid>\.
#   - Live-cloud calls are detected and skipped (never failed).

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$script:ProbeStartedAt = [datetime]::UtcNow

function Get-IsoTimestamp {
    [datetime]::UtcNow.ToString('o')
}

function Get-HostInfo {
    $osName = if ($IsWindows) { 'windows' }
              elseif ($IsLinux) { 'linux' }
              elseif ($IsMacOS) { 'macos' }
              else { 'unknown' }

    $arch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToLowerInvariant()
    $osRelease = try { [System.Environment]::OSVersion.Version.ToString() } catch { 'unknown' }
    $pwshVersion = $PSVersionTable.PSVersion.ToString()

    [pscustomobject]@{
        os           = $osName
        os_release   = $osRelease
        arch         = $arch
        pwsh_version = $pwshVersion
    }
}

function Test-CommandAvailable {
    param([Parameter(Mandatory)][string]$Name)
    $null -ne (Get-Command $Name -ErrorAction SilentlyContinue)
}

function Get-ToolVersion {
    param([Parameter(Mandatory)][string]$Name)
    if (-not (Test-CommandAvailable $Name)) { return $null }
    try {
        # Capture stderr->stdout, force everything to strings via Out-String so ErrorRecord WARNING lines don't bleed type info.
        $raw = & $Name --version *>&1 2>&1 | Out-String
        $lines = $raw -split "`r?`n" | ForEach-Object { $_.Trim() } | Where-Object { $_ -ne '' }
        # Prefer a line that mentions the tool name and a version-like token.
        $versionPattern = '\b\d+\.\d+(\.\d+)?'
        foreach ($line in $lines) {
            if ($line -match $versionPattern -and ($line -notmatch '^WARNING' -and $line -notmatch '^ERROR')) {
                return $line
            }
        }
        # Fallback: first non-empty line.
        if ($lines.Count -gt 0) { return $lines[0] }
        return $null
    } catch {
        return $null
    }
}

function New-ProbeTempDir {
    param([Parameter(Mandatory)][string]$ProbeId)
    $root = Join-Path $env:TEMP "pm-isolation-$ProbeId-$PID"
    if (Test-Path $root) { Remove-Item $root -Recurse -Force -ErrorAction SilentlyContinue }
    New-Item -ItemType Directory -Path $root -Force | Out-Null
    return $root
}

function Remove-ProbeTempDir {
    param([Parameter(Mandatory)][string]$Path)
    if (Test-Path $Path) {
        Remove-Item $Path -Recurse -Force -ErrorAction SilentlyContinue
    }
}

function Invoke-WithTimeout {
    <#
        Run a script block with a hard timeout in seconds. Returns:
          @{ stdout=...; stderr=...; exit_code=...; timed_out=$true/$false; duration_ms=int }
        Never throws.
    #>
    param(
        [Parameter(Mandatory)][scriptblock]$ScriptBlock,
        [int]$TimeoutSeconds = 15
    )
    $sw = [System.Diagnostics.Stopwatch]::StartNew()
    $job = Start-Job -ScriptBlock $ScriptBlock
    $completed = Wait-Job $job -Timeout $TimeoutSeconds
    $timedOut = $null -eq $completed
    if ($timedOut) {
        Stop-Job $job -ErrorAction SilentlyContinue | Out-Null
        Remove-Job $job -Force -ErrorAction SilentlyContinue | Out-Null
        $sw.Stop()
        return @{
            stdout      = ''
            stderr      = ''
            exit_code   = $null
            timed_out   = $true
            duration_ms = [int]$sw.ElapsedMilliseconds
        }
    }
    $stdout = ''
    $stderr = ''
    try {
        $raw = Receive-Job $job -ErrorAction SilentlyContinue 6>&1 2>&1
        if ($null -ne $raw) { $stdout = ($raw | Out-String) }
    } catch {
        $stderr = $_.Exception.Message
    }
    Remove-Job $job -Force -ErrorAction SilentlyContinue | Out-Null
    $sw.Stop()
    return @{
        stdout      = $stdout
        stderr      = $stderr
        exit_code   = 0
        timed_out   = $false
        duration_ms = [int]$sw.ElapsedMilliseconds
    }
}

function Invoke-ProbeCommand {
    <#
        Run a real external command (az, azd) with env-var overrides, capturing stdout+stderr+exit code.
        Times out at $TimeoutSeconds. Never throws.
        Returns @{ stdout; stderr; exit_code; timed_out; duration_ms }.
    #>
    param(
        [Parameter(Mandatory)][string]$Command,
        [string[]]$Arguments = @(),
        [hashtable]$EnvOverrides = @{},
        [int]$TimeoutSeconds = 15
    )
    $sw = [System.Diagnostics.Stopwatch]::StartNew()

    $stdoutFile = [System.IO.Path]::GetTempFileName()
    $stderrFile = [System.IO.Path]::GetTempFileName()

    $oldValues = @{}
    foreach ($k in $EnvOverrides.Keys) {
        $oldValues[$k] = [System.Environment]::GetEnvironmentVariable($k)
        [System.Environment]::SetEnvironmentVariable($k, $EnvOverrides[$k])
    }

    $timedOut = $false
    $exit = $null
    $launchError = $null
    try {
        $startArgs = @{
            FilePath               = $Command
            NoNewWindow            = $true
            RedirectStandardOutput = $stdoutFile
            RedirectStandardError  = $stderrFile
            PassThru               = $true
        }
        if ($Arguments -and $Arguments.Count -gt 0) {
            $startArgs['ArgumentList'] = $Arguments
        }
        $proc = Start-Process @startArgs
        if (-not $proc.WaitForExit($TimeoutSeconds * 1000)) {
            try { $proc.Kill($true) } catch { }
            $timedOut = $true
        } else {
            $exit = $proc.ExitCode
        }
    } catch {
        $launchError = "Failed to launch ${Command}: $($_.Exception.Message)"
    } finally {
        foreach ($k in $oldValues.Keys) { [System.Environment]::SetEnvironmentVariable($k, $oldValues[$k]) }
    }

    $sw.Stop()

    $stdout = ''
    if (Test-Path $stdoutFile) {
        $tmp = Get-Content $stdoutFile -Raw -ErrorAction SilentlyContinue
        if ($null -ne $tmp) { $stdout = $tmp }
    }
    $stderr = ''
    if (Test-Path $stderrFile) {
        $tmp = Get-Content $stderrFile -Raw -ErrorAction SilentlyContinue
        if ($null -ne $tmp) { $stderr = $tmp }
    }
    if ($launchError) { $stderr = $launchError }
    Remove-Item $stdoutFile, $stderrFile -Force -ErrorAction SilentlyContinue

    return @{
        stdout      = $stdout
        stderr      = $stderr
        exit_code   = $exit
        timed_out   = $timedOut
        duration_ms = [int]$sw.ElapsedMilliseconds
    }
}

function Test-AzNotLoggedIn {
    <# Returns $true if the stderr/stdout text looks like a "please run az login" message. #>
    param([string]$Text)
    if ([string]::IsNullOrWhiteSpace($Text)) { return $false }
    $patterns = @(
        'Please run .az login.',
        'AADSTS50173',
        'No subscription found',
        'az login --use-device-code',
        'ERROR: Please run .az login. to set up an account'
    )
    foreach ($p in $patterns) { if ($Text -match $p) { return $true } }
    return $false
}

function Test-AzdNotLoggedIn {
    param([string]$Text)
    if ([string]::IsNullOrWhiteSpace($Text)) { return $false }
    $patterns = @(
        'not logged in',
        'fetching token: failed',
        'azd auth login',
        'no credentials configured'
    )
    foreach ($p in $patterns) { if ($Text -match $p) { return $true } }
    return $false
}

function New-ProbeResult {
    <#
        Build the canonical probe JSON envelope. Caller fills in everything except host/generated_at/duration_ms/probe_version,
        which are auto-populated.
    #>
    param(
        [Parameter(Mandatory)][string]$Test,
        [Parameter(Mandatory)][string]$Tool,
        [Parameter(Mandatory)][string]$Category,
        [Parameter(Mandatory)][string]$Hypothesis,
        [Parameter(Mandatory)][string]$Expected,
        [string]$Actual = '',
        [object]$Isolated = $null,
        [bool]$Skipped = $false,
        [object]$SkipReason = $null,
        [object]$ToolVersion = $null,
        [string[]]$Notes = @(),
        [int]$DurationMs = -1
    )

    if ($DurationMs -lt 0) {
        $DurationMs = [int]([datetime]::UtcNow - $script:ProbeStartedAt).TotalMilliseconds
    }

    $isolatedOut = if ($null -eq $Isolated) { $null } else { [bool]$Isolated }
    $skipReasonOut = if ([string]::IsNullOrEmpty([string]$SkipReason)) { $null } else { [string]$SkipReason }
    $toolVersionOut = if ([string]::IsNullOrEmpty([string]$ToolVersion)) { $null } else { [string]$ToolVersion }

    [pscustomobject]@{
        test         = $Test
        tool         = $Tool
        category     = $Category
        hypothesis   = $Hypothesis
        expected     = $Expected
        actual       = $Actual
        isolated     = $isolatedOut
        skipped      = $Skipped
        skip_reason  = $skipReasonOut
        duration_ms  = $DurationMs
        notes        = @($Notes)
        host         = Get-HostInfo
        tool_version = $toolVersionOut
        probe_version = '1.0.0'
        generated_at  = (Get-IsoTimestamp)
    }
}

function Write-ProbeResult {
    param([Parameter(Mandatory)][pscustomobject]$Result)
    $Result | ConvertTo-Json -Depth 10 -Compress:$false
}

function Get-HomeAzureDir {
    if ($IsWindows) {
        return Join-Path $env:USERPROFILE '.azure'
    }
    return Join-Path $env:HOME '.azure'
}

function Get-HomeAzdDir {
    if ($IsWindows) {
        return Join-Path $env:USERPROFILE '.azd'
    }
    return Join-Path $env:HOME '.azd'
}

function Get-DirMtime {
    param([Parameter(Mandatory)][string]$Path)
    if (-not (Test-Path $Path)) { return $null }
    try {
        return (Get-Item $Path -Force).LastWriteTimeUtc
    } catch {
        return $null
    }
}

function Get-DirSnapshot {
    <#
        Light snapshot of a directory: top-level file/dir names and their LastWriteTimeUtc.
        Used for before/after change detection. Always returns an array (possibly empty).
    #>
    param([Parameter(Mandatory)][string]$Path)
    if (-not (Test-Path $Path)) { return @() }
    try {
        $items = Get-ChildItem $Path -Force -ErrorAction SilentlyContinue
        if (-not $items) { return @() }
        return @($items | Select-Object @{n='name';e={$_.Name}}, @{n='mtime';e={$_.LastWriteTimeUtc.ToString('o')}}, @{n='is_dir';e={$_.PSIsContainer}})
    } catch {
        return @()
    }
}
