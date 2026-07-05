# profilmanager (`pm`)

A TUI + CLI + MCP server that manages developer profiles — Azure subscriptions,
GitHub accounts, `kubectl` contexts, git identity, and secrets — out of one
place, on one binary, with one profile model. Switch profiles deliberately;
never wonder which subscription your terminal is pointed at.

> **Status:** alpha (v0.x). Foundation, providers, secret resolvers, MCP server,
> and TUI are in. Distribution, the v1.1 backends (Key Vault, Bitwarden), and
> more providers are not. See [Status](#status) for the honest breakdown.

---

## Quickstart

```sh
pm profile new          # interactive wizard; auto-applies to your shell (with pm shell-init)
pm copilot prod         # launch Copilot CLI inside Contoso.Prod's sandbox
pm env apply            # picker if no arg
pm exec prod -- az ...  # fuzzy-prefix match (prod → Contoso.Prod)
pm whoami               # shows active profile + all providers
pm doctor               # warns if you're in a Copilot/Claude session without a profile loaded
```

---

## Why this exists

You probably have ≥ 3 Azure subscriptions, two GitHub accounts, a personal git
identity and a work one, three `kubectl` contexts, and a handful of `.env`
files holding tokens. Today that means:

- **`az` and `azd` drift.** `az` is logged into tenant A; `azd` is still logged
  into tenant B. You don't find out until `azd provision` fails or — worse —
  succeeds against the wrong subscription.
- **`terraform destroy` against the wrong subscription.** `AZURE_CONFIG_DIR`
  is whatever your last shell left it as. Catching it after the fact is a bad
  day.
- **Secrets sprawled across `.env` files.** Some in `~/PSProfiles/*.env`, some
  in `~/.zshrc`, some in 1Password, some hardcoded in `pwsh -NoExit -Command`
  one-liners.
- **Agents that don't know which profile you're in.** Copilot CLI fires off
  `az account list` and gets whatever your shell's last `AZURE_CONFIG_DIR`
  pointed at — which is almost never what you wanted.

`pm` is one TOML file per profile, one Go binary, one MCP server, and four
explicit shell-switching modes (see below). It is deliberately not magic.

---

## Install

<!-- INSTALL:BEGIN — Canonical install reference is dist/INSTALL.md; this section is the short pointer. -->

Full install reference (one-shot installers, winget, scoop, brew, raw GitHub
release archives, verification with checksums) lives in
[`dist/INSTALL.md`](dist/INSTALL.md). Until the first tagged release lands, the
fast path from source is:

```sh
# Requires Go 1.25.5 or newer.
go install github.com/bvorland/profilmanager/cmd/pm@latest
pm --version
```

Or clone and build:

```sh
git clone https://github.com/bvorland/profilmanager.git
cd profilmanager
go build -o pm ./cmd/pm
./pm --version
```

<!-- INSTALL:END -->

---

## Creating a profile

Default path:

```sh
pm profile new [name] [--from <template>] [--no-login] [--apply|--no-apply]
```

`pm profile new` is a flat-prompt wizard. With `<name>`, the name step is
pre-filled. With `--from <template>`, reusable fields are copied from an
existing profile; the wizard also offers sibling templates when it sees the same
`Group.*` prefix. Use `--no-login` for scripted/template runs that should not
launch the first-login prompt.

At the end of the wizard, `pm` asks: `Apply this profile to your current shell
now? [Y/n]`. Auto-apply requires `pm shell-init pwsh` to be loaded in
`$PROFILE`; see [Shell setup](#shell-setup) for the recommended setup. Without
shell-init, the wizard prints a loud banner with the one-time apply command. Use
`--apply` to skip the prompt and always apply, or `--no-apply` to skip the prompt
and never apply. The two flags are mutually exclusive.

TUI path:

```sh
pm tui
# press n
```

At the end, the wizard can prompt for the first Azure login inside the profile's
sandbox. That avoids the common trap: running `az login` in the host shell and
accidentally populating `~/.azure` instead of the profile's isolated
`AZURE_CONFIG_DIR`.

Advanced, non-interactive path:

```sh
pm profile add <name> [flags]
```

Use `profile add` for one-shot scripted creation. For humans, prefer the wizard.

### Pasting into wizard prompts

The wizard reads input in cooked terminal mode, which lets Windows conhost /
Windows Terminal handle paste shortcuts natively:

- **Ctrl+V** — paste (Windows Terminal default; works in pwsh, cmd, bash)
- **Shift+Insert** — paste (universal terminal binding)
- **Right-click** — paste in Windows Terminal if you've enabled it under
  *Settings → Interaction → Automatically copy selection to clipboard / paste
  on right-click*. PSReadLine handles right-click at your normal prompt; inside
  `pm.exe` cooked mode lets the terminal handle it directly, so the terminal
  setting governs.

If you're scripting the wizard (piping stdin), `pm` uses the same plain bufio
reader, so existing scripts keep working.

---

## Loading a profile into your current shell

There are three ways to scope tools to a profile:

| Method | Command | When to use |
|--------|---------|-------------|
| **Auto-apply via wizard** | `pm profile new` | Default for new profiles; applies immediately when `pm shell-init pwsh` is loaded. |
| **Auto-apply existing profile** | `pm env apply [name]` | Picker if `name` omitted; auto-applies to the current shell when `pm shell-init pwsh` is loaded. Without the wrapper, prints the script + a loud banner with the one-time pipe command. |
| **Manual apply** | `pm env apply <name> \| Invoke-Expression` | Explicit pipe — always works, even without `pm shell-init`. |
| **One-shot sandbox** | `pm exec <name> -- <command>` | Scripts, CI, agents, and one command at a time. |

For interactive one-shot work, `pm copilot <name>` launches Copilot CLI in the
profile sandbox, and `pm shell <name>` opens a subshell you can close to return
to host config.

---

## Using a profile

```sh
pm env apply [name]              # current shell; picker if no name
pm exec <name> -- <command>      # one-shot sandboxed command
pm shell <name>                  # isolated subshell with profile env active
pm copilot <name>                # shortcut for `pm exec <name> -- copilot`
```

All four commands accept exact names, case-insensitive names, and unique
prefixes. If no name is provided on an interactive terminal, `pm` opens the
profile picker.

Which should I use?

- `env apply` — long sessions where you want the current shell changed.
- `exec` — one-shot commands, scripts, CI, and agents.
- `shell` — a clean subshell you can close to return to host config.
- `copilot` — a Copilot CLI shortcut that avoids the trap where bare `copilot`
  starts against host config.

---

## Shell setup

```powershell
# PowerShell — add this single line to $PROFILE
pm shell-init pwsh | Out-String | Invoke-Expression
```

`pm shell-init pwsh` is the recommended PowerShell install line. It does three
things:

1. Forwards the Copilot CLI session id as `PM_SESSION_ID`.
2. Registers the `pm` wrapper that auto-applies after `pm profile new` **and** after `pm env apply` (picker or named — both modes pipe through `Invoke-Expression` automatically). `--help` and non-pwsh `--shell` values are passed through unchanged.
3. Loads `pm` shell completion.

### Prompt integration (oh-my-posh)

If you use oh-my-posh, `pm prompt install --omp` patches your theme JSON to add a
segment that shows the active profile right after your username. The segment turns
**red with `⚠️ no profile`** when nothing is applied — a loud reminder when you're
in an AI-agent session without a sandbox.

```powershell
pm prompt install --omp                       # auto-detects theme; backs up to .bak
pm prompt install --omp --dry-run             # preview the patched JSON, don't write
pm prompt install --omp --theme C:\path\to.json  # explicit theme path
pm prompt uninstall --omp                     # restore from .bak (or strip cleanly)
pm prompt segment --omp                       # emit just the segment JSON (manual paste)
```

Auto-detection order: `--theme` flag → `$POSH_THEME` → first PowerShell `$PROFILE`
file that contains `oh-my-posh ... --config <path>` (covers OneDrive-redirected
profiles).

Re-running `install` is **idempotent** — the segment is replaced by `properties.pm_managed`
marker, never duplicated. The `.bak` is created once on first install and never
overwritten, preserving your pristine pre-pm theme.

Currently only JSON oh-my-posh themes are auto-patched. yaml/toml theme users can
use `pm prompt segment --omp` and paste the JSON snippet manually.

> If you previously installed `pm session init` in your `$PROFILE`, replace it
> with `pm shell-init pwsh` — same baseline + auto-apply wrapper + completion.

```sh
# bash — add to ~/.bashrc
source <(pm completion bash)

# zsh — add to ~/.zshrc
source <(pm completion zsh)

# fish
pm completion fish | source
```

Completion covers subcommands and profile names on `pm exec`, `pm copilot`,
`pm shell`, `pm env apply`, `pm switch`, `pm profile show`, and
`pm profile rm`.

---

## Fuzzy matching and did-you-mean

Profile arguments resolve in this order:

1. Exact match.
2. Case-insensitive exact match.
3. Unique case-insensitive prefix.

Typos get a suggestion:

```text
profile "Cntso.Dev" not found. Did you mean: Contoso.Dev?
```

Ambiguous prefixes list the matches and refuse to guess.

---

## Dashboard

Run `pm` with no args for the dashboard. It shows the active profile, total
profile count, recent profiles with color swatches, and common next commands.

---

## Active profile

`pm whoami` starts with the active profile banner, then prints provider state so
host-vs-sandbox is obvious:

```text
── active profile: (none — host config) ──
  (no pm profile applied to this shell; tools see host config)
```

---

## Renaming a profile

Rename from the CLI:

```sh
pm profile rename <old> <new>
```

…or open the profile in the TUI (`pm tui` → `e`) and edit the **Name** field —
saving performs the rename. Either way the storage follows the new name:

- the profile file `<name>.toml` is renamed and its `name` field updated;
- default-pattern config dirs (`~/.azure-<name>`, `~/.azd-<name>`) are rewritten
  to the new name (custom paths are left untouched);
- the name-derived directories that hold cached logins/state are **moved** so
  they follow the rename: `~/.azure-<name>`, `~/.azd-<name>`, and the internal
  `gh` / `kube` state dirs;
- the session's active-profile marker and the operator "last profile" pointer
  are updated when they referenced the old name.

Pass `--no-move-dirs` to repoint the paths without moving the directories (the
providers recreate fresh, empty dirs on the next apply, so you would re-login).
Cross-volume or in-use directories can't be moved; `pm` reports those and leaves
them in place.

---

## Agent guardrails

`pm doctor` includes an `agent-context-has-profile` check. It warns when you're
inside an AI agent such as Copilot CLI or Claude Code without an active profile,
because tools will inherit host config.

The `pm` dashboard shows the same situation as an inline yellow `⚠️` banner.
This catches the common trap: starting Copilot from a bare shell, inheriting host
Azure/GitHub config, then seeing confusing "no active subscription" errors.

---

## The four-mode shell switching model

`pm` will not lie to you about what it can mutate. A child process cannot
mutate its parent shell's environment — period. So instead of pretending,
there are **four explicit modes**, each solving a different problem:

| Mode                                                       | What it mutates                                  | When to use                                                            |
|------------------------------------------------------------|--------------------------------------------------|------------------------------------------------------------------------|
| **`pm exec <profile> -- <cmd>`**                           | The child process's env, for one invocation      | Agents, CI, scripts, one-off commands. The workhorse.                  |
| **`pm shell <profile>`**                                   | A fresh interactive shell's env                  | Humans who want a clean session per profile (the `op run -- pwsh` model). |
| **`pm env apply <profile> --shell pwsh \| Invoke-Expression`** | Your *current* shell's env                       | Humans who want to mutate the shell they're already in (direnv-style). |
| **`pm shell-init --with-shims`**                           | Defines `az`/`azd`/`gh`/`kubectl`/`git` aliases  | One-time setup so raw `az account list` honors the active profile.     |

**"Active profile" is metadata, not env activation.** `pm switch <name>` writes
a session-scoped marker file. That marker tells `pm exec` and the MCP tools
"use this profile by default." It does **not** mutate your shell's env, and it
does **not** affect a raw `az account list` typed at the prompt — that still
reads `~/.azure` unless you've installed the shims (mode 4). This is the most
common confusion; the design is deliberate. See
[`docs/threat-model.md`](docs/threat-model.md) for why shims are opt-in.

---

## Copilot CLI integration

`pm` is designed to plug into [GitHub Copilot CLI](https://github.com/github/copilot-cli)
at three layers:

| Layer | What pm contributes | Setup command |
|-------|---------------------|---------------|
| **Statusline** | Profile chip + model + context-bar + token/cost counters under the prompt | `pm prompt install-statusline` |
| **MCP server** | 7 tools the agent can call directly (list/get/switch profile, whoami, secret-ref metadata, allowlisted exec) | Edit `~/.copilot/mcp-config.json` (see below) |
| **Project skill** | Teaches the agent *when* to use pm (`SKILL.md` under `.copilot/skills/profilmanager/`) | Auto-loaded by Copilot CLI when present in cwd |

The three are independent — you can install one, two, or all three. If you want
the full experience, install in the order listed above and restart your Copilot
CLI session once at the end.

### 1. Statusline (`pm prompt install-statusline`)

Copilot CLI ships a `statusLine` config slot (verified in v0.0.357+) that pipes
session JSON on stdin to a command of your choice and renders its stdout
below the prompt. `pm statusline` is built for that slot.

```powershell
pm prompt install-statusline           # patches ~/.copilot/settings.json + writes embedded omp theme
pm prompt install-statusline --dry-run # preview, write nothing
pm prompt install-statusline --force   # overwrite a foreign statusLine config
pm prompt uninstall-statusline         # restore from .bak (or delete just the statusLine key)
```

What the chips show, in left-to-right order:

```
 ⚪ contoso-demo  🤖 claude-opus-4.7-xhigh  ▰▰▱▱▱ 42%  ↓12345 ↑678  ⏱ 1m 15s  ◈ 3 pr  +120 -45
   profile        model                   context%   tokens         duration  premium  lines
```

- **Profile chip** — color matches the profile's TOML `color` field; uses the
  profile emoji prefix. When you're in a Copilot CLI session with no active
  profile (`PM_SESSION_ID` set, no profile chosen), it turns red
  `⚠️ no profile` — same warning surface as `pm doctor`.
- **Context bar** — `▰▰▱▱▱` fuel gauge, background green <60%, yellow <85%,
  red ≥85%, so you can see your context window saturation at a glance.
- Every chip past the first collapses if its data isn't present yet (Copilot
  CLI doesn't populate `cost.total_*` until you've made at least one API call).

Under the hood, `pm statusline`:

1. Reads up to 1 MB of JSON from stdin with a 2-second deadline.
2. Flattens it into `PM_SL_*` env vars (`PM_SL_MODEL`, `PM_SL_CONTEXT_PCT`,
   `PM_SL_TOKENS_IN`, …) plus `PM_ACTIVE_PROFILE_*` from `state.GetActiveProfile`.
3. Shells out to `oh-my-posh print primary --config <embedded-theme> --shell uni`
   with a 2-second timeout.
4. On *any* error (no omp installed, malformed JSON, panic, anything), prints a
   plain-text fallback `⚪ contoso-demo · 🤖 model` so Copilot CLI never sees a
   non-zero exit. Crash-resistance is a hard requirement here — Copilot calls
   the statusline on every refresh.

The install command writes:
- `~/.copilot/settings.json` — adds a `statusLine` block, preserving every
  other key. One-shot `.bak` on first patch; never overwrites a non-pm
  `statusLine` command without `--force`.
- `%LOCALAPPDATA%\profilmanager\statusline.omp.json` (macOS:
  `~/Library/Application Support/profilmanager/`; Linux: `$XDG_DATA_HOME/profilmanager/`)
  — the embedded theme is unpacked here so you can hand-tweak chips later
  without re-installing pm.

If chips don't appear after install, the usual fixes are:

- Restart your Copilot CLI session — `settings.json` is read at session
  start, not on refresh.
- `oh-my-posh cache clear` — omp v26+ caches parsed themes aggressively,
  so theme edits may not appear until the cache is cleared.
- `oh-my-posh --version` — install must be ≥ v18; the install command
  doesn't currently check.

### 2. MCP server

`pm` ships an embedded [Model Context Protocol](https://modelcontextprotocol.io/)
server reachable over stdio as `pm mcp serve`. Register it once in
`~/.copilot/mcp-config.json`:

```json
{
  "mcpServers": {
    "profilmanager": {
      "type": "stdio",
      "command": "pm",
      "args": ["mcp", "serve"],
      "tools": ["*"]
    }
  }
}
```

Restart Copilot CLI and verify with `/mcp` — `profilmanager` should appear
with 7 tools.

**The 7 tools, at a glance:**

| Tool | Mutates? | What it does |
|------|----------|--------------|
| `list_profiles` | No | Metadata-only list of every profile + per-resolver availability (`op` / `wincred` / `dotenv`) |
| `get_profile` | No | Full TOML body for one profile. Secret refs returned verbatim; **values never included** |
| `get_active_profile` | No | Read the session's active-profile marker + session-id source |
| `switch_profile` | Yes | Set/clear the session-scoped active-profile marker. **Does NOT mutate the calling shell's env** |
| `whoami` | No | Per-provider login state + cross-tool drift report (e.g., `az` vs `azd` user mismatch) |
| `resolve_secret_ref` | No | Backend + availability + existence metadata for an `op://` / `wincred://` / `dotenv://` ref. **Never returns the value** |
| `exec_with_profile` | Yes | Run an allowlisted command (`az` / `azd` / `gh` / `kubectl` / `git`) in a child process with the profile's env. Output redacted, every call audited |

**Security model — the iron rules:**

- 🔒 **Resolved secret values never cross MCP.** `resolve_secret_ref` returns
  metadata only. `exec_with_profile` puts secrets into a child process's env
  variables, then redacts every occurrence of the resolved value from stdout
  and stderr before the response leaves the process. There is no MCP tool that
  can return a raw secret value — by design.
- 🔒 **Command allowlist, no shell.** `exec_with_profile` only accepts commands
  on the allowlist (default: `az`, `azd`, `gh`, `kubectl`, `git`), matched on
  basename case-insensitively. Args are passed as an explicit argv slice via
  `exec.CommandContext` — never `cmd /c "<string>"` or `sh -c`.
- 🔒 **Hard timeout + output cap.** 60s default timeout (300s hard cap),
  1 MiB per stream output cap. Truncation is signalled in the response.
- 🔒 **Every call audited.** One JSON line per invocation under
  `<StateDir>/audit/mcp.log`. Args are pre-redacted; full stdout/stderr are
  NOT logged, only a redacted 256-byte preview.

**Example: agent calls `exec_with_profile`:**

```jsonc
// Request
{
  "name": "exec_with_profile",
  "arguments": {
    "command": "az",
    "args": ["account", "show"],
    "profile": "Contoso.Prod",
    "timeout_seconds": 30
  }
}

// Response
{
  "command": "az",
  "args": ["account", "show"],
  "profile": "Contoso.Prod",
  "exit_code": 0,
  "stdout": "{ \"id\": \"…\", \"tenantId\": \"…\", \"user\": { … } }",
  "stderr": "",
  "duration_ms": 423,
  "truncated": false
}
```

The complete tool reference — full JSON schemas, every field, every exit code,
session-id resolution order, the `--json` CLI fallback envelope — lives in
[`docs/agent-integration.md`](docs/agent-integration.md). Read that before
writing an MCP client; this section is the orientation, that file is the spec.

### 3. Project skill

If your repository (or any repo in cwd's ancestry) has
`.copilot/skills/profilmanager/SKILL.md`, Copilot CLI auto-loads it as a
domain-specific instruction prefix. The skill that ships with this repo
teaches the agent:

- **When** to reach for pm (any mention of a tenant/subscription/account by
  name, login errors, "wrong sub" symptoms, before destructive ops).
- **Which** tool to prefer (MCP over shell, `exec_with_profile` over
  `pm exec`, `resolve_secret_ref` before `exec_with_profile` to verify a
  ref is available).
- **What not to do** (never print resolved secret values, never invent profile
  names, never call `switch_profile` for a one-shot — use `exec_with_profile`
  with an explicit `profile` arg instead).

Copy [`.copilot/skills/profilmanager/SKILL.md`](.copilot/skills/profilmanager/SKILL.md)
into your own project's `.copilot/skills/` if you want agents working there
to know about pm. The skill is intentionally self-contained; it doesn't depend
on any other file in this repo.

### Session-id detection (how "active profile" stays sane across tabs)

`pm` scopes "active profile" to a session, resolving the session ID in this
order (first non-empty wins):

1. `PM_SESSION_ID` — canonical. Set by `pm session init` (which `pm shell-init` calls).
2. `COPILOT_AGENT_SESSION_ID` — Copilot CLI sets this; matches the Copilot session folder UUID.
3. `WT_SESSION` — Windows Terminal session GUID.
4. PPID fallback — prefixed `ppid-<pid>`. Fragile; emits a doctor warning.

The MCP server reports which source it used in `get_active_profile`'s
`session_source` field. Anything other than `pm-session` is suboptimal;
`ppid-fallback` means the agent should suggest running `pm session init`.

---

## Status

Honest accounting of what's in and what's not, as of v0.x:

**Solid:**

- Profile foundation (`internal/core`): schema v1, atomic-write + flock,
  per-OS storage paths, name validation.
- Provider integrations (`internal/providers`): `az`, `azd`, `gh`, `kubectl`,
  `git` with per-tool env conventions and cross-tool drift detection. The
  Azure WAM-broker mitigation is shipped (see
  [`docs/isolation-matrix.md`](docs/isolation-matrix.md) §1).
- Secret resolvers (`internal/secrets`): 1Password (`op://`), Windows
  Credential Manager (`wincred://`), dotenv (`dotenv://path#KEY`), literal
  values. Audit log with rotation; opaque `Secret` type with `Reveal()` /
  `Zero()`.
- MCP server (`internal/mcp`): 7 tools, `exec_with_profile` with allowlist /
  timeout / output cap / redactor / audit.
- TUI (`internal/tui`): dashboard, list, edit, profile wizard, doctor, confirm
  modal — Bubble Tea + Lipgloss; `NO_COLOR` honored.
- CLI verbs: `pm profile {new,list,show,add,rm,set-color,rename}`, `pm whoami`,
  `pm switch`, `pm exec`, `pm shell`, `pm copilot`, `pm env apply`,
  `pm shell-init`, `pm completion`, `pm session init`, `pm import-mj`,
  `pm doctor`, `pm mcp serve`, `pm statusline`,
  `pm prompt {install,uninstall,install-statusline,uninstall-statusline,segment}`.
- Copilot CLI integration (`internal/cli/statusline*.go`): `statusLine`
  patcher, embedded oh-my-posh theme, 7-chip profile-aware status bar with
  crash-resistant top-level recover.

**TBD:**

- Distribution beyond `go install` — `install.sh` / `install.ps1`, winget,
  scoop, brew. See `dist/` and the upcoming
  GitHub release workflow.
- More secret backends: Azure Key Vault, Bitwarden. The resolver interface is
  stable; adding a backend is additive.
- More providers: Docker contexts, Terraform workspaces, npm/pnpm registries.
  Same answer — the `Provider` interface is stable.
- Live-cloud isolation probes (`scripts/isolation/run-matrix.* --allow-live`).
- Plugin marketplace and out-of-process plugins — explicitly v2.

---

## Acknowledgments

The inspiration is **[Majid Hajian](https://github.com/MajidHajian)**'s `mj`
PowerShell CLI — the `Switch-ProfileSmart` function and the
`~/PSProfiles/<name>.env` layout were the daily driver this project grew out of.
Majid's script is preserved (with profile names genericized) in
[`sample/profile.ps1`](sample/profile.ps1), and `pm import-mj` migrates the
`$ProfilesList` schema into pm's TOML profiles, hoisting the Azure-specific
keys (`AZURE_CONFIG_DIR`, `AZD_CONFIG_DIR`, `AZURE_PROFILE_NAME`) into the
structured `[azure]` / `[azd]` blocks rather than leaving them as raw env.

Huge thanks, Majid — the workflow, the mental model, and the four-mode
shell-switching pattern this project ships are direct descendants of yours.

---

## License

[MIT](LICENSE) © 2026 Bjørn Atle Vorland
