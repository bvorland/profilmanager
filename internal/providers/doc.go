// Package providers hosts the per-tool integrations (az, azd, gh, kubectl,
// git) that translate a core.Profile into the env vars and on-disk state a
// tool needs, plus a Whoami inspector that never triggers interactive
// login.
//
// Iron rule: external CLIs are untrusted. Every adapter shells
// out to the official CLI with --output json (or equivalent) and parses
// structured output. Pretty text is for humans, never for the parser.
//
// Drift detection lives alongside the adapters because the rules
// ("az subscription disagrees with azd subscription") are inherently
// cross-tool — see drift.go.
package providers
