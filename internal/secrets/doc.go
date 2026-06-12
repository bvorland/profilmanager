// Package secrets implements the v1 secret-reference resolvers used to
// expand profile [[env]] entries at exec time.
//
// # Iron rule
//
// Resolved secret values MUST NOT cross the MCP boundary, appear in
// `--json` output, be logged, or be written to disk. They exist only in:
//
//  1. The memory of the resolving Go process (cleared with [Secret.Zero]
//     after use where feasible).
//  2. The environment of a child process started via `pm exec`.
//
// Callers receive a [Secret] from [Resolver.Resolve]. To get at the
// plaintext, the caller MUST explicitly invoke [Secret.Reveal] — this is
// greppable by design (grep for ".Reveal(" to audit every leak surface).
//
// # Resolvers
//
// v1 ships three built-in resolvers, registered automatically:
//
//   - op       — 1Password CLI (`op read op://Vault/Item/field`)
//   - wincred  — Windows Credential Manager (Windows only; stub elsewhere)
//   - dotenv   — literal values + `dotenv://path/to/file#KEY`
//
// Key Vault and Bitwarden are v1.1 and intentionally out of scope.
package secrets
