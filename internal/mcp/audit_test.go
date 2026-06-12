package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// withAuditTempDir routes mcp.log into a per-test tmpdir and restores
// the previous dir afterwards. Tests use this to assert on the file
// contents without touching the operator's real StateDir.
func withAuditTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	prev := auditCfg.dir // peek; we hold no lock because tests are sequential
	SetAuditDir(dir)
	t.Cleanup(func() { SetAuditDir(prev) })
	return dir
}

// readAuditLines parses every JSON-lines record from mcp.log and
// returns them as AuditEntry slices. Fails the test on parse error
// (we wrote the file ourselves; malformed lines indicate a bug, not
// operator data).
func readAuditLines(t *testing.T, dir string) []AuditEntry {
	t.Helper()
	path := filepath.Join(dir, "mcp.log")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read mcp.log: %v", err)
	}
	var entries []AuditEntry
	for i, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if line == "" {
			continue
		}
		var e AuditEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("parse mcp.log line %d (%q): %v", i+1, line, err)
		}
		entries = append(entries, e)
	}
	return entries
}

// TestLogEntry_WritesNewlineJSON is the smoke test for the audit log:
// one call to logEntry produces one well-formed JSON line on disk.
func TestLogEntry_WritesNewlineJSON(t *testing.T) {
	dir := withAuditTempDir(t)
	logEntry(AuditEntry{
		Tool:   "resolve_secret_ref",
		Ref:    "op://Vault/Item/field",
		Result: "ok",
	})
	entries := readAuditLines(t, dir)
	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Tool != "resolve_secret_ref" || e.Ref != "op://Vault/Item/field" || e.Result != "ok" {
		t.Errorf("entry contents wrong: %+v", e)
	}
	if e.Timestamp.IsZero() {
		t.Errorf("Timestamp not populated automatically")
	}
}

// TestLogEntry_NoStdoutBeyondPreview encodes the iron rule that the
// audit log does NOT contain full stdout — only the bounded preview
// the caller computed. We feed a long, scary-looking string into the
// preview field; the entry should round-trip it as-is (the caller is
// responsible for redaction + truncation upstream).
func TestLogEntry_NoStdoutBeyondPreview(t *testing.T) {
	dir := withAuditTempDir(t)
	preview := "first 256 bytes — <REDACTED> — etc"
	logEntry(AuditEntry{
		Tool:          "exec_with_profile",
		Command:       "az",
		Result:        "ok",
		ExitCode:      0,
		OutputPreview: preview,
	})
	entries := readAuditLines(t, dir)
	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(entries))
	}
	if entries[0].OutputPreview != preview {
		t.Errorf("preview field changed: got %q", entries[0].OutputPreview)
	}
}

// TestLogEntry_RotationKeepsHistory verifies size-based rotation:
// after pushing past maxSize, mcp.log is renamed to .1 and a fresh
// file starts.
func TestLogEntry_RotationKeepsHistory(t *testing.T) {
	dir := withAuditTempDir(t)
	// Force a tiny rotation threshold so two entries push us past it.
	SetAuditRotation(64, 2)
	t.Cleanup(func() { SetAuditRotation(5*1024*1024, 3) })

	for i := 0; i < 6; i++ {
		logEntry(AuditEntry{
			Tool:   "resolve_secret_ref",
			Ref:    strings.Repeat("x", 50),
			Result: "ok",
		})
	}
	// We expect at least one .1 file to exist after enough rotations.
	if _, err := os.Stat(filepath.Join(dir, "mcp.log.1")); err != nil {
		t.Fatalf("rotation did not produce mcp.log.1: %v", err)
	}
}

// TestLogEntry_SessionFallback ensures we populate a session ID even
// when the caller doesn't supply one — the audit log without a session
// is much less useful for forensic queries.
func TestLogEntry_SessionFallback(t *testing.T) {
	dir := withAuditTempDir(t)
	t.Setenv("PM_SESSION_ID", "test-session-42")
	logEntry(AuditEntry{Tool: "switch_profile", Result: "ok", Timestamp: time.Now()})
	entries := readAuditLines(t, dir)
	if len(entries) != 1 || entries[0].Session != "test-session-42" {
		t.Fatalf("session not picked up from env: %+v", entries)
	}
}
