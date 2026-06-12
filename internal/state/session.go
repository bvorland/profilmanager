// Package state owns per-session and per-operator pm state on disk:
// active-profile-per-session, last-used profile, and the file-locking
// discipline that keeps concurrent CLI invocations from corrupting each
// other.
//
// Architectural rule: the "active profile" file is metadata only — it
// tells `pm exec` and MCP tools which profile to apply for this session.
// It does NOT mutate the calling shell.
package state

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gofrs/flock"

	"github.com/bvorland/profilmanager/internal/core"
)

// SessionSource describes how SessionID resolved an ID. `pm doctor` uses
// it to warn when we fell back to PPID.
const (
	SourcePMSession      = "pm-session"
	SourceCopilotSession = "copilot-session"
	SourceWTSession      = "wt-session"
	SourcePPIDFallback   = "ppid-fallback"
)

// SessionID returns the current session identifier and the source that
// produced it. Resolution order, first non-empty wins:
//
//  1. PM_SESSION_ID                (canonical, operator-set)
//  2. COPILOT_AGENT_SESSION_ID     (Copilot CLI)
//  3. WT_SESSION                   (Windows Terminal)
//  4. PPID fallback                (last resort)
func SessionID() (id string, source string) {
	if v := strings.TrimSpace(os.Getenv("PM_SESSION_ID")); v != "" {
		return v, SourcePMSession
	}
	if v := strings.TrimSpace(os.Getenv("COPILOT_AGENT_SESSION_ID")); v != "" {
		return v, SourceCopilotSession
	}
	if v := strings.TrimSpace(os.Getenv("WT_SESSION")); v != "" {
		return v, SourceWTSession
	}
	return "ppid-" + strconv.Itoa(os.Getppid()), SourcePPIDFallback
}

// sessionsDir is <StateDir>/sessions, created on demand.
func sessionsDir() (string, error) {
	root, err := core.StateDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create sessions dir: %w", err)
	}
	return dir, nil
}

// ActiveProfileFile returns the absolute path of the active-profile file
// for the current session. It does NOT create the file — only its parent
// directory.
func ActiveProfileFile() (string, error) {
	dir, err := sessionsDir()
	if err != nil {
		return "", err
	}
	id, _ := SessionID()
	return filepath.Join(dir, sanitizeID(id)+".profile"), nil
}

// GetActiveProfile reads the active profile name for the current session.
// Returns ("", source, nil) if no active profile is set.
func GetActiveProfile() (name string, source string, err error) {
	_, source = SessionID()
	path, err := ActiveProfileFile()
	if err != nil {
		return "", source, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", source, nil
		}
		return "", source, fmt.Errorf("read active profile: %w", err)
	}
	return strings.TrimSpace(string(data)), source, nil
}

// SetActiveProfile records name as the active profile for the current
// session. Atomic write under an advisory lock to serialize concurrent
// callers.
func SetActiveProfile(name string) error {
	if err := core.ValidateName(name); err != nil {
		return err
	}
	path, err := ActiveProfileFile()
	if err != nil {
		return err
	}
	return lockedAtomicWrite(path, []byte(name+"\n"))
}

// ClearActiveProfile removes the active-profile marker for the current
// session. No error if the file is already absent.
func ClearActiveProfile() error {
	path, err := ActiveProfileFile()
	if err != nil {
		return err
	}
	lock := flock.New(path + ".lock")
	if err := lock.Lock(); err != nil {
		return fmt.Errorf("lock active profile: %w", err)
	}
	defer func() { _ = lock.Unlock() }()
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove active profile: %w", err)
	}
	return nil
}

// LastProfileFile returns the operator-global "last profile used" path.
// This is shared across sessions on purpose: it powers `pm switch -` and
// similar UX, where humans want "go back to the last one I picked".
func LastProfileFile() (string, error) {
	root, err := core.StateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "last-profile"), nil
}

// GetLastProfile returns the most recently set profile, or "" if none.
func GetLastProfile() (string, error) {
	path, err := LastProfileFile()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("read last profile: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

// SetLastProfile records name as the operator's last-used profile.
func SetLastProfile(name string) error {
	if err := core.ValidateName(name); err != nil {
		return err
	}
	path, err := LastProfileFile()
	if err != nil {
		return err
	}
	return lockedAtomicWrite(path, []byte(name+"\n"))
}

// lockedAtomicWrite combines a flock advisory lock (on <path>.lock) with
// the standard write-temp + rename dance, so concurrent processes
// serialize cleanly without ever leaving a partial file on disk.
func lockedAtomicWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create parent dir: %w", err)
	}
	lock := flock.New(path + ".lock")
	if err := lock.Lock(); err != nil {
		return fmt.Errorf("lock %s: %w", path, err)
	}
	defer func() { _ = lock.Unlock() }()
	return atomicWrite(path, data, 0o644)
}

func atomicWrite(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".pm-state-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("sync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		cleanup()
		return fmt.Errorf("chmod temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("rename %s -> %s: %w", tmpName, path, err)
	}
	return nil
}

// sanitizeID strips anything outside the safe filename set from a session
// id. Copilot/WT IDs are GUIDs/hex so this is normally a no-op; the
// PPID-fallback id ("ppid-1234") is also safe. We still sanitize because
// PM_SESSION_ID is operator-supplied.
func sanitizeID(id string) string {
	if id == "" {
		return "unknown"
	}
	var b strings.Builder
	b.Grow(len(id))
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	if b.Len() == 0 {
		return "unknown"
	}
	return b.String()
}
