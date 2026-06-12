# Agent integration

How to wire `pm` into an agent — Copilot CLI, Squad, Claude Desktop, a custom
MCP client, or a plain shell script consuming `--json` output.

The contract is intentionally narrow: an MCP server with 7 tools, a CLI with
a stable `--json` envelope, four exit codes, and one iron rule about secrets.

---

## MCP server registration

`pm` ships an embedded MCP server reachable as `pm mcp serve`. Register it
in `.copilot/mcp-config.json` (Copilot CLI), `claude_desktop_config.json`
(Claude Desktop), or wherever your client looks:

```json
{
  "mcpServers": {
    "profilmanager": {
      "command": "pm",
      "args": ["mcp", "serve"]
    }
  }
}
```

The server reports its name as `profilmanager` and its version as
`internal/mcp.Version` (currently `0.1.0`). Transport is stdio; no network
ports are opened.

The Copilot CLI skill at
[`.copilot/skills/profilmanager/SKILL.md`](../.copilot/skills/profilmanager/SKILL.md)
teaches the agent the conventions below. Other MCP clients can read it as a
prompt prefix.

---

## The 7 MCP tools (v1)

All tools return both a text `Content` payload (JSON for human/agent reading)
and a `StructuredContent` payload (the same data as a Go-typed object for
clients that handle structured content).

| Tool                  | Purpose                                                              | Mutating? |
|-----------------------|----------------------------------------------------------------------|-----------|
| `list_profiles`       | Enumerate every profile, metadata only                               | No        |
| `get_profile`         | Return one profile's full body (refs kept as metadata, never values) | No        |
| `get_active_profile`  | Read the session's active-profile marker                             | No        |
| `switch_profile`      | Set the session's active-profile marker                              | Yes       |
| `whoami`              | Per-provider login state + cross-tool drift report                   | No        |
| `resolve_secret_ref`  | Describe a secret ref's backend, existence, and metadata             | No        |
| `exec_with_profile`   | Run an allowlisted command with profile env (incl. resolved secrets) | Yes       |

### `list_profiles`

```json
{ "name": "list_profiles", "arguments": {} }
```

Returns profiles + per-resolver availability:

```json
{
  "profiles": [
    { "name": "dev", "label": "Dev", "color": "Cyan", "path": "...", "has_azure": true, "has_azd": true, "has_gh": true, "has_kube": false, "has_git": true, "env_count": 3 }
  ],
  "load_errors": [],
  "resolvers": { "op": { "available": true }, "wincred": { "available": true }, "dotenv": { "available": true } },
  "profiles_dir": "C:\\Users\\you\\AppData\\Roaming\\profilmanager\\profiles",
  "profile_count": 1
}
```

### `get_profile`

```json
{ "name": "get_profile", "arguments": { "name": "dev" } }
```

Returns the full TOML body as a JSON object. `[[env]]` entries with `ref`
keep the ref string verbatim (where the secret lives); the value is **never**
included.

### `get_active_profile`

```json
{ "name": "get_active_profile", "arguments": {} }
```

```json
{ "active": "dev", "session_id": "8c1...-...", "session_source": "pm-session" }
```

`session_source` is one of `pm-session`, `copilot-session`, `wt-session`,
`ppid-fallback`. Anything but `pm-session` is suboptimal; `ppid-fallback`
is fragile and the agent should suggest the operator run `pm session init`.

### `switch_profile`

```json
{ "name": "switch_profile", "arguments": { "name": "prod" } }
```

```json
{
  "active": "prod",
  "previous": "dev",
  "session_id": "8c1...-...",
  "session_source": "pm-session",
  "note": "active-profile metadata updated; calling shell env is NOT mutated — use exec_with_profile or `pm exec` to run commands with this profile"
}
```

Pass `name: ""` to clear the active marker.

### `whoami`

```json
{ "name": "whoami", "arguments": {} }
```

Returns `{ providers: [...], drift: [...], resolvers: {...} }` — same shape as
the CLI's `pm whoami --json`.

### `resolve_secret_ref`

```json
{ "name": "resolve_secret_ref", "arguments": { "ref": "op://Personal/GitHub Token/credential" } }
```

```json
{
  "ref": "op://Personal/GitHub Token/credential",
  "resolver": "op",
  "available": true,
  "exists": true,
  "metadata": { /* backend-specific, no value */ },
  "note": "resolved value is NEVER returned over MCP — use exec_with_profile to consume secrets"
}
```

**This tool never returns the resolved value.** If you need the value, the
only path is `exec_with_profile` — which puts the value into a child process's
env, never into a response.

### `exec_with_profile`

```json
{
  "name": "exec_with_profile",
  "arguments": {
    "command": "az",
    "args": ["account", "show"],
    "profile": "prod",
    "timeout_seconds": 30
  }
}
```

Guardrails (all non-negotiable; see [`docs/threat-model.md`](threat-model.md) for the full
threat model):

- **Command allowlist:** default is `az`, `azd`, `gh`, `kubectl`, `git`.
  Matched on basename, case-insensitive. Anything else is rejected with
  `ErrCommandNotAllowed` before any process is spawned.
- **No shell:** `exec.CommandContext(ctx, command, args...)`. Never
  `cmd /c` / `sh -c`. Args are not joined.
- **Timeouts:** default 60s, hard cap 300s.
- **Output cap:** 1 MiB per stream; excess discarded with a truncation note.
- **Redaction:** every resolved secret value is replaced with `<REDACTED>`
  in returned stdout/stderr (longest-secret-first; secrets shorter than 4
  bytes are not registered to avoid destroying legitimate output).
- **Audit log:** every invocation appended to `<StateDir>/audit/mcp.log`.
  Args are pre-redacted; full stdout/stderr are NOT logged (only a redacted
  256-byte preview).

The response shape:

```json
{
  "command": "az",
  "args": ["account", "show"],
  "profile": "prod",
  "exit_code": 0,
  "stdout": "...redacted...",
  "stderr": "",
  "duration_ms": 423,
  "truncated": false
}
```

---

## CLI fallback (the `--json` envelope)

Structured-output verbs support `--json`. As of v1 these are
`pm profile list`, `pm profile show`, `pm whoami`, and `pm doctor`.
Three rules govern the output:

1. **Named structs, snake_case keys, stable shapes.** Fields may be added;
   never renamed or removed. See `pm profile list --json`,
   `pm profile show --json`, `pm whoami --json`, `pm doctor --json`.
2. **Pretty-printed by default** (2-space indent, no HTML escape). Pipe
   through `jq -c` if you need compact output — we don't ship a
   `--json-compact` flag.
3. **Errors are envelopes on stderr:**
   ```json
   { "error": "profile \"foo\" not found at /…/foo.toml", "code": "invalid_usage" }
   ```
   The `code` string mirrors the exit code (`ok` / `invalid_usage` /
   `not_implemented` / `error`).

`pm profile list --json` is deliberately metadata-only (no `[[env]]`, no
refs) — listing must always be safe to paste into a bug report. Use
`pm profile show --json <name>` to get one profile's full body, or
`--redacted` to get a body safe to share publicly.

---

## Exit codes

Four codes, every verb obeys them:

| Code | Meaning                                                                 |
|-----:|-------------------------------------------------------------------------|
|   `0` | success                                                                |
|   `1` | generic error (I/O, unanticipated failure)                             |
|   `2` | invalid usage (bad arg, bad flag value, profile not found, etc.)       |
|  `64` | command exists in `--help` but its implementation is not wired yet     |

The `64` slot is a deliberate signal to scripts and agents that the verb's
*surface* exists but the body is a stub — retry-after-upgrade is the right
response, not user-fix.

`pm exec` propagates the child's exit code unchanged, except for timeout
(returns `1` with a clear message).

---

## Session-ID detection

`pm` scopes "active profile" to a session. Resolution order (first non-empty
wins):

1. **`PM_SESSION_ID`** — canonical. Set by `pm session init` (which `pm
   shell-init` calls automatically).
2. **`COPILOT_AGENT_SESSION_ID`** — Copilot CLI sets this; matches the
   Copilot CLI session folder UUID.
3. **`WT_SESSION`** — Windows Terminal session GUID.
4. **PPID fallback** — last resort, prefixed `ppid-<pid>`.

`pm doctor` and the MCP `get_active_profile` tool both report which source
won. **PPID fallback is fragile:** PPIDs are recycled across process exits,
and `sudo` / `tmux` / process supervisors all break the assumption that PPID
identifies "the operator's terminal." If an agent sees
`session_source: "ppid-fallback"`, it should suggest the operator run
`pm session init` (or install `pm shell-init` permanently).

---

## Audit log format

Two audit logs:

- **`<StateDir>/audit/secrets.log`** — every `Resolver.Resolve` attempt
  (success, miss, error). Owned by `internal/secrets/audit.go`.
- **`<StateDir>/audit/mcp.log`** — every MCP tool invocation that touches
  secrets or runs a child process (`resolve_secret_ref`, `exec_with_profile`).
  Owned by `internal/mcp/audit.go`.

Both files are **newline-delimited JSON**, one object per line. Schema:

```json
{
  "ts":       "2026-06-09T10:32:14.012345Z",
  "session":  "<session id>",
  "profile":  "dev",
  "tool":     "exec_with_profile",
  "command":  "az",
  "args":     ["account", "show"],
  "ref":      "op://Personal/Token/credential",
  "result":   "ok",
  "exit_code": 0,
  "duration_ms": 123,
  "output_preview": "first 256 bytes of redacted stdout",
  "error":    ""
}
```

**Excluded by construction:** env values, full stdout, full stderr, resolved
secret values, ref *values*. The `ref` field is metadata (where the secret
lives), not the secret itself.

**Rotation:** size-based, 5 MiB per file, 3 history files kept (`.1`, `.2`,
`.3`). Runs inline on the next append; no background goroutine.

---

## The iron rule

Resolved secret values do not cross the MCP boundary, do not appear in
`--json` output, and are not logged. There is no debug flag to flip this off.
There is no `--reveal-for-debugging` option. If an agent or operator needs the
value, the only path is `exec_with_profile` (puts it into a child env block)
or `pm exec` (same, from the CLI side).

This is enforced in code by:

- `internal/secrets.Secret` — opaque type, only `Reveal()` exposes the
  plaintext. `String()`, `MarshalJSON()`, `MarshalText()` all redact or
  refuse. `Zero()` overwrites the backing bytes.
- `resolve_secret_ref` MCP tool — `DescribeRef` returns metadata only and is
  the only path the handler uses.
- `exec_with_profile` redactor — walks captured stdout/stderr
  longest-secret-first and replaces every occurrence with `<REDACTED>` before
  returning to the agent.
- Audit log schema — `LogResolve` takes no value argument; `mcp.AuditEntry`
  has no value field.

A code-review check is one `rg "\.Reveal\("` away: every leak surface in the
repo is named.
