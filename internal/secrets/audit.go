package secrets

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

// AuditResult is the small enum of outcomes recorded in the secrets audit log.
type AuditResult string

const (
	AuditOK    AuditResult = "ok"
	AuditMiss  AuditResult = "miss"
	AuditError AuditResult = "error"
)

// AuditEntry is one append-only record written to <StateDir>/audit/secrets.log.
//
// The ref string is the original, unredacted reference (e.g.
// "op://Personal/GitHub Token/credential"). The reference is metadata in
// our threat model — it tells you where a secret lives, not what it is —
// so it is safe to log. The resolved value is NEVER logged.
type AuditEntry struct {
	Timestamp time.Time   `json:"ts"`
	Session   string      `json:"session"`
	Profile   string      `json:"profile,omitempty"`
	Resolver  string      `json:"resolver"`
	Ref       string      `json:"ref"`
	Result    AuditResult `json:"result"`
	Error     string      `json:"error,omitempty"`
}

// AuditOptions hooks for tests and callers that need to override defaults.
type AuditOptions struct {
	// Profile sets the optional profile name field on the entry.
	Profile string
	// SessionID overrides the session ID resolution (tests, non-default agents).
	SessionID string
	// Error is the human-readable error string, recorded when Result is AuditError.
	Error string
}

// auditConfig is the package-level audit logger configuration. Tests
// swap Dir to a temp directory via [SetAuditDir]; the default uses
// <StateDir>/audit.
type auditConfig struct {
	mu      sync.Mutex
	dir     string // empty → derive from core.StateDir on first use
	maxSize int64  // bytes per file before rotation
	keep    int    // number of historical files kept (.1 .. .keep)
}

var auditCfg = &auditConfig{
	maxSize: 5 * 1024 * 1024,
	keep:    3,
}

// SetAuditDir overrides the directory where secrets.log lives. Tests use
// this; production code should leave it unset so the default
// (<StateDir>/audit) applies.
func SetAuditDir(dir string) {
	auditCfg.mu.Lock()
	defer auditCfg.mu.Unlock()
	auditCfg.dir = dir
}

// SetAuditRotation configures rotation thresholds. maxSize ≤ 0 keeps the
// current setting; keep < 0 keeps the current setting.
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

// auditPath returns <auditDir>/secrets.log.
func auditPath() (string, error) {
	dir, err := auditDirLocked()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "secrets.log"), nil
}

// LogResolve records the outcome of a Resolve attempt. Backends call this
// from inside Resolve (success or failure); Describe MUST NOT call it.
//
// Defence in depth: this function refuses to embed any field other than
// the documented metadata above. There is no escape hatch to log a
// resolved value.
func LogResolve(resolver, ref string, result AuditResult, opts AuditOptions) {
	auditCfg.mu.Lock()
	defer auditCfg.mu.Unlock()

	entry := AuditEntry{
		Timestamp: time.Now().UTC(),
		Resolver:  resolver,
		Ref:       ref,
		Result:    result,
		Profile:   opts.Profile,
	}
	if opts.SessionID != "" {
		entry.Session = opts.SessionID
	} else {
		id, _ := state.SessionID()
		entry.Session = id
	}
	if result == AuditError {
		entry.Error = opts.Error
	}

	if err := writeAuditEntryLocked(entry); err != nil {
		// Audit logging must never abort a resolve. We still tell stderr
		// so an operator running `pm` interactively can notice a broken
		// log directory; the secret itself is unaffected.
		fmt.Fprintf(os.Stderr, "secrets: audit log write failed: %v\n", err)
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

// rotateIfNeededLocked rotates secrets.log when it has reached maxSize.
// Rotation walks down: .keep → discarded, .(keep-1) → .keep, …, .1 → .2,
// secrets.log → .1. Best-effort: any individual rename failure is logged
// and ignored so the next append can still proceed.
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
		// No history kept: truncate by removing the file. Next write
		// recreates it.
		_ = os.Remove(path)
		return nil
	}
	// Drop the oldest file if present.
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
