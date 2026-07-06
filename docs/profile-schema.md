# Profile schema reference (v1)

Authoritative reference for the profile TOML format. The Go source of truth is
[`internal/core/profile.go`](../internal/core/profile.go); this doc reflects
schema version `"1"` and tracks it in lockstep.

> **Location of profile files:**
> - Windows: `%APPDATA%\profilmanager\profiles\<name>.toml`
> - macOS: `~/Library/Application Support/profilmanager/profiles/<name>.toml`
> - Linux: `${XDG_CONFIG_HOME:-~/.config}/profilmanager/profiles/<name>.toml`
>
> One file per profile. File basename (minus `.toml`) must equal the
> top-level `name` field.

---

## Top-level fields

| Field    | Type   | Required | Default | Notes                                                              |
|----------|--------|----------|---------|--------------------------------------------------------------------|
| `schema` | string | yes      | —       | Must be `"1"`. Bumped on breaking schema changes; old files migrate. |
| `name`   | string | yes      | —       | Profile identifier. Validated against `^[A-Za-z0-9._-]+$`; `.` and `..` rejected. Must match filename. |
| `label`  | string | no       | name    | Free-form display label for the TUI / `pm profile list`. UTF-8 / emoji fine. |
| `color`  | string | no       | (dim)   | PowerShell `ConsoleColor` name: `Cyan`, `Magenta`, `Yellow`, `Red`, `DarkYellow`, `Green`, `Blue`, `White`, `DarkCyan`, etc. Unknown names render dim. |

**Profile name rules** (enforced on every `Load`, `Save`, `pm switch`,
`pm exec`, `state.SetActiveProfile`):

- ASCII letters, digits, and `.`, `-`, `_` only.
- `.` and `..` are explicitly rejected (path-traversal guard).
- Empty is rejected.
- No unicode, no spaces, no slashes. Use `label` for display text.

Rationale: the name is simultaneously a filename, a TOML identifier, a CLI
argument, and a shell-shim function name. Boring ASCII is the only character
set that works in all four roles.

> **Renaming a profile** keeps storage in sync: `pm profile rename <old> <new>`
> (or editing the **Name** field in the TUI) renames the `<name>.toml` file and the
> `name` field, rewrites the default-pattern `~/.azure-<name>` / `~/.azd-<name>` config
> dirs, and moves the per-profile `gh` / `kube` state dirs to match. Custom
> `config_dir` paths are left untouched. See the README's *Renaming a profile* section.

---

## `[azure]` — Azure CLI / `az`

| Field          | Type   | Required | Default            | Notes                                                                                 |
|----------------|--------|----------|--------------------|---------------------------------------------------------------------------------------|
| `subscription` | string | no       | —                  | Subscription ID (GUID) or name. Honored by `az` once `AZURE_CONFIG_DIR` is set.       |
| `tenant`       | string | no       | —                  | Tenant ID (GUID).                                                                     |
| `config_dir`   | string | no       | auto-derived       | Path used as `AZURE_CONFIG_DIR` for child processes. If unset, no per-profile dir.    |

**Side effect when present:** `pm exec` sets `AZURE_CONFIG_DIR=<config_dir>`,
sets `AZURE_CORE_OUTPUT=json`, and writes a baseline `config` file under
`<config_dir>` enforcing `[core] enable_broker_on_windows = false` (idempotent;
preserves any other keys the operator has set). See
[`docs/isolation-matrix.md`](isolation-matrix.md) §1 for why disabling WAM
matters.

---

## `[azd]` — Azure Developer CLI

| Field          | Type   | Required | Default | Notes                                                                              |
|----------------|--------|----------|---------|------------------------------------------------------------------------------------|
| `config_dir`   | string | no       | —       | Path used as `AZD_CONFIG_DIR` for child processes.                                 |
| `subscription` | string | no       | —       | Default subscription. Currently informational; `azd` inherits sub from `az` state. |

**Side effect:** `pm exec` sets `AZD_CONFIG_DIR=<config_dir>` on the child env.
When both `[azure]` and `[azd]` are present, both env vars ride along — so
`azd auth login`'s internal shell-out to `az` writes to the per-profile dir,
not `~/.azure`.

---

## `[gh]` — GitHub CLI

| Field   | Type      | Required | Default              | Notes                                                                  |
|---------|-----------|----------|----------------------|------------------------------------------------------------------------|
| `user`  | string    | no       | —                    | GitHub account handle. Used for drift detection vs `git.user_email`.   |
| `hosts` | string[]  | no       | `["github.com"]`     | One or more `gh` host names (e.g., `["github.com", "github.example.com"]`). |

**Side effect:** `pm exec` always sets `GH_CONFIG_DIR` under
`<StateDir>/gh/<profile.name>`, so each profile has its own `hosts.yml` and
`config.yml`. The path is auto-derived — operators don't need to spell it out.
Symlink the directory if you want to share `gh` config across profiles.

---

## `[kube]` — kubectl

| Field       | Type   | Required | Default | Notes                                                                  |
|-------------|--------|----------|---------|------------------------------------------------------------------------|
| `context`   | string | no       | —       | kubectl context name (set as `KUBE_CONTEXT` env var; informational).   |
| `namespace` | string | no       | —       | Default namespace (set as `KUBE_NAMESPACE`; informational).            |

**Side effect:** `pm exec` always sets `KUBECONFIG` to
`<StateDir>/kube/<profile.name>/config` (per-profile writable file, so two
`pm exec` runs don't race on a shared `current-context:` rewrite).
`KUBE_CONTEXT` and `KUBE_NAMESPACE` are informational env vars consumed by
`helm`, `k9s`, `kube-ps1`, etc. — kubectl itself reads context from the file.

---

## `[git]` — Git identity

| Field         | Type   | Required | Default | Notes                                                                          |
|---------------|--------|----------|---------|--------------------------------------------------------------------------------|
| `user_name`   | string | no       | —       | Sets `GIT_AUTHOR_NAME` and `GIT_COMMITTER_NAME`.                               |
| `user_email`  | string | no       | —       | Sets `GIT_AUTHOR_EMAIL` and `GIT_COMMITTER_EMAIL`.                             |
| `signing_key` | string | no       | —       | Path to SSH private key. Sets `GIT_SSH_COMMAND="ssh -i <key> -o IdentitiesOnly=yes"`. |

`IdentitiesOnly=yes` is critical: without it, `ssh-agent` will offer every key
the operator has loaded, and the wrong identity may sign the commit.

---

## `[[env]]` — Free-form environment variables

Array-of-tables. Each entry contributes one env var to the child process.
Exactly one of `value` or `ref` MUST be set per entry — never both, never
neither.

| Field   | Type   | Required          | Notes                                                                                                |
|---------|--------|-------------------|------------------------------------------------------------------------------------------------------|
| `key`   | string | yes               | The env var name.                                                                                    |
| `value` | string | exclusive w/ ref  | Literal value. Goes into the child env block verbatim. Not redacted in output.                       |
| `ref`   | string | exclusive w/ value | Secret reference. Resolved at exec time, redacted from output, never returned via MCP or `--json`.   |

**Supported ref schemes** (more in v1.1):

| Scheme prefix         | Resolver | Example                                                | Notes                                                       |
|-----------------------|----------|--------------------------------------------------------|-------------------------------------------------------------|
| `op://`               | `op`     | `op://Personal/GitHub Token/credential`                | Requires the 1Password CLI (`op`) signed in.                |
| `wincred://`          | `wincred`| `wincred://my-secret-name`                             | Windows only. Stub elsewhere (returns `ErrUnavailable`).    |
| `dotenv://path#KEY`   | `dotenv` | `dotenv://~/.env#MY_TOKEN`                             | URL-encode exotic paths. No `$VAR` expansion inside the file. |
| (no scheme)           | `dotenv` | `value = "literal-string"` — handled by `value`, not `ref` | Bare literals go in `value`, not `ref`.                 |

Refs are resolver-agnostic at write time — the registered resolver claims its
scheme prefix when `pm exec` runs. Adding a new resolver is additive; existing
profiles don't change.

---

## Full annotated example

```toml
# C:\Users\you\AppData\Roaming\profilmanager\profiles\Contoso.Dev.toml
schema = "1"
name   = "Contoso.Dev"
label  = "🔵 Contoso Dev"
color  = "Cyan"

[azure]
subscription = "11111111-2222-3333-4444-555555555555"
tenant       = "00000000-0000-0000-0000-000000000002"
config_dir   = "C:\\Users\\you\\.azure-Contoso.Dev"

[azd]
config_dir = "C:\\Users\\you\\.azd-Contoso.Dev"

[gh]
user  = "alex-example"
hosts = ["github.com"]

[kube]
context   = "contoso-aks-dev"
namespace = "contoso-app"

[git]
user_name   = "Alex Example"
user_email  = "alex@example.com"
signing_key = "C:\\Users\\you\\.ssh\\id_ed25519_work"

# Literal env var — goes into child env verbatim, not redacted.
[[env]]
key   = "TF_VAR_region"
value = "norwayeast"

# 1Password ref — resolved in memory, redacted from any output that
# echoes it back, never returned over MCP.
[[env]]
key = "ARM_CLIENT_SECRET"
ref = "op://Contoso/Dev SP/credential"

# dotenv ref — looks up MY_TOKEN inside ~/.env-contoso-dev.
[[env]]
key = "EXTRA_TOKEN"
ref = "dotenv://~/.env-contoso-dev#MY_TOKEN"
```

---

## Validation behavior

- `pm profile show <name>` rejects malformed TOML and prints the parser error.
- `core.Load` runs `Validate()` after parse — schema mismatch, bad name, or
  `value`+`ref` conflict are hard errors.
- Editing via the TUI re-validates on save and refuses to write an invalid
  profile.

The schema is forward-evolvable: adding a new optional field is non-breaking;
renaming or removing one is. Breaking changes bump `schema` and add a
migration; pm refuses to load a profile whose `schema` it doesn't recognize.
