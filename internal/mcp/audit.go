package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/bvorland/profilmanager/internal/core"
	"github.com/bvorland/profilmanager/internal/state"
)

// AuditEntry is one append-only record written to <StateDir>/audit/mcp.log.
//
// Schema invariants (iron rule, enforced at write time):
//
//   - NO secret values. Args have been redacted in [Exec] before they
//     reach this struct; the resolver never sees a value here.
//   - NO stdout/stderr beyond OutputPreview (≤ 256 chars, redacted).
//   - The ref field on resolve_secret_ref entries is metadata only
//     (where the secret lives, not what it is). Same threat model as
//     internal/secrets/audit.go.
//
// Adding a field is non-breaking; renaming or removing is.
type AuditEntry struct {
	Timestamp time.Time `json:"ts"`
	Session   string    `json:"session"`
	Profile   string    `json:"profile,omitempty"`
	Tool      string    `json:"tool"`
	Command   string    `json:"command,omitempty"`
	Args      []string  `json:"args,omitempty"`
	// Ref is populated for resolve_secret_ref entries.
	Ref string `json:"ref,omitempty"`
	// Result is "ok", "miss", "error", "denied" (allowlist violation).
	Result string `json:"result"`
	// ExitCode is populated for exec_with_profile entries (0 if absent).
	ExitCode int `json:"exit_code,omitempty"`
	// DurationMs is populated for exec_with_profile entries.
	DurationMs int64  `json:"duration_ms,omitempty"`
	Error      string `json:"error,omitempty"`
	// OutputPreview is the first 256 bytes of redacted stdout, for
	// post-hoc debugging. Capped to keep the log small and to avoid
	// inadvertently embedding even short large blobs.
	OutputPreview string `json:"output_preview,omitempty"`
}

// auditConfig mirrors the secrets package: a package-level mutex around
// a config struct, swappable for tests. Kept separate from
// internal/secrets so the two audit logs (secrets.log, mcp.log) can
// evolve independently without coupling.
type auditConfig struct {
	mu      sync.Mutex
	dir     string // empty → derive from core.StateDir on first use
	maxSize int64  // bytes per file before rotation
	keep    int    // number of historical files kept
}

var auditCfg = &auditConfig{
	maxSize: 5 * 1024 * 1024, // 5 MiB — matches secrets audit
	keep:    3,
}

// SetAuditDir overrides the directory where mcp.log lives. Tests use
// this; production code leaves it unset so the default
// (<StateDir>/audit) applies.
func SetAuditDir(dir string) {
	auditCfg.mu.Lock()
	defer auditCfg.mu.Unlock()
	auditCfg.dir = dir
}

// SetAuditRotation configures rotation thresholds. maxSize ≤ 0 keeps
// the current setting; keep < 0 keeps the current setting.
func SetAuditRotation(maxSize int64, keep int) {
	auditCfg.mu.Lock()
	defer auditCfg.mu.Unlock()
	if maxSize > 0 {
		auditCfg.maxSize = maxSize
	}
	if keep >= 0 {
		auditCfg.keep = keep
	}
}

// AuditDir returns the resolved audit directory, creating it on first use.
func AuditDir() (string, error) {
	auditCfg.mu.Lock()
	defer auditCfg.mu.Unlock()
	return auditDirLocked()
}

func auditDirLocked() (string, error) {
	if auditCfg.dir != "" {
		if err := os.MkdirAll(auditCfg.dir, 0o755); err != nil {
			return "", fmt.Errorf("create audit dir: %w", err)
		}
		return auditCfg.dir, nil
	}
	root, err := core.StateDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, "audit")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create audit dir: %w", err)
	}
	return dir, nil
}

func auditPath() (string, error) {
	dir, err := auditDirLocked()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "mcp.log"), nil
}

// logEntry writes entry to mcp.log under the package mutex. Best-effort
// — a write failure is surfaced on stderr (so an interactive operator
// sees broken plumbing) but never aborts the tool call. Caller MUST
// have already redacted any secret-looking values from Args / OutputPreview.
func logEntry(entry AuditEntry) {
	auditCfg.mu.Lock()
	defer auditCfg.mu.Unlock()

	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	}
	if entry.Session == "" {
		id, _ := state.SessionID()
		entry.Session = id
	}

	if err := writeAuditEntryLocked(entry); err != nil {
		fmt.Fprintf(os.Stderr, "mcp: audit log write failed: %v\n", err)
	}
}

func writeAuditEntryLocked(entry AuditEntry) error {
	path, err := auditPath()
	if err != nil {
		return err
	}
	if err := rotateIfNeededLocked(path); err != nil {
		return err
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal audit entry: %w", err)
	}
	line = append(line, '\n')

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open audit log: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(line); err != nil {
		return fmt.Errorf("write audit entry: %w", err)
	}
	return nil
}

// rotateIfNeededLocked rotates mcp.log when it has reached maxSize.
// Same algorithm as internal/secrets/audit.go: drop the oldest, shift
// every numbered file down by one, rename the live log to .1.
func rotateIfNeededLocked(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("stat audit log: %w", err)
	}
	if info.Size() < auditCfg.maxSize {
		return nil
	}
	keep := auditCfg.keep
	if keep <= 0 {
		_ = os.Remove(path)
		return nil
	}
	oldest := path + "." + strconv.Itoa(keep)
	_ = os.Remove(oldest)
	for i := keep - 1; i >= 1; i-- {
		from := path + "." + strconv.Itoa(i)
		to := path + "." + strconv.Itoa(i+1)
		if _, err := os.Stat(from); err == nil {
			_ = os.Rename(from, to)
		}
	}
	if err := os.Rename(path, path+".1"); err != nil {
		return fmt.Errorf("rotate audit log: %w", err)
	}
	return nil
}
