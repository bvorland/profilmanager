---
name: "profilmanager"
description: "Use the pm (profilmanager) MCP tools or CLI whenever the user mentions a tenant/subscription/account by name (dev, work, personal, contoso, fabrikam, …), switches profiles or subs, gets az/azd/gh login errors, sees a wrong-subscription / wrong-account / wrong-tenant message, runs ops against an Azure subscription, deploys to staging/prod, rotates a token, references op:// or 1Password, asks about environment variables / AZURE_CONFIG_DIR / AZD_CONFIG_DIR / kubectl context / git identity, suspects drift between az and azd, talks about multi-tenant or multi-account isolation, asks 'which sub am I in' / 'which profile' / 'what account', or is about to do anything destructive (az group delete, gh repo delete, kubectl delete). Never print resolved secret values. Prefer MCP tools when registered; fall back to `pm <verb> --json`."
domain: "profilmanager, environment-isolation, multi-tenant-azure, secret-handling, copilot-cli-integration"
confidence: "high"
source: "manual"
tools:
  - name: "list_profiles"
    description: "Metadata-only list of every profile pm knows about, plus per-resolver availability (op / wincred / dotenv)."
    when: "Discover what profiles exist before asking the user to pick one; check whether the secret backend the workflow needs is signed in."
  - name: "get_profile"
    description: "Full TOML body for one profile. Secret refs are returned verbatim (metadata); values are NEVER included."
    when: "Need to inspect what a profile actually configures (AZURE_CONFIG_DIR, subscription, env vars, refs) before acting."
  - name: "get_active_profile"
    description: "Which profile is marked active for this MCP session, plus session-id source."
    when: "Answer 'which profile am I in?' without touching providers. Verify there IS an active profile before exec."
  - name: "switch_profile"
    description: "Set/clear the session-scoped active profile marker. Metadata only — does NOT mutate the calling shell."
    when: "User says 'switch to <name>' AND every following command will go through pm exec / exec_with_profile / the optional shims. Otherwise prefer per-call exec_with_profile."
  - name: "whoami"
    description: "Drift report across az / azd / gh / kubectl / git plus resolver availability. Never triggers interactive login."
    when: "Diagnose 'wrong sub' / 'wrong account' problems, surface drift between az and azd, confirm an account/tenant before destructive ops."
  - name: "resolve_secret_ref"
    description: "Look up a secret reference's METADATA only — resolver, availability, existence, last-resolved timestamp. NEVER returns the value."
    when: "Verify an op:// / wincred:// / dotenv:// ref is wired and reachable before invoking exec_with_profile. Diagnose 'this op ref keeps failing'."
  - name: "exec_with_profile"
    description: "Run an allowlisted command (default: az / azd / gh / kubectl / git) in a child process with the profile's env applied. Secrets materialise in the child env only, stdout/stderr is redacted, every call is audited."
    when: "ANY time the agent needs to execute a provider command on behalf of the user with a specific profile's identity. This is the workhorse."
---

## Context

`pm` (profilmanager) is the user's profile/identity manager. It owns the mapping from a friendly profile name ("Contoso.Dev", "Fabrikam.Work", "Fabrikam.Personal", …) to the bundle of state every tenant-aware tool needs: `AZURE_CONFIG_DIR`, `AZD_CONFIG_DIR`, `az` subscription/tenant, `gh` account, `kubectl` context, `git` identity, plus arbitrary env vars (literal or 1Password / wincred / dotenv refs).

The user spends most of their day in Copilot CLI and Squad agents. The whole point of `pm` is that when an agent gets dispatched — "deploy to dev staging", "show me the backend sub", "rotate the gh token" — it should figure out which profile to operate in WITHOUT the user re-explaining every turn.

Two integration paths exist; the skill teaches both:

1. **MCP tools** — `pm mcp serve` is registered in `.copilot/mcp-config.json` as the `profilmanager` MCP server. Seven tools (above). **Prefer MCP when available** — it's structured, audited, and the resolver-availability metadata lets you plan ahead.
2. **CLI** — `pm <verb> --json` is the lowest common denominator. Use as fallback when MCP isn't registered or when the agent is shelling out anyway.

## When to use

Activate on any of these signals from the user:

- **Profile/tenant reference by name** — "dev", "work", "personal", "Contoso.*", "Fabrikam.*", "the dev sub", "my work account", or any string that looks like a profile name. Treat it as a profile until proven otherwise.
- **Sub/account/identity errors** — `az login` failure, `azd login` failure, `gh auth status` mismatch, "wrong subscription", "wrong account", "wrong tenant", "not logged in", `AADSTS*`, `ChainedTokenCredential` failure on Windows (WAM brokers).
- **Destructive ops** — `az group delete`, `az resource delete`, `azd down`, `gh repo delete`, `kubectl delete`, `terraform destroy`. ALWAYS confirm which profile/sub is active before exec.
- **Drift suspicion** — `az` shows subscription X but `azd` shows subscription Y; `gh auth status` lists a different account than expected; deploys land in the wrong place.
- **Secret resolution** — anything referencing `op://...`, `wincred://...`, `dotenv://...`, or "the secret in my profile".
- **New environment setup** — "set up a profile for client X", "I'm onboarding the backend tenant", "import my old mj profiles".
- **Session-scoped questions** — "which profile am I in?", "what's my current sub?", "am I in the right account?".

## When NOT to use

- The user has no `pm` installed AND is not asking about pm. Check `pm doctor` (or `pm --version`) once — if it exits non-zero or the binary is missing, mention installation once (see Quick install pointer) and move on. Don't loop on retries.
- Single-profile users with no tenant-switching need — `pm` adds no value over running `az`/`azd`/`gh` directly. Don't force the abstraction on them.
- Pure-app code work that doesn't touch a provider (writing a function, editing a README, fixing a unit test). `pm` is about identity, not editing.
- The user explicitly says "don't use pm" or "I'll handle the env myself".

## Patterns

### Tool-call order (the iron rule)

1. **MCP first.** If the `profilmanager` MCP server is registered (check by attempting `list_profiles` once at task start), use MCP tools exclusively. They're structured, the resolver-availability metadata avoids round-trips, and every call is audited centrally.
2. **CLI fallback.** If the MCP server is not registered (tool call fails with "no such tool" or similar), drop to `pm <verb> --json` and parse stdout. Same semantics, slightly more parsing.
3. **Don't mix.** Within a single workflow, pick one path and stay there. Mixing MCP `switch_profile` with CLI `pm exec` works (they share the same on-disk session state) but obscures the audit trail.

### The "switch is metadata" rule

`switch_profile` (MCP) and `pm switch <name>` (CLI) write a **session-scoped marker file**. They do NOT mutate the calling shell's environment, the agent's process env, or the user's terminal. You cannot "switch profile and then run `az ...` raw" — the raw `az` will read `~/.azure` exactly as before.

Two correct patterns:

- **Per-call** (preferred for agents): every command goes through `exec_with_profile` / `pm exec <name> -- <cmd>`. No switch needed, no state to drift.
- **Switch + exec** (preferred when a workflow needs ≥3 calls in the same profile): call `switch_profile` once, then call `exec_with_profile` with `profile=""` — the active marker is consulted.

### Secret values: the iron rule

**Resolved secret values NEVER leave pm.** `resolve_secret_ref` returns existence + resolver + last-resolved timestamp; `exec_with_profile` materialises values into the child process env only and redacts them from stdout/stderr before returning. The agent must do its part:

- **Never** ask `pm` to print resolved values, even "as debug output", even "just this once", even in a test.
- **Never** `echo $FOO` inside an `exec_with_profile` command if `$FOO` came from a ref — the redactor catches the common cases but the iron rule is don't ask in the first place.
- If the user asks "what's the value of $FOO?", **refuse** and explain: every `resolve_secret_ref` and `exec_with_profile` call is appended to the MCP audit log, and a request for a resolved value would leave a permanent record of the violation attempt. Direct them to `op item get ...` or the relevant backend if they genuinely need to read it.

### The MCP tools (full surface)

#### `list_profiles`
Metadata-only. Returns every profile pm knows about plus the per-resolver availability map. **Call this first** whenever you don't already know what profiles exist. The `resolvers` field lets you tell the user "your `op` CLI isn't signed in" before they ask why a deploy hung.

Response shape:
```json
{
  "profiles": [
    {"name": "Contoso.Dev", "label": "🔵 Contoso Dev", "color": "cyan",
     "path": "…/Contoso.Dev.toml",
     "has_azure": true, "has_azd": true, "has_gh": true, "has_kube": false, "has_git": true,
     "env_count": 4}
  ],
  "load_errors": [],
  "resolvers": {"op": {"available": false, "reason": "…"}, "wincred": {"available": true}, "dotenv": {"available": true}},
  "profiles_dir": "C:/Users/<user>/AppData/Roaming/profilmanager/profiles",
  "profile_count": 1
}
```

#### `get_profile`
Full TOML body for one profile. Secret refs are returned verbatim — they pinpoint WHERE secrets live, not what they are. Resolved values are NEVER included.

Args: `name` (required).

#### `get_active_profile`
What's currently active for this MCP session. Empty string = no active profile.

Response: `{"active": "Contoso.Dev", "session_id": "…", "session_source": "copilot-session"}`.

If `session_source` is `ppid-fallback`, the user's shell isn't seeding `PM_SESSION_ID` — see the **Session-ID note** below.

#### `switch_profile`
Sets/clears the session-scoped active-profile marker. Pass empty string to clear. Returns a `note` field that reminds you it doesn't mutate the shell — DO surface that note to the user when they say "switch me to X".

Args: `name` (required, empty to clear).

#### `whoami`
Aggregates every provider's logged-in state (no interactive prompts) plus cross-tool drift. **The most useful diagnostic tool.** Always include in the answer to "which sub am I in?" or "is something wrong?".

Response includes `providers[]`, `drift[]` (with `severity` and `fix`), and `resolvers`. A drift entry like `{"code": "az-azd-subscription-mismatch", "severity": "warn", "fix": "azd env refresh"}` tells you exactly what to suggest.

#### `resolve_secret_ref`
**METADATA ONLY.** Returns `{ref, resolver, available, exists, last_resolved_at, note}`. Use this to verify a ref is wired before calling `exec_with_profile` — saves you debugging an opaque "command exited 1" later.

Args: `ref` (required, e.g. `"op://Personal/GitHub Token/credential"`).

If `available: false`, the resolver backend isn't reachable (e.g. `op` not signed in). Tell the user how to fix it (`op signin`), don't try to resolve anyway.

#### `exec_with_profile`
The only MCP tool that triggers real provider state. Heavily guarded:

- **Allowlist** — default `az`, `azd`, `gh`, `kubectl`, `git`. Operator-configurable. A command not on the allowlist returns a tool error; do NOT ask the user to "just add it" without explaining the trust boundary.
- **No shell** — explicit argv only. `command: "az"`, `args: ["group", "list"]`. Never embed pipes, redirects, or `&&`. If the user asks for a pipeline, run the LHS through `exec_with_profile`, parse the output, then run the RHS yourself.
- **Timeout** — default 60s, hard cap 300s. Pass `timeout_seconds` for long-running deploys.
- **Redaction + audit** — automatic.

Args: `command` (required, basename), `args` (string[]), `profile` (optional — empty falls back to active), `timeout_seconds` (optional), `stdin` (optional).

### The CLI surface (fallback)

Every verb that produces structured output supports `--json`. Use these only when MCP is unavailable.

| Verb | Purpose | Example output |
|---|---|---|
| `pm profile list --json` | List profiles (metadata only). | `{"profiles":[{"name":"Contoso.Dev","label":"🔵 Contoso Dev",…}]}` |
| `pm profile show <name> --json` | Full TOML body as JSON. Add `--redacted` to mask sub/tenant IDs and replace refs with `<ref>` — safe for bug reports. | `{"schema":"1","name":"…","azure":{…},"env":[…]}` |
| `pm profile add <name> [--label …] [--color …]` | Create a new profile (basics only). Editing env/refs is the TUI's job. | `created C:\…\foo.toml` |
| `pm profile rm <name> [--force]` | Delete. Requires `--force` from non-TTY callers (i.e. agents). | `removed C:\…\foo.toml` |
| `pm whoami --json` | Same as MCP `whoami`. | `{"providers":[…],"drift":[…]}` |
| `pm whoami --profile-name` | Prints ONLY the active profile name, one line, empty if none. Designed for shell shims and agent scripts. | `Contoso.Dev` |
| `pm switch <name> [--quiet]` | Same as MCP `switch_profile`. Prints the activation hint unless `--quiet`. | `active: Contoso.Dev` + hint |
| `pm env apply <name> --shell {bash\|zsh\|pwsh\|fish\|cmd}` | Emit shell-evaluable env. Refuses if profile contains secret refs (use `pm exec`/`pm shell` instead). | `export AZURE_CONFIG_DIR=…` |
| `pm exec [<name>] -- <cmd> [args…]` | Workhorse. Run a child with profile env. `--` is required to separate profile from command. Falls back to active profile if name omitted. | passes through child stdout/exit |
| `pm shell [<name>]` | Spawn an interactive shell with profile env baked in. For humans, not agents. | drops user into pwsh/bash |
| `pm doctor --json` | Health check. Non-zero exit on hard fail. | `{"checks":[{"name":"session-id-source","status":"ok",…}]}` |
| `pm session init --shell <s>` | Emit shell code to bind `PM_SESSION_ID` for the current shell. One-time setup per shell session. | `export PM_SESSION_ID="…"` |
| `pm shell-init --shell <s> [--with-shims]` | Emit rc-loadable code: always sets `PM_SESSION_ID`; `--with-shims` also installs `az`/`azd`/`gh`/`kubectl`/`git` wrappers that route through the active profile. **Opt-in** — don't suggest `--with-shims` without explaining the silent reroute. | shell function definitions |
| `pm import-mj --from-powershell <profile.ps1>` | Migrate from Majid Hajian's `mj` CLI (the user's mental model). Lossy — prints a per-profile summary. | `imported: Contoso.Dev …` |
| `pm mcp serve` | Start the MCP server on stdio. Used by `.copilot/mcp-config.json`, not by agents directly. | (long-running stdio process) |

## Examples

Five concrete recipes. The CLI form is shown as a fallback; prefer MCP when registered.

### Recipe 1 — "Which profile am I in?"

**MCP:**
```
call: whoami
→ Read active from get_active_profile in parallel if you also want the session marker.
→ Report: active profile + az/azd/gh accounts + any drift entries.
```

**CLI:**
```
pm whoami --json
pm whoami --profile-name    # if you only need the marker
```

Always report drift entries if present — they're the early-warning system for "wrong sub" deploys.

### Recipe 2 — "Run X in profile Y" (the workhorse)

**Wrong (broken model):**
```
switch_profile Y           # only sets metadata
az group list              # still reads ~/.azure — uses whatever was there before
```

**Right (MCP):**
```
exec_with_profile {profile: "Contoso.Dev", command: "az", args: ["group", "list"]}
```

**Right (CLI):**
```
pm exec Contoso.Dev -- az group list
```

If the user said "switch to dev and run …", interpret that as "run … in dev" — do NOT actually call `switch_profile`. The switch is only useful if you're going to make ≥3 consecutive calls in the same profile.

### Recipe 3 — "Set me up for profile Y in this shell"

This is the user wanting to mutate their interactive shell. **You cannot do this for them** — a child process cannot mutate its parent's env. Tell them what to run.

**Bash/Zsh:** `eval "$(pm env apply Contoso.Dev --shell bash)"`
**PowerShell:** `pm env apply Contoso.Dev --shell pwsh | Invoke-Expression`
**Fish:** `pm env apply Contoso.Dev --shell fish | source`
**Cmd:** `pm env apply Contoso.Dev --shell cmd > %TEMP%\pm-env.bat && call %TEMP%\pm-env.bat`

If the profile has secret refs, `env apply` will refuse — tell the user to `pm shell Y` (fresh shell with secrets in env) instead, and explain why (writing secrets to shell history is a per-host privacy hazard).

### Recipe 4 — "Where does my az token go for profile Y?"

```
MCP:  get_profile {name: "Contoso.Dev"}
CLI:  pm profile show Contoso.Dev --json
```

Look at `profile.azure.config_dir` in the response — that's where `az` will write its `accessTokens.json` when invoked via `pm exec`. Typical shape: `C:\Users\<user>\.azure-Contoso.Dev` on Windows, `~/.azure-Contoso.Dev` elsewhere.

**Windows WAM caveat:** if the user is on Windows and `az login` is silently re-using a different account than expected, mention that the Web Account Manager (WAM) broker on Windows can short-circuit `AZURE_CONFIG_DIR` isolation in some `az` versions. The mitigation is usually `az config set core.enable_broker_on_windows=false` per profile (or set `AZURE_IDENTITY_DISABLE_DEVELOPER_CLI_AUTHENTICATION` for SDK callers). Confirm by running `pm exec Y -- az account show` and comparing against `pm whoami` drift.

### Recipe 5 — "This op:// ref keeps failing"

**Always check resolver availability first:**

```
MCP:  list_profiles    → inspect resolvers.op.available
CLI:  pm whoami --json → inspect resolvers (same map)
```

If `op.available: false` and `reason` mentions "not signed in":
> "Your 1Password CLI isn't signed in for this shell. Run `op signin` (or `eval $(op signin)` for some shells), then retry."

If available, verify the specific ref exists:

```
MCP:  resolve_secret_ref {ref: "op://Personal/GitHub Token/credential"}
      → check exists=true, available=true
CLI:  (no CLI equivalent — use the MCP path or have the user run `op item get …` themselves)
```

If `exists: false`, the vault/item/field path is wrong — tell the user to verify in 1Password directly. **Never** ask `pm` to print the value, even to "test if it works".

## Session-ID note

`pm` scopes the active-profile marker to a session ID. Resolution order (first non-empty wins):

1. `PM_SESSION_ID` — canonical; set by `pm session init` / `pm shell-init`.
2. `COPILOT_AGENT_SESSION_ID` — Copilot CLI sets this; pm verified it matches the Copilot session folder UUID.
3. `WT_SESSION` — Windows Terminal GUID.
4. PPID fallback — fragile under `sudo`, tmux, process recycling.

**If `pm doctor --json` reports `session-id-source` with `status: "warn"` and message mentions `ppid-fallback`**, the user's shell isn't seeding a stable ID. This is THE thing that makes profile state correlate to the agent's session. Tell the user, once, to add to their shell rc:

**Bash/Zsh** (`~/.bashrc` or `~/.zshrc`): `eval "$(pm shell-init --shell bash)"`
**Fish** (`~/.config/fish/config.fish`): `pm shell-init --shell fish | source`
**PowerShell** (`$PROFILE`): `pm shell-init --shell pwsh | Invoke-Expression`

Inside Copilot CLI itself, `COPILOT_AGENT_SESSION_ID` should already be present — verify with `get_active_profile` and look at `session_source`. If it shows `copilot-session`, you're good.

## Anti-Patterns

- ❌ **Calling `pm switch` and then running raw `az group list`.** Switch is metadata only; the raw `az` reads `~/.azure` unchanged. Use `pm exec` / `exec_with_profile`.
- ❌ **Asking `pm` to print resolved secret values.** Not even for debug. Audit log captures every access; refuse and explain.
- ❌ **`exec_with_profile` with a command not on the allowlist.** Don't paper around it with `git` wrapping the actual binary, don't ask the user to whitelist `bash` "just this once". The allowlist is the trust boundary.
- ❌ **Echoing or redirecting env vars in an exec command** — `command: "sh"`, `args: ["-c", "echo $FOO"]`. There's no shell on the exec path anyway, but even if there were, this would defeat redaction.
- ❌ **Suggesting `pm shell-init --with-shims` without warning the user.** Silently rerouting raw `az`/`gh`/`kubectl` through `pm exec` is exactly the kind of surprise the four-mode model exists to prevent. Operator must consent.
- ❌ **Using `pm env apply` when the profile has secret refs.** It will refuse for good reason. Use `pm exec`/`pm shell` or have the user run `op run --env-file=... -- <cmd>` themselves.
- ❌ **Treating `pm doctor` warnings as decorative.** A `session-id-source: warn` means the active-profile marker can drift between turns of the same conversation. Fix at root.
- ❌ **Doing a destructive op without confirming the active profile first.** Before `az group delete` / `azd down` / `gh repo delete` / `kubectl delete`, call `whoami` and read the subscription/account back to the user.

## Quick install pointer

If `pm` isn't installed on the user's machine (e.g. `pm doctor` exits with "command not found"), tell them once: "See the README at the repo root, or <https://github.com/bvorland/profilmanager>." Don't try to install for them.
