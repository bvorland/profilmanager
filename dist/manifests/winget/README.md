# Winget submission

This directory holds a **template** winget manifest for `profilmanager`.
It is not picked up automatically by GoReleaser — until winget submission
is automated, the flow is manual.

## After each release

1. The GitHub Release publishes `pm_<version>_windows_amd64.zip` and
   `pm_<version>_windows_arm64.zip` plus `checksums.txt`.

2. Install [wingetcreate](https://github.com/microsoft/winget-create) and
   run (PowerShell, with the placeholders filled in):

   ```powershell
   $version = '0.1.0'
   $base    = "https://github.com/bvorland/profilmanager/releases/download/v$version"
   wingetcreate update bvorland.profilmanager `
     --urls "$base/pm_${version}_windows_amd64.zip|x64" `
            "$base/pm_${version}_windows_arm64.zip|arm64" `
     --version $version `
     --submit
   ```

3. `wingetcreate` opens a PR against
   [microsoft/winget-pkgs](https://github.com/microsoft/winget-pkgs).
   Once merged, users can install with:

   ```powershell
   winget install bvorland.profilmanager
   ```

## Why not automated yet?

Submitting to `winget-pkgs` requires:

- A GitHub PAT with public-repo write access that can open PRs against
  the upstream registry.
- A first-time human-reviewed submission (the package identifier
  `bvorland.profilmanager` must be accepted before automated updates
  are trusted).

Once the first submission is merged, this can be promoted to a
post-release job in `.github/workflows/release.yml`.

The `profilmanager.yaml` file in this directory is a reference template:
keep it in sync with any schema changes (`ManifestVersion`) so manual
submissions don't drift.
