<#
.SYNOPSIS
  One-shot installer for profilmanager (pm) on Windows.

.DESCRIPTION
  Downloads the matching release archive from GitHub, verifies its
  SHA-256 against checksums.txt, extracts pm.exe to the install dir,
  and (optionally) adds the install dir to the user's PATH.

.PARAMETER Version
  Release tag to install (default: latest). Example: -Version v0.1.0

.PARAMETER InstallDir
  Directory pm.exe is installed into. Default:
  $env:LOCALAPPDATA\Programs\profilmanager

.PARAMETER NoPath
  Skip the user-PATH update step.

.PARAMETER DryRun
  Print the steps but do not download or write anything.

.EXAMPLE
  irm https://raw.githubusercontent.com/bvorland/profilmanager/main/install.ps1 | iex
.EXAMPLE
  .\install.ps1 -Version v0.1.0
.EXAMPLE
  .\install.ps1 -InstallDir 'C:\Tools\pm' -NoPath
#>
[CmdletBinding()]
param(
  [string]$Version = '',
  [string]$InstallDir = "$env:LOCALAPPDATA\Programs\profilmanager",
  [switch]$NoPath,
  [switch]$DryRun
)

$ErrorActionPreference = 'Stop'
$Repo = 'bvorland/profilmanager'
$BinName = 'pm.exe'

function Step([string]$msg) { Write-Host "install.ps1: $msg" }
function Do-Action([string]$desc, [scriptblock]$action) {
  if ($DryRun) { Write-Host "  + $desc" } else { & $action }
}

# Architecture detection: PROCESSOR_ARCHITECTURE is 'AMD64' or 'ARM64' on
# native shells. WOW64 shells expose 'AMD64' in PROCESSOR_ARCHITEW6432.
$archEnv = $env:PROCESSOR_ARCHITEW6432
if ([string]::IsNullOrEmpty($archEnv)) { $archEnv = $env:PROCESSOR_ARCHITECTURE }
switch -Regex ($archEnv) {
  'ARM64' { $Arch = 'arm64' }
  'AMD64|x86_64' { $Arch = 'amd64' }
  default { throw "Unsupported architecture: $archEnv" }
}
$OS = 'windows'

if ([string]::IsNullOrEmpty($Version)) {
  Step "Resolving latest release of $Repo ..."
  # /releases/latest 302-redirects to the actual /tag/<version> URL; we
  # follow only to read the final path.
  $resp = Invoke-WebRequest -UseBasicParsing -MaximumRedirection 5 `
    -Uri "https://github.com/$Repo/releases/latest"
  $Version = ($resp.BaseResponse.ResponseUri.AbsolutePath -split '/')[-1]
  if (-not $Version) { throw "Could not resolve latest version" }
}
$VerNoV = $Version.TrimStart('v')
Step "Installing pm $Version for $OS/$Arch into $InstallDir"

$Archive  = "pm_${VerNoV}_${OS}_${Arch}.zip"
$BaseUrl  = "https://github.com/$Repo/releases/download/$Version"
$Tmp      = Join-Path ([System.IO.Path]::GetTempPath()) ("pm-install-" + [Guid]::NewGuid().ToString('N'))

Do-Action "mkdir $Tmp" { New-Item -ItemType Directory -Force -Path $Tmp | Out-Null }

try {
  $ArchivePath  = Join-Path $Tmp $Archive
  $ChecksumPath = Join-Path $Tmp 'checksums.txt'

  Do-Action "download $BaseUrl/$Archive" {
    Invoke-WebRequest -UseBasicParsing -Uri "$BaseUrl/$Archive"      -OutFile $ArchivePath
  }
  Do-Action "download $BaseUrl/checksums.txt" {
    Invoke-WebRequest -UseBasicParsing -Uri "$BaseUrl/checksums.txt" -OutFile $ChecksumPath
  }

  if (-not $DryRun) {
    Step "Verifying SHA-256 checksum ..."
    $expected = (Get-Content $ChecksumPath | Where-Object { $_ -match "  $([regex]::Escape($Archive))$" } |
                 ForEach-Object { ($_ -split '\s+')[0] }) | Select-Object -First 1
    if (-not $expected) { throw "No checksum entry for $Archive in checksums.txt" }
    $actual = (Get-FileHash -Algorithm SHA256 -Path $ArchivePath).Hash.ToLower()
    if ($actual -ne $expected.ToLower()) {
      throw "Checksum mismatch for ${Archive}: expected $expected, got $actual"
    }
  }

  Do-Action "expand $Archive" { Expand-Archive -Force -Path $ArchivePath -DestinationPath $Tmp }
  Do-Action "mkdir $InstallDir" { New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null }
  Do-Action "move pm.exe -> $InstallDir" {
    Move-Item -Force -Path (Join-Path $Tmp $BinName) -Destination (Join-Path $InstallDir $BinName)
  }
}
finally {
  if (-not $DryRun -and (Test-Path $Tmp)) { Remove-Item -Recurse -Force $Tmp }
}

Step "Installed $(Join-Path $InstallDir $BinName)"

if (-not $NoPath) {
  $userPath = [Environment]::GetEnvironmentVariable('PATH', 'User')
  $parts = if ($userPath) { $userPath.Split(';') } else { @() }
  if ($parts -notcontains $InstallDir) {
    Do-Action "add $InstallDir to user PATH" {
      $newPath = (@($InstallDir) + $parts) -join ';'
      [Environment]::SetEnvironmentVariable('PATH', $newPath, 'User')
    }
    Step "Open a new terminal to pick up the updated PATH."
  }
}

@"

Next: enable shell integration so PM_SESSION_ID is bound for every shell.
  PowerShell (current session):
    Invoke-Expression (& pm session init --shell pwsh)
  PowerShell (persistent):
    Add 'Invoke-Expression (& pm session init --shell pwsh)' to `$PROFILE
  cmd.exe:
    pm session init --shell cmd  (then follow the printed instructions)

Run 'pm doctor' to verify your environment.
"@
