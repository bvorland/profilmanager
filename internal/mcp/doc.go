// Package mcp implements the embedded Model Context Protocol server
// that ships inside the pm binary as `pm mcp serve`.
//
// The server exposes profile context to MCP-capable agents (Copilot CLI,
// Squad, Claude Desktop, …) over stdio JSON-RPC. The tool surface is
// intentionally small and metadata-first:
//
//	list_profiles       — metadata (no env, no refs, no values)
//	get_profile         — full TOML body minus resolved secret values
//	get_active_profile  — current session's active profile name
//	switch_profile      — write active-profile metadata for this session
//	whoami              — provider drift report (calls internal/providers)
//	resolve_secret_ref  — metadata only (ref + existence); NEVER the value
//	exec_with_profile   — allowlisted, audited, redacted child process
//
// # Iron rule
//
// Resolved secret values never cross the MCP protocol boundary. They are
// materialised only inside the pm process — once, briefly, in [Exec] —
// long enough to be set on the child process env block and to redact any
// leakage from the child's stdout/stderr. Then [secrets.Secret.Zero] is
// called and the bytes are dropped.
//
// # SDK
//
// The server is built on github.com/mark3labs/mcp-go, chosen for active
// maintenance and comprehensive tool/stdio support.
package mcp
