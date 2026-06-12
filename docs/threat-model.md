# Threat model (v1)

What `pm` defends against, what it doesn't, and where the residual risk lives.
Honest about tradeoffs; defensible at 2am.

This document is the threat model, not a vulnerability-disclosure policy.
The reporting channel and SLA live in [`SECURITY.md`](../SECURITY.md) —
see [Reporting issues](#reporting-issues) at the end.

---

## Assets

What we're protecting, ranked by sensitivity:

| Asset                                | Sensitivity   | Where it lives                                                                 |
|--------------------------------------|---------------|--------------------------------------------------------------------------------|
| **Resolved secret values**           | **CRITICAL**  | Process memory only (parent during resolution; child env during `pm exec`). Never disk, never MCP, never logs. |
| Profile metadata                     | Low–Medium    | TOML files under `ProfilesDir()`. Includes tenant/sub IDs, repo URLs, secret *refs* (not values). |
| Session ID                           | Low           | Env vars; copied into per-session state filenames (sanitized).                 |
| Audit logs (`secrets.log`, `mcp.log`)| Low–Medium    | `<StateDir>/audit/*.log`. Contains refs (where secrets live), command names, args (pre-redacted), exit codes. |
| Profile name                         | Low           | Filename + TOML field. Constrained to `^[A-Za-z0-9._-]+$`.                     |

The asymmetry is deliberate: profile metadata is enough to *find* a secret in
1Password, but not to *be* the secret. Resolved values are the only thing
treated as critical.

---

## Trust boundaries

Three boundaries `pm` crosses:

1. **User shell ↔ `pm` process.** `pm` trusts env vars set in the operator's
   shell (`PM_SESSION_ID`, `COPILOT_AGENT_SESSION_ID`, `AZURE_CONFIG_DIR`,
   etc.) and the operator's TTY input. The shell is part of the operator's
   TCB; if it's malicious, this is the wrong layer to defend at.
2. **`pm` process ↔ child process (`pm exec`).** The child sees a curated env
   (provider applies + literal `[[env]]` + resolved refs). The child is
   trusted with the resolved values — that's the entire point of the call —
   but `pm` does not trust the child's stdout/stderr: it runs the redactor
   over them before returning to an MCP peer.
3. **`pm` process ↔ MCP stdio peer.** The peer (Copilot CLI, Claude Desktop,
   Squad) is a separate process that issues JSON-RPC requests. We treat it as
   a potentially confused-deputy: it may relay attacker-influenced prompts.
   The defenses are the command allowlist, the redactor, and the iron rule
   about secrets.

---

## Threats and mitigations

### T1 — MCP peer attempts to extract secret values

**Threat:** an MCP peer (or its upstream agent) asks `pm` to reveal a secret
value, either directly (`resolve_secret_ref`) or indirectly (e.g.,
`exec_with_profile` with `command: "echo"` and the secret in args).

**Mitigations (shipped):**

- `resolve_secret_ref` returns metadata only — `DescribeRef`, not
  `Resolve`. No code path in the handler ever calls `Reveal()`.
- `exec_with_profile` allowlist: default `{ az, azd, gh, kubectl, git }`.
  `echo`, `cat`, `printenv`, etc. are rejected before any process spawns.
  The allowlist matches on the bare command basename (case-insensitive,
  `.exe` suffix stripped); **path-prefixed input is rejected outright**
  (`C:\Users\Public\az.exe`, `/usr/bin/az`, `./az`, `..\az.exe`, UNC
  paths) so a caller cannot route the call through an attacker-planted
  binary that happens to share a basename with an allowlisted command.
  After the check passes, `exec.LookPath` resolves the bare name via
  the operator's PATH (see T6) and the audit log records the resolved
  absolute path for forensics.
- Output redactor: every resolved secret is replaced with `<REDACTED>` in
  returned stdout/stderr (longest-secret-first; min length 4 bytes).
- Audit log records `command`, `args` (already passed through the redactor),
  `result`, `exit_code`, and a 256-byte redacted `output_preview` —
  never the full stream, never the env block, never a resolved value.
- `internal/secrets.Secret` type — `String()`, `MarshalJSON()`,
  `MarshalText()` all refuse or redact, so an accidental `fmt.Printf` or
  `json.Marshal` cannot leak.

**Residual risk:** the redactor is best-effort. A secret that's also a common
word (or shorter than 4 bytes) won't be registered. The allowlist can be
expanded by `mcp.SetConfig`; if an operator adds `echo`, that's an operator
choice. The Go runtime may have copied bytes (escape analysis, stack copies)
before `Zero()` ran — `Zero()` raises the bar, doesn't eliminate the risk.

---

### T2 — Malicious profile with embedded malicious env values

**Threat:** an operator imports a profile from an untrusted source (a coworker's
gist, a screenshot OCR, `pm import-mj` from a tampered `profile.ps1`). The
profile contains `[[env]] LD_PRELOAD=...` or `PATH=/tmp/evil:...` or a `ref`
pointing at a phished 1Password vault.

**Mitigations (shipped):**

- Profile load validates schema, name, and `value`/`ref` exclusivity. It
  does NOT scan env keys for "dangerous" names — that's a losing arms race.
- `pm profile show --redacted` exists so reviewers can read a profile's body
  before adopting it (subscription/tenant IDs masked, refs replaced with
  `<ref>`).
- `pm exec` does not invoke a shell. Args go through `os.Exec` directly;
  word-splitting and glob expansion don't apply.
- `pm import-mj` is idempotent and skip-by-default; it cannot silently
  clobber a hand-edited profile. `--dry-run` shows what it would write.

**Residual risk:** an operator who imports a malicious profile and runs
`pm exec` against it gets exactly what they asked for. The defense is social
("don't import profiles from strangers"), not technical. A future signed-
profile model (out of scope for v1) would help.

---

### T3 — Path-traversal / symlink attacks via profile name

**Threat:** an attacker-controlled value reaches `core.ProfilePath(name)`
with `name = "../../../../etc/passwd"` or `name = ".ssh"`, hoping to
trick `Save` into overwriting an unrelated file.

**Mitigations (shipped):**

- `core.ValidateName` enforces `^[A-Za-z0-9._-]+$` and explicitly rejects
  `.` and `..`. Empty rejected.
- Enforced on every `Load`, `Save`, `SetActiveProfile`, `SetLastProfile`,
  `ProfilePath`.
- `state.sanitizeID` replaces every non-`[A-Za-z0-9._-]` rune with `_`
  before building the per-session state filename. The original session ID
  string remains the canonical identity for comparisons and logging.

**Residual risk:** a profile name like `dev` plus a `profiles_dir` that is
itself a symlink to somewhere unexpected is a configuration error we don't
defend against. If the operator's `XDG_CONFIG_HOME` points at
`/etc/shadow`, that's outside our threat model.

---

### T4 — Log file disclosure of secrets

**Threat:** an operator shares an audit log for debugging, or backs up
`<StateDir>` to cloud storage, and a secret ends up in the wrong hands.

**Mitigations (shipped):**

- The audit log schema has no value field. `LogResolve` takes no value
  argument. There is no debug flag that can be flipped to include values.
- The MCP `output_preview` field is the first 256 bytes of redacted stdout
  — the redactor has already replaced every registered secret with
  `<REDACTED>` before the slice is taken.
- Ref strings ARE logged (they're metadata: "this secret lives at
  `op://Personal/X/credential`"). Operators who want to share a log for
  debugging should review the refs and decide whether to redact them
  manually.

**Residual risk:** ref strings are sometimes sensitive (knowing a secret
lives at `op://Production/AzureDevOps/credential` tells an attacker where
to phish). Operators who routinely paste logs into public bug trackers
should `grep -v "ref"` first. We do not encrypt audit logs at rest; OS-level
disk encryption is the right layer.

---

### T5 — Audit log tampering

**Threat:** an attacker with write access to `<StateDir>` deletes or
rewrites audit entries to cover their tracks.

**Mitigations (shipped):**

- Append-only with `O_APPEND` and a package-level mutex.
- Rotation is size-based and inline (no background goroutine that could
  truncate mid-write).

**Residual risk:** an attacker with write access to `<StateDir>` can trivially
delete the file or replace it. This is not a cryptographic audit log. The
defense against `<StateDir>` being writable by the attacker is OS-level
file permissions, not pm. We do not sign log entries; if you need
tamper-evidence, forward the log to an append-only sink (syslog, CloudWatch,
etc.) — that's a v2 addition the audit format is intentionally simple
enough to support.

---

### T6 — Opportunistic disk reads of the state directory

**Threat:** another process running as the same user reads
`<StateDir>/sessions/<id>.profile` to learn which profile is active, or
reads the profile TOMLs to enumerate the operator's accounts.

**Mitigations (shipped):**

- All on-disk files are created with mode `0o644` on Unix (Windows ignores
  the mode but inherits the user's ACL). Files are NOT world-readable on
  Unix in the sense that matters — the parent directory `0o755` exposes
  the names but the user's home directory is typically `0o700`.
- No secret values on disk, period. The worst-case read is profile
  metadata + audit log entries — both classified Low–Medium above.

**Residual risk:** any process running as the operator can read the state
dir. This is not multi-tenant software; we trust other processes running as
the same user. If you need stronger isolation, run `pm` as a dedicated
service account with its own home directory.

---

### T7 — WAM / credential-broker bypass of `AZURE_CONFIG_DIR` isolation

**Threat:** Azure CLI ≥ 2.61 on Windows enables the WAM (Web Account
Manager) broker by default. WAM stores tokens in
`%LOCALAPPDATA%\.IdentityService` — a per-user, machine-wide location
**independent of `AZURE_CONFIG_DIR`**. A token obtained by `az login` in
profile A becomes silently usable from `pm exec --profile B -- az ...`.
The isolation matrix flagged this as the largest leak vector on
Windows.

**Mitigation (shipped):**

- `internal/providers/az.go::Apply` writes a baseline `config` file into
  every per-profile `AZURE_CONFIG_DIR` containing:
  ```ini
  [core]
  enable_broker_on_windows = false
  output = json
  ```
- The write is idempotent and preserves any other operator-set keys.
- With the broker disabled, MSAL falls back to the file-based token cache
  under `AZURE_CONFIG_DIR`. Two profiles == two caches == two real
  credentials.

**Residual risk:** the browser-SSO leak (separate from WAM). `az login`
opens the default OS browser; if the browser is already signed in to
`login.microsoftonline.com`, it auto-selects the previous account. The fix
is operator policy ("prefer `az login --use-device-code` for multi-identity
setups"), not an isolation primitive `pm` can enforce. See
[`docs/isolation-matrix.md`](isolation-matrix.md) §1 and §3 for the full
analysis.

---

### T8 — Atomic-write window vulnerabilities

**Threat:** a writer is killed (Ctrl-C, OOM, machine reboot) between
"start writing profile.toml" and "finish writing profile.toml," leaving a
half-written file that breaks subsequent reads. Two concurrent `pm`
invocations race on the same state file and one's write clobbers the
other's.

**Mitigations (shipped):**

- Every write to a `pm`-managed file is `write-temp + fsync + rename`. The
  temp file lives in the same directory as the target (same-volume rename
  is atomic on every OS we support). On failure, the temp is removed; we
  never leave a partial file.
- State files in `internal/state` (active-profile-per-session, last-profile)
  serialize their rename behind a `gofrs/flock` advisory lock on a sidecar
  `<target>.lock` file. Sidecar is intentional — locking the target on
  Windows holds a handle that interferes with the rename.
- Concurrency tested in `internal/state/session_test.go::TestConcurrentSetActiveProfile`
  (16 goroutines, no leftover temps, final state consistent).

**Residual risk:** profile TOMLs (`internal/core/profile.go::Save`) use the
atomic-write pattern without flock — the operator is presumed not to race
themselves on a single `.toml` from one machine. If two machines share a
profile directory over NFS/SMB and write simultaneously, advisory locks
won't help; that's not a supported configuration.

---

## Out of scope for v1 (explicit, not hidden)

These are real concerns we have deliberately not addressed in v1. Listed so
they cannot be "discovered" as gaps later.

- **Encryption of profile.toml at rest.** OS-level disk encryption (BitLocker,
  FileVault, LUKS) is the right layer. Adding pm-side crypto would expand the
  attack surface (key handling) for marginal benefit.
- **Signed profiles.** Operators don't share profile files between operators
  today. If a real workflow emerges, signing is additive; the schema can
  carry a `[signature]` block without breaking v1 readers.
- **MFA on MCP `exec_with_profile`.** The MCP peer is already trusted by
  virtue of being on stdio. The correct control is operator-side: don't
  register untrusted MCP servers. Adding MFA inside the protocol doesn't
  change the threat model — a compromised agent could just ask the operator
  to MFA on the agent's behalf.
- **Supply-chain verification of `pm` itself.** Checksums on release
  artifacts, sigstore signing, reproducible builds — all valid concerns, all
  owned by the release pipeline (`dist/`,
  `.github/workflows/release.yml`). Not addressed in v1; planned for v1.1.
- **Live-cloud isolation verification.** The isolation probes
  (`scripts/isolation/`) run without live credentials in v1. A `--allow-live`
  mode that actually drives `az login` / `azd auth login` against a test
  tenant is planned; until it lands, operators run the manual smoke tests
  documented in `docs/isolation-matrix.md` §6.
- **Tamper-evident audit log.** Append-only with `O_APPEND` is not the
  same as cryptographically signed. Operators who need tamper-evidence
  should forward to an append-only sink.

---

## Reporting issues

The canonical reporting channel, supported versions, and response SLA
live in [`SECURITY.md`](../SECURITY.md). **Please do not open public
GitHub issues for suspected vulnerabilities.** Use one of:

1. **GitHub Security Advisory (preferred):** open a draft advisory at
   <https://github.com/bvorland/profilmanager/security/advisories/new>
   — GitHub keeps it private until a coordinated disclosure date.
2. **Email:** `bvorland@users.noreply.github.com` with subject prefix
   `[profilmanager security]`.

We aim to acknowledge within **5 business days** and to ship a fix or
mitigation within **30 days** for high-severity issues.
