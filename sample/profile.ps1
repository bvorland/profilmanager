# ===== MJ CLI — Personal Command Hub =====
#
# Origin: this script was authored by Majid Hajian (https://github.com/MajidHajian)
# and is the direct inspiration for profilmanager (`pm`). It's preserved here
# verbatim (with profile names genericized for the public repo) so anyone
# coming from the `mj` workflow can compare the mental model.
#
# `pm import-mj --from-powershell sample/profile.ps1` migrates the
# $script:ProfilesList shape below into pm's TOML profiles losslessly.
#
$script:ProfilesList = @(
  @{name="Contoso.Dev";              label="🔵 Contoso Dev";              color="Cyan"}
  @{name="Contoso.Backend";          label="🟣 Contoso Backend";          color="Magenta"}
  @{name="Contoso.Pipeline";         label="🟡 Contoso Pipeline";         color="Yellow"}
  @{name="Fabrikam.Work";            label="🔴 Fabrikam Work";            color="Red"}
  @{name="Fabrikam.External";        label="🟠 Fabrikam External";        color="DarkYellow"}
  @{name="Fabrikam.Personal";        label="🟢 Fabrikam Personal";        color="Green"}
  @{name="Contoso.Personal";         label="⚪ Contoso Personal";         color="White"}
  @{name="Fabrikam.Sub1";            label="🟠 Fabrikam Sub1";            color="DarkYellow"}
  @{name="Fabrikam.Demo";            label="🔵 Fabrikam Demo";            color="Cyan"}
)

function script:Resolve-Profile {
  param([string]$Value)
  if ([int]::TryParse($Value, [ref]$null)) {
    $idx = [int]$Value - 1
    if ($idx -ge 0 -and $idx -lt $script:ProfilesList.Count) { return $script:ProfilesList[$idx] }
  }
  foreach ($p in $script:ProfilesList) { if ($p.name -eq $Value) { return $p } }
  return $null
}

function script:Set-ProfileEnv {
  param([hashtable]$Profile)
  $env:AZURE_CONFIG_DIR = "$HOME\.azure-$($Profile.name)"
  $env:AZD_CONFIG_DIR = "$HOME\.azd-$($Profile.name)"
  $env:AZURE_PROFILE_NAME = $Profile.name
  if (-not (Test-Path $env:AZD_CONFIG_DIR)) { New-Item -ItemType Directory -Path $env:AZD_CONFIG_DIR -Force | Out-Null }
  Set-Content -Path "$HOME\PSProfiles\.last-profile" -Value $Profile.name -Encoding UTF8
}

function script:Test-HasOpSecrets {
  param([string]$ProfileName)
  $envFile = "$HOME\PSProfiles\$ProfileName.env"
  if (-not (Test-Path $envFile)) { return $false }
  foreach ($line in (Get-Content $envFile)) {
    if ($line -match '^\s*[^#].*=op://') { return $true }
  }
  return $false
}

function script:Switch-ProfileSmart {
  param([hashtable]$Profile)
  script:Set-ProfileEnv $Profile
  if (script:Test-HasOpSecrets $Profile.name) {
    $envFile = "$HOME\PSProfiles\$($Profile.name).env"
    Write-Host "  🔐 Secrets detected — launching 1Password session..." -ForegroundColor $($Profile.color)
    op run --env-file=$envFile -- pwsh -NoExit -Command "`$env:AZURE_PROFILE_NAME = '$($Profile.name)'; `$env:AZURE_CONFIG_DIR = '$HOME\.azure-$($Profile.name)'; `$env:AZD_CONFIG_DIR = '$HOME\.azd-$($Profile.name)'; Write-Host '  $($Profile.label) active (1Password)' -ForegroundColor $($Profile.color)"
  } else {
    Write-Host "  $($Profile.label) activated" -ForegroundColor $($Profile.color)
  }
}

function script:Show-ProfileList {
  param([switch]$Compact)
  Write-Host ""
  for ($i = 0; $i -lt $script:ProfilesList.Count; $i++) {
    $p = $script:ProfilesList[$i]
    $num = $i + 1
    $active = if ($env:AZURE_PROFILE_NAME -eq $p.name) { " ◄" } else { "" }
    Write-Host "  [$num] $($p.label)$active" -ForegroundColor $($p.color)
  }
  if (-not $Compact) {
    Write-Host "  ─────────────────────────────────────" -ForegroundColor DarkGray
    Write-Host "  [0] Reset to default (~/.azure)" -ForegroundColor DarkGray
  }
  Write-Host ""
}

function script:Pick-Profile {
  script:Show-ProfileList
  $choice = Read-Host "  Pick a number"
  if ($choice -eq "0") {
    Remove-Item Env:\AZURE_CONFIG_DIR -ErrorAction SilentlyContinue
    Remove-Item Env:\AZD_CONFIG_DIR -ErrorAction SilentlyContinue
    Remove-Item Env:\AZURE_PROFILE_NAME -ErrorAction SilentlyContinue
    Remove-Item "$HOME\PSProfiles\.last-profile" -ErrorAction SilentlyContinue
    Write-Host "  Reset to default Azure config" -ForegroundColor Yellow
    return $null
  }
  $p = script:Resolve-Profile $choice
  if (-not $p) { Write-Host "  Invalid selection" -ForegroundColor Red; return $null }
  return $p
}

function Invoke-MJ {
  param(
    [Parameter(Position=0)][string]$Command,
    [Parameter(Position=1, ValueFromRemainingArguments)][string[]]$Rest
  )
  switch ($Command) {
    {$_ -in 'switch','sw','s'} {
      $pick = if ($Rest) { $Rest[0] } else { $null }
      if ($pick) {
        $p = script:Resolve-Profile $pick
        if (-not $p) { Write-Host "  Unknown profile: $pick" -ForegroundColor Red; return }
      } else {
        $p = script:Pick-Profile
        if (-not $p) { return }
      }
      script:Switch-ProfileSmart $p
    }
    {$_ -in 'secret','sec'} {
      $sub = if ($Rest) { $Rest[0] } else { $null }
      $profName = $env:AZURE_PROFILE_NAME
      if (-not $profName) { Write-Host "  No active profile. Switch first: mj s" -ForegroundColor Red; return }
      $envFile = "$HOME\PSProfiles\$profName.env"
      switch ($sub) {
        {$_ -in 'add','a'} {
          if ($Rest.Count -lt 3) {
            Write-Host ""
            Write-Host "  Usage: mj secret add <KEY> <VALUE>" -ForegroundColor Yellow
            Write-Host ""
            Write-Host "  Examples:" -ForegroundColor DarkGray
            Write-Host "    mj secret add GITHUB_TOKEN ghp_abc123..." -ForegroundColor DarkGray
            Write-Host "    mj secret add API_KEY op://MyVault/MyItem/password" -ForegroundColor DarkGray
            Write-Host ""
            Write-Host "  Plain values are stored directly." -ForegroundColor DarkGray
            Write-Host "  op:// values are resolved by 1Password on switch." -ForegroundColor DarkGray
            Write-Host ""
            return
          }
          $key = $Rest[1]; $val = ($Rest[2..($Rest.Count-1)]) -join ' '
          $lines = Get-Content $envFile
          $exists = $false
          $newLines = $lines | ForEach-Object {
            if ($_ -match "^$([regex]::Escape($key))=") { $exists = $true; "$key=$val" } else { $_ }
          }
          if ($exists) { $newLines | Set-Content $envFile -Encoding UTF8 }
          else { Add-Content $envFile -Value "$key=$val" -Encoding UTF8 }
          $type = if ($val -match '^op://') { "🔐 1Password ref" } else { "📝 plain value" }
          Write-Host "  ✅ $key added ($type)" -ForegroundColor Green
        }
        {$_ -in 'list','ls','l'} {
          Write-Host ""
          Write-Host "  Secrets in $profName" -ForegroundColor White
          Write-Host "  ─────────────────────────────────────" -ForegroundColor DarkGray
          $found = $false
          Get-Content $envFile | ForEach-Object {
            if ($_ -match '^\s*([^#=]\S*?)=(.+)$') {
              $k = $Matches[1]; $v = $Matches[2]
              if ($k -in 'AZURE_PROFILE_NAME','AZURE_CONFIG_DIR') { return }
              $found = $true
              if ($v -match '^op://') {
                Write-Host "  🔐 $k" -ForegroundColor Cyan -NoNewline
                Write-Host " = $v" -ForegroundColor DarkGray
              } else {
                Write-Host "  📝 $k" -ForegroundColor Cyan -NoNewline
                Write-Host " = $v" -ForegroundColor DarkGray
              }
            }
          }
          if (-not $found) { Write-Host "  (none)" -ForegroundColor DarkGray }
          Write-Host ""
        }
        {$_ -in 'remove','rm','r'} {
          if ($Rest.Count -lt 2) { Write-Host "  Usage: mj secret remove <KEY>" -ForegroundColor Yellow; return }
          $key = $Rest[1]
          $lines = Get-Content $envFile
          $escaped = [regex]::Escape($key)
          $newLines = $lines | Where-Object { $_ -notmatch "^$escaped=" }
          if ($lines.Count -eq $newLines.Count) { Write-Host "  Key '$key' not found" -ForegroundColor Yellow; return }
          $newLines | Set-Content $envFile -Encoding UTF8
          Write-Host "  🗑️ $key removed from $profName" -ForegroundColor Yellow
        }
        {$_ -in 'browse','b'} {
          Write-Host ""
          Write-Host "  Browsing 1Password vaults..." -ForegroundColor Cyan
          Write-Host ""
          try {
            $vaults = op vault list --format=json 2>$null | ConvertFrom-Json
            if (-not $vaults) { Write-Host "  No vaults found. Run 'op signin' first." -ForegroundColor Red; return }
            for ($i = 0; $i -lt $vaults.Count; $i++) {
              Write-Host "  [$($i+1)] $($vaults[$i].name)" -ForegroundColor White
            }
            Write-Host ""
            $vc = Read-Host "  Pick a vault (number) or Enter to cancel"
            if (-not $vc) { return }
            $vi = [int]$vc - 1
            if ($vi -lt 0 -or $vi -ge $vaults.Count) { Write-Host "  Invalid" -ForegroundColor Red; return }
            $vaultName = $vaults[$vi].name
            Write-Host ""
            Write-Host "  Items in '$vaultName':" -ForegroundColor Cyan
            $items = op item list --vault $vaultName --format=json 2>$null | ConvertFrom-Json
            foreach ($item in $items) {
              Write-Host "    $($item.title)" -ForegroundColor White -NoNewline
              Write-Host " ($($item.category))" -ForegroundColor DarkGray
            }
            Write-Host ""
            Write-Host "  To add a secret from this vault:" -ForegroundColor DarkGray
            Write-Host "    mj secret add MY_KEY op://$vaultName/<ItemName>/password" -ForegroundColor DarkGray
            Write-Host ""
            Write-Host "  Common fields: password, username, token, credential, notesPlain" -ForegroundColor DarkGray
            Write-Host ""
          }
          catch { Write-Host "  1Password CLI error. Make sure 'op' is installed and you're signed in." -ForegroundColor Red }
        }
        default {
          Write-Host ""
          Write-Host "  Secret Management" -ForegroundColor White
          Write-Host "    mj secret add KEY VALUE   " -ForegroundColor Cyan -NoNewline; Write-Host " Add any secret (plain or op://)" -ForegroundColor Gray
          Write-Host "    mj secret list            " -ForegroundColor Cyan -NoNewline; Write-Host " Show all secrets in profile" -ForegroundColor Gray
          Write-Host "    mj secret remove KEY      " -ForegroundColor Cyan -NoNewline; Write-Host " Remove a secret" -ForegroundColor Gray
          Write-Host "    mj secret browse          " -ForegroundColor Cyan -NoNewline; Write-Host " Browse 1Password vaults/items" -ForegroundColor Gray
          Write-Host ""
          Write-Host "  📝 = plain value    🔐 = 1Password (resolved on switch)" -ForegroundColor DarkGray
          Write-Host ""
        }
      }
    }
    {$_ -in 'profiles','list','ls','p'} { script:Show-ProfileList -Compact }
    {$_ -in 'whoami','who','w'} {
      Write-Host ""
      Write-Host "  ─── Profile ───" -ForegroundColor DarkGray
      if ($env:AZURE_PROFILE_NAME) {
        $p = script:Resolve-Profile $env:AZURE_PROFILE_NAME
        if ($p) { Write-Host "  Profile:  $($p.label)" -ForegroundColor $($p.color) }
        Write-Host "  Config:   $env:AZURE_CONFIG_DIR" -ForegroundColor DarkGray
      } else { Write-Host "  Profile:  (default)" -ForegroundColor Yellow; Write-Host "  Config:   ~/.azure" -ForegroundColor DarkGray }

      # --- Collect az info ---
      $azSubId = $null; $azSubName = $null; $azRg = $null; $azLoc = $null; $azUser = $null
      Write-Host ""
      Write-Host "  ─── Azure CLI (az) ───" -ForegroundColor DarkGray
      if (Get-Command az -ErrorAction SilentlyContinue) {
        try {
          $azJson = az account show -o json 2>$null
          if ($azJson) {
            $az = $azJson | ConvertFrom-Json
            $azUser = $az.user.name
            $azSubId = $az.id; $azSubName = $az.name
            Write-Host "  Account:        $azUser" -ForegroundColor Cyan
            Write-Host "  Tenant:         $($az.tenantId)" -ForegroundColor DarkGray
            Write-Host ""
            Write-Host "  Default Sub:    $azSubName" -ForegroundColor White
            Write-Host "                  $azSubId" -ForegroundColor DarkGray
            try {
              $defaults = az configure -l -o json 2>$null | ConvertFrom-Json
              $azRg = ($defaults | Where-Object { $_.name -eq 'group' }).value
              $azLoc = ($defaults | Where-Object { $_.name -eq 'location' }).value
              if ($azRg)  { Write-Host "  Resource Group: $azRg" -ForegroundColor White }
              if ($azLoc) { Write-Host "  Location:       $azLoc" -ForegroundColor White }
            } catch {}
            Write-Host ""
            Write-Host "  Subscriptions (your account):" -ForegroundColor White
            try {
              $allSubs = az account list --query "[].{name:name, id:id, isDefault:isDefault, userName:user.name}" -o json 2>$null | ConvertFrom-Json
              $mySubs = $allSubs | Where-Object { $_.userName -eq $azUser }
              if ($mySubs.Count -eq 0) { Write-Host "    (none for $azUser)" -ForegroundColor DarkGray }
              else {
                foreach ($s in $mySubs) {
                  $mark = if ($s.isDefault) { ' ◄' } else { '' }
                  $color = if ($s.isDefault) { 'Cyan' } else { 'Gray' }
                  Write-Host "    $($s.name)$mark" -ForegroundColor $color
                  Write-Host "    $($s.id)" -ForegroundColor DarkGray
                }
              }
            } catch { Write-Host "    (could not list)" -ForegroundColor DarkGray }
            Write-Host ""
            Write-Host "  Resource Groups (current sub):" -ForegroundColor White
            try {
              $rgs = az group list --query "[].{name:name, location:location}" -o json 2>$null | ConvertFrom-Json
              if (-not $rgs -or $rgs.Count -eq 0) { Write-Host "    (none)" -ForegroundColor DarkGray }
              else {
                foreach ($g in $rgs) {
                  Write-Host "    $($g.name)" -ForegroundColor Gray -NoNewline
                  Write-Host " ($($g.location))" -ForegroundColor DarkGray
                }
              }
            } catch { Write-Host "    (could not list)" -ForegroundColor DarkGray }
            Write-Host ""
            Write-Host "  Commands:" -ForegroundColor DarkGray
            Write-Host "    az account set -s <sub-id>                " -ForegroundColor DarkGray
            Write-Host "    az configure --defaults group=<rg> location=<loc>" -ForegroundColor DarkGray
          } else { Write-Host "  Not logged in" -ForegroundColor Yellow }
        } catch { Write-Host "  Not logged in" -ForegroundColor Yellow }
      } else { Write-Host "  az CLI not installed" -ForegroundColor Red }

      # --- Collect azd info ---
      $azdSubId = $null; $azdLoc = $null; $azdUser = $null
      Write-Host ""
      Write-Host "  ─── Azure Developer CLI (azd) ───" -ForegroundColor DarkGray
      if (Get-Command azd -ErrorAction SilentlyContinue) {
        try {
          $azdTokenJson = azd auth token --output json 2>$null
          if ($azdTokenJson) {
            $azdToken = $azdTokenJson | ConvertFrom-Json
            # Decode JWT payload to get user info
            try {
              $payload = $azdToken.token.Split('.')[1]
              # Fix base64url padding
              switch ($payload.Length % 4) { 2 { $payload += '==' } 3 { $payload += '=' } }
              $payload = $payload.Replace('-','+').Replace('_','/')
              $decoded = [System.Text.Encoding]::UTF8.GetString([System.Convert]::FromBase64String($payload)) | ConvertFrom-Json
              $azdUser = if ($decoded.upn) { $decoded.upn } elseif ($decoded.unique_name) { $decoded.unique_name } elseif ($decoded.email) { $decoded.email } else { $null }
            } catch {}
            if ($azdUser) { Write-Host "  Account:        $azdUser" -ForegroundColor Cyan }
            else { Write-Host "  Logged in" -ForegroundColor Cyan }
            if ($azdToken.expiresOn) { Write-Host "  Expires:        $($azdToken.expiresOn)" -ForegroundColor DarkGray }
            try {
              $azdCfgJson = azd config list -o json 2>$null
              if ($azdCfgJson) {
                $azdCfg = $azdCfgJson | ConvertFrom-Json
                $azdSubId = $azdCfg.defaults.subscription
                $azdLoc = $azdCfg.defaults.location
                if ($azdSubId) { Write-Host "  Subscription:   $azdSubId" -ForegroundColor White }
                if ($azdLoc)   { Write-Host "  Location:       $azdLoc" -ForegroundColor White }
              }
            } catch {}
            Write-Host ""
            Write-Host "  Commands:" -ForegroundColor DarkGray
            Write-Host "    azd config set defaults.subscription <sub-id>" -ForegroundColor DarkGray
            Write-Host "    azd config set defaults.location <location>   " -ForegroundColor DarkGray
          } else { Write-Host "  Not logged in" -ForegroundColor Yellow }
        } catch { Write-Host "  Not logged in" -ForegroundColor Yellow }
      } else { Write-Host "  azd CLI not installed" -ForegroundColor Red }

      # --- Mismatch warnings ---
      if ($azSubId -and $azdSubId -and ($azSubId -ne $azdSubId)) {
        Write-Host ""
        Write-Host "  ⚠️  SUBSCRIPTION MISMATCH" -ForegroundColor Red
        Write-Host "  az  → $azSubId" -ForegroundColor Yellow
        Write-Host "  azd → $azdSubId" -ForegroundColor Yellow
        Write-Host "  To align azd to az:  azd config set defaults.subscription $azSubId" -ForegroundColor DarkGray
        Write-Host "  To align az to azd:  az account set -s $azdSubId" -ForegroundColor DarkGray
      }
      if ($azUser -and $azdUser -and ($azUser -ne $azdUser)) {
        Write-Host ""
        Write-Host "  ⚠️  ACCOUNT MISMATCH" -ForegroundColor Red
        Write-Host "  az  → $azUser" -ForegroundColor Yellow
        Write-Host "  azd → $azdUser" -ForegroundColor Yellow
        Write-Host "  Run 'az login' or 'azd auth login' to align accounts." -ForegroundColor DarkGray
      }

      Write-Host ""
      Write-Host "  ─── 1Password CLI ───" -ForegroundColor DarkGray
      if (Get-Command op -ErrorAction SilentlyContinue) {
        try {
          $opJson = op whoami --format=json 2>$null
          if ($opJson) {
            $opUser = $opJson | ConvertFrom-Json
            Write-Host "  Account:  $($opUser.email)" -ForegroundColor Cyan
            Write-Host "  URL:      $($opUser.url)" -ForegroundColor DarkGray
          } else { Write-Host "  Not signed in (run 'op signin')" -ForegroundColor Yellow }
        } catch { Write-Host "  Not signed in (run 'op signin')" -ForegroundColor Yellow }
      } else { Write-Host "  op CLI not installed" -ForegroundColor Red }
      Write-Host ""
    }
    {$_ -in 'reset','r'} {
      Remove-Item Env:\AZURE_CONFIG_DIR -ErrorAction SilentlyContinue
      Remove-Item Env:\AZD_CONFIG_DIR -ErrorAction SilentlyContinue
      Remove-Item Env:\AZURE_PROFILE_NAME -ErrorAction SilentlyContinue
      Remove-Item "$HOME\PSProfiles\.last-profile" -ErrorAction SilentlyContinue
      Write-Host "  Reset to default Azure config (~/.azure, ~/.azd)" -ForegroundColor Yellow
    }
    {$_ -in 'edit','e'} {
      $pick = if ($Rest) { $Rest[0] } else { $env:AZURE_PROFILE_NAME }
      if (-not $pick) { Write-Host "  No active profile. Usage: mj edit <number>" -ForegroundColor Red; return }
      $p = script:Resolve-Profile $pick
      if (-not $p) { Write-Host "  Unknown profile: $pick" -ForegroundColor Red; return }
      code "$HOME\PSProfiles\$($p.name).env"
    }
    {$_ -in 'add','new','a'} {
      $colorMap = @{
        '1'=@{color='Cyan';     emoji='🔵'}; '2'=@{color='Magenta';    emoji='🟣'}
        '3'=@{color='Yellow';   emoji='🟡'}; '4'=@{color='Red';        emoji='🔴'}
        '5'=@{color='DarkYellow';emoji='🟠'}; '6'=@{color='Green';     emoji='🟢'}
        '7'=@{color='White';    emoji='⚪'}; '8'=@{color='Blue';       emoji='🔷'}
        '9'=@{color='DarkCyan'; emoji='🔹'}
      }
      $name = if ($Rest) { $Rest[0] } else { Read-Host "  Profile name (e.g. Contoso.MyProject-dev)" }
      if (-not $name) { Write-Host "  Cancelled" -ForegroundColor Yellow; return }
      # Check if exists
      foreach ($existing in $script:ProfilesList) {
        if ($existing.name -eq $name) { Write-Host "  Profile '$name' already exists" -ForegroundColor Red; return }
      }
      # Pick color
      Write-Host ""
      foreach ($k in ($colorMap.Keys | Sort-Object)) {
        $c = $colorMap[$k]
        Write-Host "    [$k] $($c.emoji) $($c.color)" -ForegroundColor $($c.color)
      }
      Write-Host ""
      $colorChoice = Read-Host "  Pick a color (1-9)"
      if (-not $colorMap.ContainsKey($colorChoice)) { Write-Host "  Invalid color" -ForegroundColor Red; return }
      $picked = $colorMap[$colorChoice]
      # Generate label from name: "Contoso.Backend-dev" → "Contoso Backend Dev"
      $label = "$($picked.emoji) " + ($name -replace '[.\-_]',' ' -replace '(\b\w)',{ $_.Groups[1].Value.ToUpper() })
      $ps5Label = ($name -replace '[.\-_]',' ' -replace '(\b\w)',{ $_.Groups[1].Value.ToUpper() })
      Write-Host "  Label: $label" -ForegroundColor $($picked.color)
      $confirm = Read-Host "  Create? (y/n)"
      if ($confirm -ne 'y') { Write-Host "  Cancelled" -ForegroundColor Yellow; return }
      # Create dirs
      $azDir = "$HOME\.azure-$name"
      $azdDir = "$HOME\.azd-$name"
      if (-not (Test-Path $azDir))  { New-Item -ItemType Directory -Path $azDir -Force | Out-Null }
      if (-not (Test-Path $azdDir)) { New-Item -ItemType Directory -Path $azdDir -Force | Out-Null }
      # Create .env
      $envFile = "$HOME\PSProfiles\$name.env"
      @"
AZURE_PROFILE_NAME=$name
# Profile: $name
# Azure CLI config isolation
AZURE_CONFIG_DIR=$azDir
AZD_CONFIG_DIR=$azdDir

# 1Password secret references (uncomment and set vault paths)
# MY_SECRET=op://VaultName/ItemName/field
"@ | Set-Content $envFile -Encoding UTF8
      # Add to runtime list
      $script:ProfilesList += @{name=$name; label=$label; color=$picked.color}
      # Persist to PS7 profile
      $ps7Profile = "$HOME\OneDrive - Company\Documents\PowerShell\profile.ps1"
      $ps5Profile = "$HOME\OneDrive - Company\Documents\WindowsPowerShell\profile.ps1"
      $ps7Entry = "  @{name=`"$name`"; label=`"$label`"; color=`"$($picked.color)`"}"
      $ps5Entry = "  @{name=`"$name`"; label=`"$ps5Label`"; color=`"$($picked.color)`"}"
      foreach ($pf in @($ps7Profile, $ps5Profile)) {
        $entry = if ($pf -match 'WindowsPowerShell') { $ps5Entry } else { $ps7Entry }
        $content = Get-Content $pf -Raw
        $content = $content -replace '(\r?\n)\)', "`$1$entry`$1)"
        Set-Content $pf $content -NoNewline -Encoding UTF8
      }
      Write-Host ""
      Write-Host "  ✅ Profile '$name' created!" -ForegroundColor Green
      Write-Host "     .env:  $envFile" -ForegroundColor DarkGray
      Write-Host "     az:    $azDir" -ForegroundColor DarkGray
      Write-Host "     azd:   $azdDir" -ForegroundColor DarkGray
      Write-Host ""
      Write-Host "  Next: mj switch $($script:ProfilesList.Count) → az login → azd auth login" -ForegroundColor Cyan
    }
    default {
      Write-Host ""
      Write-Host "  ⚡ MJ CLI" -ForegroundColor White
      Write-Host "  ═══════════════════════════════════════════════════════" -ForegroundColor DarkGray
      Write-Host ""
      Write-Host "  Profile Management" -ForegroundColor White
      Write-Host "    mj switch           " -ForegroundColor Cyan -NoNewline; Write-Host " Interactive profile picker" -ForegroundColor Gray
      Write-Host "    mj switch 2         " -ForegroundColor Cyan -NoNewline; Write-Host " Switch to profile #2 directly" -ForegroundColor Gray
      Write-Host "    mj profiles         " -ForegroundColor Cyan -NoNewline; Write-Host " List all profiles" -ForegroundColor Gray
      Write-Host "    mj whoami           " -ForegroundColor Cyan -NoNewline; Write-Host " Current profile + Azure account" -ForegroundColor Gray
      Write-Host "    mj reset            " -ForegroundColor Cyan -NoNewline; Write-Host " Reset to default (~/.azure)" -ForegroundColor Gray
      Write-Host "    mj edit             " -ForegroundColor Cyan -NoNewline; Write-Host " Edit current profile .env" -ForegroundColor Gray
      Write-Host "    mj add              " -ForegroundColor Cyan -NoNewline; Write-Host " Create a new profile" -ForegroundColor Gray
      Write-Host ""
      Write-Host "  Secrets" -ForegroundColor White
      Write-Host "    mj secret add K V   " -ForegroundColor Cyan -NoNewline; Write-Host " Add secret (plain text or op://)" -ForegroundColor Gray
      Write-Host "    mj secret list      " -ForegroundColor Cyan -NoNewline; Write-Host " Show all secrets in profile" -ForegroundColor Gray
      Write-Host "    mj secret remove K  " -ForegroundColor Cyan -NoNewline; Write-Host " Remove a secret" -ForegroundColor Gray
      Write-Host "    mj secret browse    " -ForegroundColor Cyan -NoNewline; Write-Host " Browse 1Password vaults" -ForegroundColor Gray
      Write-Host ""
      Write-Host "  Shortcuts" -ForegroundColor White
      Write-Host "    mj s  = switch      " -ForegroundColor DarkGray -NoNewline; Write-Host " mj p  = profiles" -ForegroundColor DarkGray
      Write-Host "    mj w  = whoami      " -ForegroundColor DarkGray -NoNewline; Write-Host " mj r  = reset" -ForegroundColor DarkGray
      Write-Host "    mj e  = edit        " -ForegroundColor DarkGray -NoNewline; Write-Host " mj a  = add" -ForegroundColor DarkGray
      Write-Host "    mj sec = secret     " -ForegroundColor DarkGray
      Write-Host ""
      Write-Host "  💡 Plain secrets load instantly. op:// secrets auto-resolve via 1Password." -ForegroundColor DarkGray
      Write-Host ""
      Write-Host "  ═══════════════════════════════════════════════════════" -ForegroundColor DarkGray
      Write-Host ""
    }
  }
}

Set-Alias -Name mj -Value Invoke-MJ

# --- Auto-restore last used profile ---
$_lastProfFile = "$HOME\PSProfiles\.last-profile"
if ((-not $env:AZURE_PROFILE_NAME) -and (Test-Path $_lastProfFile)) {
  $_lastName = (Get-Content $_lastProfFile -Raw).Trim()
  $_p = script:Resolve-Profile $_lastName
  if ($_p) { script:Set-ProfileEnv $_p }
}
Remove-Variable _lastProfFile, _lastName, _p -ErrorAction SilentlyContinue
# ===== End MJ CLI =====

oh-my-posh init pwsh --config "~/Projects/ohmyposh-themes/1_shell.omp.json" | Invoke-Expression

# Atuin - magical shell history (Ctrl+R and Up arrow)
if (Get-Command atuin -ErrorAction SilentlyContinue) {
  $env:ATUIN_POWERSHELL_PROMPT_OFFSET = -1  # multi-line prompt
  atuin init powershell | Out-String | Invoke-Expression
}
