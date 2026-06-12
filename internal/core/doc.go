// Package core holds the profile model, validation, on-disk paths, and
// session-state interfaces shared by the CLI, TUI, and MCP entry points.
//
// Architectural rule: no business logic in the entry points — every
// face of the binary calls into core.
package core
