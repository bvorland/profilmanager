# Installing profilmanager (`pm`)

Pick the method that fits your platform and tolerance for moving parts.
Methods are listed in **preferred order** — the first that works on your
machine is the easiest path.

> Status legend: ✅ available now · 🚧 placeholder / awaiting first
> release · 📦 channel exists, see notes

---

## 1. One-shot installer (recommended) 🚧

> Awaiting the first tagged release. The installer scripts and `dist/`
> manifests are present and will go live the moment a `vX.Y.Z` tag ships
> via `.github/workflows/release.yml`. Until then, use **method 5**
> (build from source with `go install` / `go build`).

The installer detects your OS and architecture, downloads the matching
GitHub release archive, verifies its SHA-256 against `checksums.txt`,
and drops `pm` into a per-user location.

**macOS / Linux** — `bash`, `zsh`, or `sh`:

```sh
curl -fsSL https://raw.githubusercontent.com/bvorland/profilmanager/main/install.sh | sh
```

Pin a specific version, or override the install prefix:

```sh
PREFIX=/usr/local ./install.sh --version v0.1.0
```

Default install dir: `$HOME/.local/bin` (no `sudo` required). Add it to
`PATH` if it isn't already there — the script prints the exact line to
append to your shell rc.

**Windows** — PowerShell 5.1+ or PowerShell 7+:

```powershell
irm https://raw.githubusercontent.com/bvorland/profilmanager/main/install.ps1 | iex
```

Pin a version, change the install directory, or skip the PATH update:

```powershell
.\install.ps1 -Version v0.1.0 -InstallDir 'C:\Tools\pm' -NoPath
```

Default install dir: `%LOCALAPPDATA%\Programs\profilmanager`. The script
adds it to the **user** PATH (not system PATH) so no Admin elevation is
required.

---

## 2. winget (Windows) 🚧

Once the package is accepted to
[microsoft/winget-pkgs](https://github.com/microsoft/winget-pkgs):

```powershell
winget install bvorland.profilmanager
```

Until then, use the installer in §1 or download the zip directly (§6).
The submission template lives in
[`dist/manifests/winget/`](manifests/winget/).

---

## 3. Scoop (Windows) 📦

A self-hosted bucket is on the roadmap. In the meantime, install from
the per-release manifest directly:

```powershell
scoop install https://raw.githubusercontent.com/bvorland/profilmanager/main/dist/manifests/scoop/pm.json
```

When the bucket exists:

```powershell
scoop bucket add bvorland https://github.com/bvorland/scoop-bucket
scoop install pm
```

---

## 4. Homebrew (macOS / Linux) 🚧

Planned tap: `bvorland/homebrew-tap`. Once published:

```sh
brew install bvorland/tap/pm
```

A reference formula lives in [`dist/Formula/pm.rb`](Formula/pm.rb) for
ad-hoc installs from a local clone.

---

## 5. `go install` (any platform with Go ≥ 1.25) ✅

Skips the release pipeline entirely — builds from the latest tagged
commit on the default branch:

```sh
go install github.com/bvorland/profilmanager/cmd/pm@latest
```

The binary lands in `$(go env GOBIN)` or `$(go env GOPATH)/bin`. Note
that `pm version` will report `0.0.0-dev` because the build does **not**
go through the GoReleaser ldflags-injection step. Use a release method
above if you want a clean version string.

---

## 6. Download a binary from GitHub Releases 🚧

> Awaiting the first tagged release. Once a `vX.Y.Z` tag ships, every
> tagged release will publish archives for all 6 supported targets at
> <https://github.com/bvorland/profilmanager/releases>:

| Platform        | Archive                                  |
| --------------- | ---------------------------------------- |
| linux/amd64     | `pm_<version>_linux_amd64.tar.gz`        |
| linux/arm64     | `pm_<version>_linux_arm64.tar.gz`        |
| darwin/amd64    | `pm_<version>_darwin_amd64.tar.gz`       |
| darwin/arm64    | `pm_<version>_darwin_arm64.tar.gz`       |
| windows/amd64   | `pm_<version>_windows_amd64.zip`         |
| windows/arm64   | `pm_<version>_windows_arm64.zip`         |

Always verify the download against `checksums.txt`:

```sh
sha256sum -c <(grep pm_<version>_linux_amd64.tar.gz checksums.txt)
```

Extract the archive and drop the `pm` (or `pm.exe`) binary somewhere on
your `PATH`.

---

## Post-install: shell integration

`pm` cannot mutate the **current** shell's environment — that's
impossible from a child process — so you must source a tiny stub at shell
startup. Add the line for your shell to your shell rc:

| Shell      | Line                                              |
| ---------- | ------------------------------------------------- |
| bash       | `eval "$(pm session init --shell bash)"`          |
| zsh        | `eval "$(pm session init --shell zsh)"`           |
| fish       | `pm session init --shell fish \| source`           |
| PowerShell | `Invoke-Expression (& pm session init --shell pwsh)` |

Then verify everything wired up correctly:

```sh
pm doctor
```
