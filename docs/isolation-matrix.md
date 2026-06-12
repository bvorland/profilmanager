# Azure / azd Profile Isolation Test Matrix

> Status: **v1 — diagnostic harness, no live-cloud calls yet.**
> Companion harness: [`scripts/isolation/`](../scripts/isolation/)

---

## Why this document exists

The `sample/profile.ps1` script — and the v1 architecture — both assume that the pair

```text
AZURE_CONFIG_DIR = <some path A>
AZD_CONFIG_DIR   = <some path A'>
```

is **sufficient to isolate Azure CLI and azd state between profiles.** That assumption is the load-bearing primitive under every secret-resolution, every `pm exec --profile foo -- az ...` invocation, and every MCP `switch_profile` call. If it leaks, the entire product story leaks with it.

The assumption needs to be tested empirically inside this repo. Before any provider/resolver layer is built on top of these env vars, we need:

1. A written list of **exactly what each env var is supposed to isolate**.
2. **Probe scripts** that confirm — on the operator's actual host — whether that isolation holds.
3. A way for the operator to **re-run the matrix on any new machine** and get a single pass/fail JSON report.

This document is (1) and (3); the probes under `scripts/isolation/` are (2).

---

## Scope

| In scope                                                                                                                              | Out of scope                                                                                                                                 |
| ------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- |
| `az` CLI (Azure CLI 2.x) config, profile, token cache, extension state                                                                | Resource-graph correctness and role assignments — that's threat-model territory                                                              |
| `azd` (Azure Developer CLI) config, auth, env list                                                                                    | `gh`, `kubectl`, `git` — separate matrices, separate probes (deferred to v1.1)                                                               |
| The **interaction** between `AZURE_CONFIG_DIR` and `AZD_CONFIG_DIR` when both are set differently                                     | Behavior of `op run --env-file=...` from 1Password — covered by `internal/secrets/op` tests                                                  |
| Known side-channels: **WAM (Web Account Manager) on Windows**, MSAL token cache placement, browser cookie/cache, `bicep` install path | Whether the provider adapters call into the right CLI surface — that's adapter-layer testing, separate suite                                 |
| Day-1 CI: probes must **run on any host without live Azure credentials** and skip cleanly                                             | Live-cloud verification — operators run this against their real tenants by hand; gated behind `--allow-live`, deferred from day 1            |

---

## Hypothesis (what we claim must be true)

> **H0:** Setting `AZURE_CONFIG_DIR=/path/A` in shell A and `AZURE_CONFIG_DIR=/path/B` in shell B causes every subsequent `az` command in shell A to read and write state exclusively under `/path/A`, and every `az` command in shell B exclusively under `/path/B`. **No file, lock, cache, or in-memory broker call made by either shell mutates state in the other shell's directory or in `$HOME/.azure`.**
>
> **H0':** The same claim, with `AZD_CONFIG_DIR` substituted for `AZURE_CONFIG_DIR` and `azd` substituted for `az`.
>
> **H0'':** When both env vars are set to differently-named directories per-shell, the isolation in H0 and H0' compose: az state lands in `AZURE_CONFIG_DIR`, azd state lands in `AZD_CONFIG_DIR`, and neither tool writes to the other's directory.

The probe matrix exists to falsify H0, H0', and H0''. **We expect at least one of them to leak on Windows** — see "Known unknowns" below for the leading candidates.

---

## Tests per tool

Each row is one probe script. Probe IDs match `scripts/isolation/probe-<id>.{ps1,sh}` filenames. Every probe emits the same JSON envelope (see "Probe contract") and exits `0` unconditionally so the aggregator can collect partial results.

### `az` (Azure CLI 2.x)

| Probe ID                       | What it checks                                                                                                                              | Pass criteria                                                                                                                      | Requires live login? |
| ------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------- | -------------------- |
| `az-config-dir`                | Run `az config get` (read-only) with `AZURE_CONFIG_DIR=$T`. Check `$T` exists and contains config/log/cache after the call.                 | `$T` is created and populated; `$HOME/.azure` mtime unchanged within the window of the call.                                       | No                   |
| `az-version`                   | Sanity probe: `az --version` parses, returns within timeout, doesn't write to `$HOME/.azure` when `AZURE_CONFIG_DIR` is redirected.         | `azureProfile.json` (if it exists in `$HOME/.azure`) has unchanged mtime; `$T/telemetry.txt` or similar appears in `$T` if at all. | No                   |
| `az-account-show`              | With `AZURE_CONFIG_DIR=$T`, `az account show --output json`. Expect "Please run 'az login'" stderr if not logged in.                        | Either: (a) JSON parses and `homeTenantId` is read from `$T/azureProfile.json`; or (b) `not-logged-in` detected → probe skips.     | Yes (gracefully skips if no) |
| `az-account-set-isolation`     | Two sequential `az account set --subscription <id>` calls into two different `AZURE_CONFIG_DIR` values. Inspect each profile file.          | Each `$T/azureProfile.json` records only its own active sub; neither file mentions the other's sub.                                | Yes (skip if no)     |
| `az-multi-tenant`              | If logged in to ≥2 tenants in `$HOME/.azure`, set `AZURE_CONFIG_DIR=$T` (empty), confirm `az account list` returns `[]`.                    | Empty result from the fresh dir; original `$HOME/.azure` `accounts` still intact.                                                  | Yes (skip if no)     |
| `az-extension-isolation`       | `az extension list` against `AZURE_CONFIG_DIR=$T` (empty). Expect `[]`. Then `az extension list-available` (no add — would need network).   | Empty list from fresh dir; existing extensions in `$HOME/.azure/cliextensions` not enumerated.                                     | No                   |
| `az-msal-cache-location`       | After login or simulated login, find any `msal_token_cache.bin` / `msal_http_cache.bin` / `msal_extension_cache.json`. Confirm under `$T`.  | Cache files exist only under `$AZURE_CONFIG_DIR`; **none** appear under `$HOME/.azure` or in `%LOCALAPPDATA%\.IdentityService`.    | Yes (skip if no)     |
| `az-msal-wam-broker` (Windows) | Inspect `$T/config` for `[core] enable_broker_on_windows`. Report whether WAM broker is enabled. If yes, **flag as known leak vector**.    | Probe always returns a result; `isolated=false` if WAM broker is on (see "Known unknowns").                                        | No                   |
| `az-bicep-install-path`        | `az bicep version` triggers install of bicep to `$AZURE_CONFIG_DIR/bin/bicep` historically. Probe whether the binary lands in `$T/bin`.    | `$T/bin/bicep(.exe)` exists after the call; `$HOME/.azure/bin` unchanged.                                                          | No                   |

### `azd` (Azure Developer CLI)

| Probe ID              | What it checks                                                                                                                | Pass criteria                                                                                                              | Requires live login? |
| --------------------- | ----------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------- | -------------------- |
| `azd-config-dir`      | `azd config list --output json` with `AZD_CONFIG_DIR=$T`. Inspect `$T` for `state.json` / `config.json`.                      | `$T` exists and is populated; `$HOME/.azd` mtime unchanged.                                                                | No                   |
| `azd-version`         | `azd version --output json` — sanity, no writes outside `$T`.                                                                 | Version JSON parses; no files appear in `$HOME/.azd` newer than the probe start time.                                      | No                   |
| `azd-auth-token`      | `azd auth token --output json` — expect either token JSON or `"not logged in"` error. Token file path should be under `$T`.   | If logged in: token JSON parses, the `expiresOn` is in the future, and any cache files live under `$T/auth/`. Else: skip.  | Yes (skip if no)     |
| `azd-auth-jwt-decode` | If `azd auth token` returns a token, base64-decode the middle segment and confirm `iss`/`aud` are Azure AD issuers.           | JWT decodes; `iss` matches `https://login.microsoftonline.com/<tenant>/v2.0` or v1 equivalent; `aud` is non-empty.         | Yes (skip if no)     |
| `azd-env-list`        | `azd env list --output json` with `AZD_CONFIG_DIR=$T` (empty). Expect `[]`.                                                   | Empty list; pre-existing envs in `$HOME/.azd` not visible.                                                                 | No                   |
| `azd-honors-azure-config-dir` | Cross-check: does `azd` read `AZURE_CONFIG_DIR` for its own purposes? (Per docs, `azd` shells out to `az` for some flows.) | Document whatever we observe. **No claim of pass/fail** — purely descriptive.                                              | No                   |

### Combined `AZURE_CONFIG_DIR` + `AZD_CONFIG_DIR`

| Probe ID                 | What it checks                                                                                                                                       | Pass criteria                                                                                                                                                      | Requires live login? |
| ------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------ | -------------------- |
| `combined-fresh-dirs`    | Set both env vars to fresh, distinct temp paths. Run `az config get` and `azd config list` back-to-back. Inspect both paths and both `$HOME` dirs.   | Both temp paths populated; **no writes** to `$HOME/.azure` or `$HOME/.azd` during the window.                                                                      | No                   |
| `combined-cross-write`   | Confirm `az` writes only under `AZURE_CONFIG_DIR`, never under `AZD_CONFIG_DIR`, and vice versa.                                                     | After both calls, files in `AZURE_CONFIG_DIR` are recognizably az artifacts (e.g. `azureProfile.json`); files in `AZD_CONFIG_DIR` are azd artifacts. No crossover. | No                   |
| `combined-same-process`  | If both tools are invoked from the same shell (e.g. `azd auth login` which internally calls `az`), confirm both paths still respected.               | Skipped without live login — operator must run with `--allow-live` and report.                                                                                     | Yes (skip if no)     |

### Cross-process leak vectors (Windows-specific, descriptive only)

These probes don't pass/fail — they **report what they find** so the operator can judge.

| Probe ID                | What it reports                                                                                                                          |
| ----------------------- | ---------------------------------------------------------------------------------------------------------------------------------------- |
| `windows-wam-presence`  | Whether `%LOCALAPPDATA%\.IdentityService` exists and contains broker cache. (WAM is shared per-user, ignores `AZURE_CONFIG_DIR`.)        |
| `windows-browser-sso`   | Lists default browser; notes that `az login` cookie state for `login.microsoftonline.com` persists in the browser independent of `$T`.   |
| `windows-credman-keys`  | Lists any Windows Credential Manager entries with `MicrosoftAzure` / `azurecli` / `azd` in the name. These are per-user, not per-dir.    |

---

## Probe contract

Every probe is a standalone script in `scripts/isolation/` that:

1. Takes **no required arguments**. Optional `-Json` (PowerShell) / `--json` (bash) is always implied; the only output mode is JSON to stdout.
2. **Exits `0` unconditionally.** Errors are reported in the JSON payload, never via exit code. The aggregator depends on this.
3. Performs **no writes outside `$env:TEMP\pm-isolation-<probeId>-<pid>\`** (PowerShell) or `${TMPDIR:-/tmp}/pm-isolation-<probeId>-$$/` (bash). The probe owns and cleans up that directory.
4. **Never** issues network calls that would trigger MFA, browser launch, or token refresh against a real tenant. Probes that need a live session **detect** it and skip with `skipped: true`.
5. Times out internally at 15 seconds per shell-out (`az`, `azd`). Hangs are reported as `actual: "timeout"`, `isolated: null`.

### JSON envelope

```jsonc
{
  "test": "az-config-dir",                  // probe ID (== filename minus probe- and extension)
  "tool": "az",                             // "az" | "azd" | "combined" | "windows"
  "category": "config-dir",                 // free-form short category
  "hypothesis": "AZURE_CONFIG_DIR=$T causes az to write config/cache only under $T",
  "expected": "After 'az config get' with AZURE_CONFIG_DIR=$T, $T is populated and $HOME/.azure mtime is unchanged",
  "actual": "AZURE_CONFIG_DIR=C:\\Users\\...\\AppData\\Local\\Temp\\pm-isolation-az-config-dir-1234; $T contains azureProfile.json (size=2, mtime=2026-06-09T11:39:50+02:00); $HOME/.azure mtime unchanged.",
  "isolated": true,                         // true | false | null (null when skipped or descriptive-only)
  "skipped": false,
  "skip_reason": null,                      // "az CLI not found on PATH" | "not logged in" | "windows only" | ...
  "duration_ms": 1843,
  "notes": [
    "First-run dir creation observed; if probe is re-run immediately it may report mtime delta of 0."
  ],
  "host": {
    "os": "windows",
    "os_release": "10.0.26100",
    "arch": "amd64",
    "pwsh_version": "7.4.1"
  },
  "tool_version": "azure-cli 2.61.0",       // null if tool not present
  "probe_version": "1.0.0",
  "generated_at": "2026-06-09T11:39:50+02:00"
}
```

### Aggregator output (`run-matrix.{ps1,sh}`)

```jsonc
{
  "schema": "isolation-matrix/v1",
  "generated_at": "2026-06-09T11:39:50+02:00",
  "host": { /* same shape as probe host block */ },
  "summary": {
    "total":     14,
    "isolated":   9,
    "leaked":     0,
    "skipped":    5,
    "errors":     0,
    "descriptive": 3
  },
  "probes": [ /* array of probe envelopes */ ]
}
```

The aggregator **always exits 0** for day-1 CI use. A `--strict` flag (deferred) will later flip non-zero exits when any probe reports `isolated: false`.

---

## Known unknowns — the things that probably leak

These are the candidates we expect to find broken when the operator runs the matrix on a real Windows host. They're called out here so we don't forget to check, and so the product roadmap can plan around them.

### 1. WAM (Web Account Manager) broker on Windows

Azure CLI ≥ **2.61** enables the Windows broker (WAM) by default when running on Windows. WAM stores tokens in **`%LOCALAPPDATA%\.IdentityService`** — a **per-user, machine-wide** location that is **independent of `AZURE_CONFIG_DIR`**. If broker is on:

- `az login` in shell A (with `AZURE_CONFIG_DIR=A`) can produce a token that becomes silently usable from shell B (with `AZURE_CONFIG_DIR=B`).
- `az account show` reads sub/tenant from `AZURE_CONFIG_DIR/azureProfile.json` and isolates correctly, but the **token underlying that account** is brokered.

**Implication for `pm`:** isolation of "which subscription am I targeting" works. Isolation of "which credential is in play" does not, unless we explicitly disable broker via `az config set core.enable_broker_on_windows=false` per profile.

**Mitigation shipped:** `internal/providers/az.go` (`ensureAzConfigDefaults` + `upsertINI`) writes a baseline `config` file into every per-profile `AZURE_CONFIG_DIR` at `Apply()` time, with:

```ini
[core]
enable_broker_on_windows = false
output = json
```

The write is idempotent and preserves any other operator-set keys. Apply also sets `AZURE_CORE_OUTPUT=json` in the env map. **The browser-SSO leak (#3 below) is NOT addressed by this** — the operator must still consciously pick the right identity in the browser, or use `--use-device-code`.

### 2. MSAL extension cache location

Older Azure CLI versions wrote `msal_token_cache.bin` to `$AZURE_CONFIG_DIR`. Newer versions on Windows defer to **DPAPI-protected storage in the user profile**, which is again per-user, not per-dir. Need to confirm which version draws the line.

### 3. Browser SSO cookies

`az login` (without `--use-device-code`) opens the default OS browser. The OS browser carries persistent `login.microsoftonline.com` cookies. Two `az login`s in different `AZURE_CONFIG_DIR`s, against the same browser, will **silently auto-select the previously-signed-in account** unless the user explicitly clicks "use another account".

**Implication for `pm`:** for profiles that map to **different identities**, we should default `az login` invocations to `--use-device-code` to force conscious account selection. Probe `windows-browser-sso` documents the leak; the fix is a `pm` policy, not an isolation primitive.

### 4. `bicep` binary location

Historically `bicep` installed under `$AZURE_CONFIG_DIR/bin/bicep`. If true today, `bicep` isolates per profile (good, also wasteful — N copies of the same binary). If `az` started installing `bicep` to a shared `~/.azure/bin` regardless of `AZURE_CONFIG_DIR`, that's a state leak.

### 5. Windows Credential Manager

`az` and `azd` may persist refresh tokens, service principal secrets, or device codes into Windows Credential Manager under target names like `MicrosoftAzureCli`, `azure-cli-...`. These are **per-user, not per-dir**. Probe `windows-credman-keys` enumerates them; the matrix flags them as a known leak vector and the product needs a strategy (per-profile key naming, or wipe-on-switch).

### 6. azd → az shell-out

`azd auth login` and several `azd` flows shell out to `az`. When they do, do they inherit `AZURE_CONFIG_DIR` from the parent environment, or do they reset it? If `azd` resets it, an `azd auth login` inside a profiled shell would write tokens to `$HOME/.azure`, breaking isolation in a surprising way.

**Code defense:** `internal/providers/azd.go` `Apply` sets `AZD_CONFIG_DIR`, and when a profile also has an `[azure]` section, `az.Apply` sets `AZURE_CONFIG_DIR` on the same env map — so child `az` processes spawned by `azd` inherit both.

**Probed:** Promoted from "known unknown" to "probed via `windows-azd-shellout`" (see `scripts/isolation/windows-azd-shellout/probe.ps1`, schema in sibling `expected.json`). The probe drives `azd config show` + `azd version --output json` with both env vars pointed at fresh temp dirs and proves:

- both calls exit 0 (env vars don't make `azd` itself refuse to start);
- `$HOME/.azure` and `$HOME/.azd` see no new files and no mtime bump during the call window (no leak under non-auth flows).

**What this probe does NOT cover (manual smoke test only):** `azd auth login` itself. A real login requires interactive browser/device-code auth against a real tenant — out of CI scope. Operators who want to close this gap manually should run:

```pwsh
$T = (New-Item -ItemType Directory -Path (Join-Path $env:TEMP "pm-azd-live-$(Get-Random)")).FullName
$env:AZURE_CONFIG_DIR = $T; $env:AZD_CONFIG_DIR = $T
azd auth login --use-device-code
# then verify token files landed under $T, not under $HOME/.azure
Get-ChildItem $T -Recurse; Get-ChildItem $HOME/.azure -Recurse | Where { $_.LastWriteTime -gt (Get-Date).AddMinutes(-2) }
```

If new files appear under `$HOME/.azure` within the 2-minute window, the leak is confirmed and the probe should be upgraded to live mode behind an `--allow-live` flag.

### 7. Telemetry & survey state

Azure CLI writes telemetry-survey state (`telemetry.txt`, `survey.json`) — likely per `AZURE_CONFIG_DIR`, but worth confirming the telemetry endpoint isn't keyed by anything machine-wide that would correlate profiles.

---

## How to run

### Day-1 (no Azure creds required, runs in CI)

```pwsh
# PowerShell (Windows / macOS / Linux with pwsh)
pwsh -NoProfile -File scripts/isolation/run-matrix.ps1 -OutputFile isolation-report.json
```

```sh
# bash (macOS / Linux)
bash scripts/isolation/run-matrix.sh --output isolation-report.json
```

Both print a human-readable summary table to stdout and write the full JSON to the path given. Exit code is always 0 in v1.

### When operator has live creds (manual, not CI)

Same commands, but with at least one Azure tenant signed in via `az login` / `azd auth login` beforehand. Probes that previously skipped will run for real. This is the moment to discover which of the "Known unknowns" actually leak on this host.

A future `--strict` flag will fail the run if any probe reports `isolated: false`. Until then, **read the JSON**. The summary table is for triage; the JSON is the source of truth.

---

## What to do when a probe reports `isolated: false`

1. **Don't panic, don't silently fix.** File an issue tagged `type:bug` with the probe JSON attached.
2. Decide whether the leak is fixable in `pm` (e.g. set `enable_broker_on_windows=false` per profile) or a vendor limitation we document and route around (e.g. "always use device-code login for distinct identities").
3. Add a regression test that re-runs the failing probe in CI's `--allow-live` lane (when that lane exists) so the leak can't silently come back.

---

## Versioning

This matrix is `isolation-matrix/v1`. The probe contract (JSON envelope, exit-0 rule, 15s timeout) is frozen at v1. Adding probes is non-breaking; changing field names or exit semantics requires a `v2` bump.
