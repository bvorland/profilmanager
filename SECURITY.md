# Security Policy

## Reporting a vulnerability

**Please do not open public GitHub issues for security problems.**

Report suspected vulnerabilities privately via either channel:

- **GitHub Security Advisory (preferred):** open a draft advisory at
  <https://github.com/bvorland/profilmanager/security/advisories/new>.
  GitHub keeps it private until a coordinated disclosure date.
- **Email:** `bvorland@users.noreply.github.com` with subject prefix
  `[profilmanager security]`.

Please include:

- A short description of the issue and its impact.
- Steps to reproduce, or a minimal proof-of-concept.
- The `pm version` output (binary version + commit), OS, and shell.
- Whether you would like public credit, and the name/handle to use.

We aim to acknowledge reports within **5 business days** and to ship a
fix or mitigation within **30 days** for high-severity issues.

## Supported versions

Until `pm` reaches a 1.0 release, only the **latest** published GitHub
Release receives security fixes. Older tags are not patched.

| Version  | Supported |
| -------- | --------- |
| latest   | ✅        |
| < latest | ❌        |

## Threat model

`pm` runs as a user-mode CLI that mediates access to other developer
tools (Azure CLI, gh, kubectl, git) and to credential stores
(OS keychains, file secrets). The full assumptions and non-goals live in
[`docs/threat-model.md`](docs/threat-model.md) — read that for context
before filing a report that depends on a specific trust boundary.

In short:

- The local user account is trusted; `pm` does not defend against an
  attacker who already has shell access as the same user.
- Credentials at rest are delegated to OS-native secret stores
  (Windows Credential Manager, macOS Keychain, libsecret) by default.
- Secrets are never logged or written to crash reports.

If your report falls outside the documented threat model we will still
read it — please include why you think it should be in scope.
