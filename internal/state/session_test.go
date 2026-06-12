package state

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// stubStateDirs points core.StateDir/ProfilesDir at a per-test tmpdir by
// setting the relevant OS env vars and clearing session-id env vars.
func stubStateDirs(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	switch runtime.GOOS {
	case "windows":
		t.Setenv("APPDATA", filepath.Join(tmp, "appdata"))
		t.Setenv("LOCALAPPDATA", filepath.Join(tmp, "localappdata"))
	default:
		t.Setenv("HOME", filepath.Join(tmp, "home"))
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "xdgcfg"))
		t.Setenv("XDG_STATE_HOME", filepath.Join(tmp, "xdgstate"))
	}
	t.Setenv("PM_SESSION_ID", "")
	t.Setenv("COPILOT_AGENT_SESSION_ID", "")
	t.Setenv("WT_SESSION", "")
	return tmp
}

func TestSessionIDResolution(t *testing.T) {
	stubStateDirs(t)

	t.Setenv("PM_SESSION_ID", "pm-abc")
	t.Setenv("COPILOT_AGENT_SESSION_ID", "copilot-xyz")
	t.Setenv("WT_SESSION", "wt-guid")
	id, src := SessionID()
	if id != "pm-abc" || src != SourcePMSession {
		t.Errorf("PM_SESSION_ID precedence broken: id=%q src=%q", id, src)
	}

	t.Setenv("PM_SESSION_ID", "")
	id, src = SessionID()
	if id != "copilot-xyz" || src != SourceCopilotSession {
		t.Errorf("COPILOT_AGENT_SESSION_ID precedence broken: id=%q src=%q", id, src)
	}

	t.Setenv("COPILOT_AGENT_SESSION_ID", "")
	id, src = SessionID()
	if id != "wt-guid" || src != SourceWTSession {
		t.Errorf("WT_SESSION precedence broken: id=%q src=%q", id, src)
	}

	t.Setenv("WT_SESSION", "")
	id, src = SessionID()
	if src != SourcePPIDFallback || !strings.HasPrefix(id, "ppid-") {
		t.Errorf("PPID fallback broken: id=%q src=%q", id, src)
	}
}

func TestActiveProfileRoundTrip(t *testing.T) {
	stubStateDirs(t)
	t.Setenv("PM_SESSION_ID", "test-session-1")

	got, src, err := GetActiveProfile()
	if err != nil {
		t.Fatalf("Get(empty): %v", err)
	}
	if got != "" {
		t.Errorf("expected empty active profile, got %q", got)
	}
	if src != SourcePMSession {
		t.Errorf("source: %q", src)
	}

	if err := SetActiveProfile("Contoso.MainDev"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, src, err = GetActiveProfile()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "Contoso.MainDev" {
		t.Errorf("active: %q", got)
	}
	if src != SourcePMSession {
		t.Errorf("source: %q", src)
	}

	// File path should encode the sanitized session id.
	path, err := ActiveProfileFile()
	if err != nil {
		t.Fatalf("ActiveProfileFile: %v", err)
	}
	if !strings.HasSuffix(path, "test-session-1.profile") {
		t.Errorf("unexpected file path: %q", path)
	}

	// Sessions in different IDs do not see each other.
	t.Setenv("PM_SESSION_ID", "other-session")
	got2, _, err := GetActiveProfile()
	if err != nil {
		t.Fatalf("Get(other): %v", err)
	}
	if got2 != "" {
		t.Errorf("expected other session to be empty, got %q", got2)
	}

	// Clear is idempotent.
	if err := ClearActiveProfile(); err != nil {
		t.Fatalf("Clear (empty): %v", err)
	}
	t.Setenv("PM_SESSION_ID", "test-session-1")
	if err := ClearActiveProfile(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	got, _, _ = GetActiveProfile()
	if got != "" {
		t.Errorf("after clear: %q", got)
	}
}

func TestSetActiveProfileRejectsBadName(t *testing.T) {
	stubStateDirs(t)
	t.Setenv("PM_SESSION_ID", "x")
	if err := SetActiveProfile("bad name"); err == nil {
		t.Error("expected validation error for bad profile name")
	}
}

func TestLastProfileRoundTrip(t *testing.T) {
	stubStateDirs(t)

	got, err := GetLastProfile()
	if err != nil {
		t.Fatalf("Get(empty): %v", err)
	}
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}

	if err := SetLastProfile("Acme-Prod"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err = GetLastProfile()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "Acme-Prod" {
		t.Errorf("got %q", got)
	}

	// Survives session switch (operator-global).
	t.Setenv("PM_SESSION_ID", "another")
	got, err = GetLastProfile()
	if err != nil {
		t.Fatalf("Get(other session): %v", err)
	}
	if got != "Acme-Prod" {
		t.Errorf("last-profile should be session-independent, got %q", got)
	}
}

func TestConcurrentSetActiveProfile(t *testing.T) {
	stubStateDirs(t)
	t.Setenv("PM_SESSION_ID", "concurrent")

	var wg sync.WaitGroup
	const N = 16
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			_ = SetActiveProfile("p-1")
		}()
	}
	wg.Wait()

	got, _, err := GetActiveProfile()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "p-1" {
		t.Errorf("concurrent writes produced %q", got)
	}

	path, _ := ActiveProfileFile()
	dir := filepath.Dir(path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".pm-state-") {
			t.Errorf("leftover temp file after concurrent writes: %q", e.Name())
		}
	}
}

func TestSanitizeID(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"":              "unknown",
		"abc-123":       "abc-123",
		"foo/bar":       "foo_bar",
		"a b\tc":        "a_b_c",
		"X.Y_Z-9":       "X.Y_Z-9",
		"../../escape":  ".._.._escape",
		"path\\sep":     "path_sep",
	}
	for in, want := range cases {
		if got := sanitizeID(in); got != want {
			t.Errorf("sanitizeID(%q) = %q want %q", in, got, want)
		}
	}
}
