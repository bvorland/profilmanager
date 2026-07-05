# Quickstart

Five steps. Real commands. Sample output. ~5 minutes from `git clone` to a
working `pm exec` invocation.

If `pm` isn't installed yet, see [Install](../README.md#install) in the README.

---

## 1. Confirm `pm` is on PATH

```sh
pm --version
```

```text
pm 0.1.0
```

If `pm` isn't found, your install step didn't put it where your shell looks.
On Windows that usually means `%USERPROFILE%\go\bin` isn't on `PATH`; on
macOS / Linux it's typically `$HOME/go/bin`.

---

## 2. Wire `pm shell-init` into your shell rc

`pm` needs a stable session ID so it can scope "active profile" to your
terminal. Without it, `pm doctor` will fall back to your shell's PPID and
warn you (PPIDs are recycled and unreliable). One-time install:

```sh
# bash / zsh — add to ~/.bashrc or ~/.zshrc:
eval "$(pm shell-init --shell bash)"

# fish — add to ~/.config/fish/config.fish:
pm shell-init --shell fish | source

# PowerShell — add to $PROFILE:
pm shell-init --shell pwsh | Invoke-Expression
```

Restart your shell (or source the rc) and verify:

```sh
pm doctor
```

```text
session-id-source        ok    PM_SESSION_ID (pm-session)
profiles-dir-exists      ok    C:\Users\you\AppData\Roaming\profilmanager\profiles
state-dir-writable       ok    C:\Users\you\AppData\Local\profilmanager
providers-registered     ok    az(ok) azd(ok) gh(ok) kubectl(ok) git(ok)
mcp-registered           ok    .copilot/mcp-config.json references `pm mcp serve`
```

If `session-id-source` says `ppid-fallback`, the shell-init line above didn't
load. Check your rc.

> **Want shims too?** Pass `--with-shims` to `pm shell-init` to wrap `az`,
> `azd`, `gh`, `kubectl`, and `git` so raw commands honor the active profile.
> Opt-in by design — see [the four-mode model](../README.md#the-four-mode-shell-switching-model).

---

## 3. Create your first profile

```sh
pm profile add my-first --label "My First Profile" --color cyan
```

```text
created  C:\Users\you\AppData\Roaming\profilmanager\profiles\my-first.toml
  set color:  pm profile set-color my-first <color>
  show:       pm profile show my-first
```

`pm profile add` only creates the skeleton — name, label, color. The provider
blocks (`[azure]`, `[gh]`, etc.) and `[[env]]` entries are filled in via the
TUI:

```sh
pm tui
```

This launches the Bubble Tea TUI. Highlight `my-first`, press `e` to edit,
fill in the fields you need (e.g., set `azure.subscription` to a real GUID
and `gh.user` to your GitHub handle). `Ctrl+S` to save, `Esc` to leave.

To rename a profile, change the **Name** field here (or run `pm profile rename <old> <new>`); the profile file plus its default `~/.azure-<name>` / `~/.azd-<name>` and internal gh/kube directories are renamed to match.

The full schema is documented in
[`profile-schema.md`](profile-schema.md).

---

## 4. Run a command under the profile

```sh
pm exec my-first -- az account show
```

The child `az` process sees `AZURE_CONFIG_DIR` pointed at a profile-specific
directory, the WAM broker disabled, and any `op://` refs in the profile's
`[[env]]` resolved into its env block. The plaintext lives in the child's
env only; `pm` zeros its own copy as soon as the child has started.

If `az` isn't logged in yet, you'll get the standard `Please run 'az login'`.
That's expected — `pm exec` doesn't trigger interactive auth. Run
`pm exec my-first -- az login` once and the credential lands under the
profile's `AZURE_CONFIG_DIR`, isolated from your other profiles.

---

## 5. Check `pm whoami` for drift

```sh
pm whoami
```

```text
── az ──
  Account:      you@example.com
  Tenant:       00000000-...
  Subscription: 11111111-...

── azd ──
  Account:      you@example.com
  Tenant:       00000000-...

── gh ──
  Account:      you
  Hosts:        github.com

── git ──
  user.name:    Your Name
  user.email:   you@example.com

── kubectl ──
  (not logged in)

── drift ──
  [warn] az-azd-account-mismatch — az and azd are signed in as different users
    fix: azd auth login --tenant 00000000-...
```

The drift block is the value-add. If `az` and `azd` disagree on subscription
or account, `pm whoami` tells you up front — before `terraform apply` does it
for you.

`pm whoami --json` emits the same data as a stable JSON document, suitable
for scripts and agents.

---

## What's next

- **Migrate from `mj`?** `pm import-mj` reads a Majid-style `profile.ps1`
  (see [`sample/profile.ps1`](../sample/profile.ps1) for the layout) and
  the matching `~/PSProfiles/*.env`, then writes one TOML per profile.
  Idempotent; `--dry-run` shows you what it would do first.
- **Hook up Copilot CLI?** Add the MCP server stanza shown in the
  [README](../README.md#copilot-cli-integration), then read
  [`agent-integration.md`](agent-integration.md).
- **Editing TOML by hand?** [`profile-schema.md`](profile-schema.md) is the
  authoritative reference.
- **Security questions?** [`threat-model.md`](threat-model.md) covers what
  pm defends against and what it doesn't.
