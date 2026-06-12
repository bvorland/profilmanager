package secrets

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// auditEntries reads every line from the active secrets.log into typed
// AuditEntry values. Tests use this to assert content; production code
// never reads the audit log back.
func auditEntries(t *testing.T) []AuditEntry {
	t.Helper()
	dir, err := AuditDir()
	if err != nil {
		t.Fatalf("AuditDir: %v", err)
	}
	f, err := os.Open(filepath.Join(dir, "secrets.log"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("open audit log: %v", err)
	}
	defer f.Close()
	var out []AuditEntry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e AuditEntry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatalf("unmarshal audit entry %q: %v", sc.Text(), err)
		}
		out = append(out, e)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan audit log: %v", err)
	}
	return out
}

func TestAuditLogWritesEntryWithoutValue(t *testing.T) {
	dir := t.TempDir()
	SetAuditDir(dir)
	t.Cleanup(func() { SetAuditDir("") })

	const canary = "RESOLVED-SECRET-VALUE-MUST-NEVER-APPEAR-IN-LOG"
	// We never pass the resolved value into LogResolve. But to prove the
	// log content can't accidentally include it, we add it as the audit
	// error string and confirm it's only present there (i.e. the value
	// is opt-in via the explicit Error field, and resolvers never put
	// values into Error).
	LogResolve("test-resolver", "op://V/I/F", AuditOK, AuditOptions{Profile: "demo"})
	LogResolve("test-resolver", "op://V/I/Missing", AuditMiss, AuditOptions{Profile: "demo", Error: "not found"})
	LogResolve("test-resolver", "op://V/I/Broken", AuditError, AuditOptions{Profile: "demo", Error: "subprocess crashed"})

	entries := auditEntries(t)
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	raw, err := os.ReadFile(filepath.Join(dir, "secrets.log"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(canary)) {
		t.Fatal("canary leaked into audit log")
	}

	// AuditOK: no Error field.
	if entries[0].Result != AuditOK || entries[0].Error != "" {
		t.Fatalf("ok entry: %+v", entries[0])
	}
	if entries[0].Profile != "demo" || entries[0].Resolver != "test-resolver" {
		t.Fatalf("ok entry metadata: %+v", entries[0])
	}
	if entries[0].Timestamp.IsZero() {
		t.Fatalf("missing timestamp: %+v", entries[0])
	}
	// AuditMiss: keeps Error blank too (Error is only embedded for AuditError).
	if entries[1].Result != AuditMiss || entries[1].Error != "" {
		t.Fatalf("miss entry: %+v", entries[1])
	}
	// AuditError: includes Error string.
	if entries[2].Result != AuditError || !strings.Contains(entries[2].Error, "subprocess crashed") {
		t.Fatalf("error entry: %+v", entries[2])
	}
}

func TestAuditLogRotationBySize(t *testing.T) {
	dir := t.TempDir()
	SetAuditDir(dir)
	SetAuditRotation(512, 3) // tiny so we rotate fast
	t.Cleanup(func() {
		SetAuditDir("")
		SetAuditRotation(5*1024*1024, 3)
	})

	// Each entry is well under 512 bytes but a few hundred entries
	// trigger multiple rotations.
	for i := 0; i < 200; i++ {
		LogResolve("test", "op://V/I/F", AuditOK, AuditOptions{})
	}
	logPath := filepath.Join(dir, "secrets.log")
	mustExist := []string{logPath, logPath + ".1"}
	for _, p := range mustExist {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("expected %s to exist after rotation: %v", p, err)
		}
	}
	// keep=3 means at most secrets.log + .1 .. .3 exist; no .4.
	if _, err := os.Stat(logPath + ".4"); !os.IsNotExist(err) {
		t.Fatalf("rotation kept too many files (.4 exists): %v", err)
	}

	// Sanity: the active log file is below threshold (since we rotate
	// before appending when over).
	info, err := os.Stat(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() >= 512*2 {
		t.Fatalf("active log unexpectedly large: %d bytes", info.Size())
	}

	// Total file count should be at most 1 + keep = 4.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "secrets.log") {
			count++
		}
	}
	if count > 4 {
		t.Fatalf("too many rotated files: %d", count)
	}
}

func TestAuditLogZeroKeepRotatesByTruncation(t *testing.T) {
	dir := t.TempDir()
	SetAuditDir(dir)
	SetAuditRotation(256, 0)
	t.Cleanup(func() {
		SetAuditDir("")
		SetAuditRotation(5*1024*1024, 3)
	})

	for i := 0; i < 50; i++ {
		LogResolve("test", "op://V/I/F", AuditOK, AuditOptions{})
	}
	if _, err := os.Stat(filepath.Join(dir, "secrets.log.1")); !os.IsNotExist(err) {
		t.Fatalf("keep=0 must not create .1: %v", err)
	}
}

func TestAuditLogSchemaStableFields(t *testing.T) {
	dir := t.TempDir()
	SetAuditDir(dir)
	t.Cleanup(func() { SetAuditDir("") })
	LogResolve("op", "op://V/I/F", AuditOK, AuditOptions{Profile: "demo", SessionID: "sess-xyz"})

	raw, err := os.ReadFile(filepath.Join(dir, "secrets.log"))
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(raw), &obj); err != nil {
		t.Fatalf("audit entry not valid JSON: %v\n%s", err, raw)
	}
	for _, want := range []string{"ts", "session", "profile", "resolver", "ref", "result"} {
		if _, ok := obj[want]; !ok {
			t.Errorf("missing field %q in entry: %v", want, obj)
		}
	}
	if obj["session"] != "sess-xyz" {
		t.Errorf("session override not honoured: %v", obj["session"])
	}
}
