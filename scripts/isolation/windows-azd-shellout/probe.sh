#!/usr/bin/env bash
# scripts/isolation/windows-azd-shellout/probe.sh
#
# Linux/macOS no-op-with-explanation companion to probe.ps1.
#
# Why this is a stub:
#   The "azd -> az shell-out" risk (Known Unknown #6)
#   is rooted in how Windows process startup propagates environment variables
#   through the WAM (Web Account Manager) auth flow. On Linux/macOS, azd's
#   shell-out to az inherits the parent env via standard POSIX exec semantics,
#   which we cover with the unit-level isolation tests under
#   internal/providers/fakecli_test.go and probe-az-config-dir.{ps1,sh}.
#
#   Running probe.ps1's full leak detection on POSIX would still be informative
#   (and the .ps1 file does NOT gate on $IsWindows — it just annotates the
#   platform). We deliberately keep this .sh as a stub for the matrix runner so
#   that "azd-shellout" is a Windows-only matrix cell for v1. Operators on
#   Linux/macOS who want the data can run probe.ps1 via `pwsh probe.ps1`
#   directly.
#
# Exit code: 0 (always). Emits a skipped: true envelope so the matrix renderer
# treats it the same as any other skipped probe.

set -e

cat <<'JSON'
{
  "test": "windows-azd-shellout",
  "tool": "azd",
  "category": "cross-tool",
  "hypothesis": "azd preserves AZURE_CONFIG_DIR and AZD_CONFIG_DIR when shelling out to az during a config-only flow; $HOME/.azure and $HOME/.azd are untouched.",
  "expected": "Probe runs on Windows only for v1; POSIX platforms are covered by probe-az-config-dir + internal/providers/fakecli_test.go.",
  "actual": "",
  "isolated": null,
  "skipped": true,
  "skip_reason": "Windows-only probe for v1; run probe.ps1 via pwsh for informational POSIX result.",
  "duration_ms": 0,
  "notes": ["platform=posix-stub"],
  "host": {"os": "posix"},
  "tool_version": null,
  "probe_version": "1.0.0",
  "generated_at": "1970-01-01T00:00:00Z"
}
JSON
exit 0
