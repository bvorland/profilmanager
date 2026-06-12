package mcp

import (
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Config holds the runtime knobs for the embedded MCP server.
//
// All fields have safe defaults — see [DefaultConfig]. Tests and
// future config-file plumbing call [SetConfig] to override.
//
// Why a process-global rather than passing through every handler? The
// MCP SDK's tool-handler signature is fixed; the alternative is wrapping
// each handler in a closure over a *Server, which we already do for the
// per-server bits. Config is intentionally read-mostly: rare overrides
// (tests, future `pm config set mcp.*`) take a lock; hot-path reads in
// [Exec] take a read lock only.
type Config struct {
	// AllowedCommands is the allowlist of executable names that
	// exec_with_profile will run. Matched against the basename of the
	// requested command (case-insensitive on Windows). Anything not in
	// this set returns a tool error before any process is spawned.
	//
	// Defaults to the v1 set: az, azd, gh, kubectl, git.
	AllowedCommands []string

	// DefaultExecTimeout is the timeout applied to exec_with_profile
	// when the caller does not specify one. 60s by default.
	DefaultExecTimeout time.Duration

	// MaxExecTimeout is the upper bound on any caller-supplied timeout.
	// A request asking for more is clamped to this value. 300s by default.
	MaxExecTimeout time.Duration

	// MaxOutputBytes caps the size of stdout/stderr returned in the
	// exec_with_profile tool result. Beyond this size the stream is
	// truncated and a note is appended. 1 MiB by default.
	MaxOutputBytes int
}

// DefaultConfig returns the v1 defaults. Callers MUST NOT mutate the
// returned slice — use [SetConfig] to change the allowlist.
func DefaultConfig() Config {
	return Config{
		AllowedCommands:    []string{"az", "azd", "gh", "kubectl", "git"},
		DefaultExecTimeout: 60 * time.Second,
		MaxExecTimeout:     300 * time.Second,
		MaxOutputBytes:     1 << 20, // 1 MiB
	}
}

var (
	cfgMu sync.RWMutex
	cfg   = DefaultConfig()
)

// SetConfig replaces the package-level config. Zero-valued fields fall
// back to their default. Returns the effective config (defaults
// substituted in) so tests can assert on the result.
func SetConfig(c Config) Config {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	merged := DefaultConfig()
	if len(c.AllowedCommands) > 0 {
		merged.AllowedCommands = append([]string(nil), c.AllowedCommands...)
	}
	if c.DefaultExecTimeout > 0 {
		merged.DefaultExecTimeout = c.DefaultExecTimeout
	}
	if c.MaxExecTimeout > 0 {
		merged.MaxExecTimeout = c.MaxExecTimeout
	}
	if c.MaxOutputBytes > 0 {
		merged.MaxOutputBytes = c.MaxOutputBytes
	}
	cfg = merged
	return merged
}

// GetConfig returns a copy of the current config. Safe for concurrent use.
func GetConfig() Config {
	cfgMu.RLock()
	defer cfgMu.RUnlock()
	out := cfg
	// Defensive copy of the slice — callers can't mutate the package state.
	out.AllowedCommands = append([]string(nil), cfg.AllowedCommands...)
	return out
}

// IsAllowedCommand reports whether name passes the allowlist. The check
// matches on the bare command basename (case-insensitive); a request
// like "az.exe" matches the "az" allowlist entry. This is the only
// correctness guard between an agent and arbitrary shell execution, so
// we err on the strict side: no path separators (caller cannot pre-resolve
// the binary they want us to spawn — PATH lookup is ours to do), no shell
// metacharacters, no absolute paths, only the exact basename.
//
// Security note: stripping the path BEFORE comparing would let an attacker
// pass "C:\Users\Public\az.exe" and have it match the "az" entry, then
// have us exec the attacker-planted binary with profile secrets in its
// env. We instead reject any path-containing input outright; callers MUST
// pass a bare basename and let [Exec] do the PATH lookup.
func IsAllowedCommand(name string) bool {
	bare := commandBasename(name)
	if bare == "" {
		return false
	}
	c := GetConfig()
	for _, allowed := range c.AllowedCommands {
		if strings.EqualFold(bare, allowed) {
			return true
		}
	}
	return false
}

// commandBasename returns the bare basename of name if and only if name
// is itself already a bare basename — no path separators, no shell
// metacharacters, no absolute path. The .exe suffix (case-insensitive)
// is stripped so the allowlist comparison is portable across Windows
// and POSIX. Returns "" for any input that contains a path separator,
// a shell metacharacter, or that filepath.IsAbs would consider absolute.
//
// This deliberately rejects "C:\Users\Public\az.exe", "/usr/bin/az",
// "./az", and "..\az.exe" — see [IsAllowedCommand] for the security
// rationale.
func commandBasename(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	// Disallow shell metacharacters anywhere. exec.CommandContext does
	// not invoke a shell, but an allowlist entry like "az; rm -rf /" is
	// a programming bug we want to surface — not silently match.
	for _, r := range name {
		switch r {
		case ';', '&', '|', '`', '$', '<', '>', '"', '\'', '\n', '\r', '\t', 0:
			return ""
		}
	}
	// Reject any path-containing input. The MCP contract is "pass a bare
	// command name; the server resolves it via PATH." Pre-resolved paths
	// would bypass our PATH-based binary selection and let an attacker
	// route the call through a planted binary that happens to share a
	// basename with an allowlisted command.
	if strings.ContainsAny(name, `/\`) {
		return ""
	}
	if filepath.IsAbs(name) {
		return ""
	}
	// Strip trailing .exe (case-insensitive) so "az.exe" matches "az".
	if strings.HasSuffix(strings.ToLower(name), ".exe") {
		name = name[:len(name)-4]
	}
	if name == "" {
		return ""
	}
	return name
}
