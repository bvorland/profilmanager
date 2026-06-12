package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// TestDoctorJSONShape locks the JSON envelope and the names of the
// built-in checks. Values vary (paths, tool availability), but the keys
// are the API contract callers will assert on later.
func TestDoctorJSONShape(t *testing.T) {
	testEnv(t)

	stdout, _, err := runCLI(t, "doctor", "--json")
	// doctor exits non-zero if any check fails; that's allowed for this
	// shape test as long as JSON is parseable.
	if err != nil && CodeFor(err) != ExitError {
		t.Fatalf("unexpected error code: err=%v code=%d", err, CodeFor(err))
	}

	var doc struct {
		Checks []CheckResult `json:"checks"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("parse: %v stdout=%s", err, stdout)
	}
	if len(doc.Checks) < 5 {
		t.Fatalf("expected at least 5 checks, got %d", len(doc.Checks))
	}

	got := map[string]CheckResult{}
	for _, c := range doc.Checks {
		got[c.Name] = c
	}

	wantNames := []string{
		"profiles-dir-exists",
		"state-dir-writable",
		"session-id-source",
		"agent-context-has-profile",
		"shell-init-wrapper-loaded",
		"profiles-not-in-git",
		"mcp-registered",
	}
	for _, name := range wantNames {
		r, ok := got[name]
		if !ok {
			t.Errorf("missing check %q", name)
			continue
		}
		switch r.Status {
		case StatusOK, StatusWarn, StatusFail:
		default:
			t.Errorf("check %q: unexpected status %q", name, r.Status)
		}
		if r.Name != name {
			t.Errorf("check %q: name field mismatch %q", name, r.Name)
		}
		if r.Message == "" {
			t.Errorf("check %q: empty message", name)
		}
	}

	// Tool checks all use the "tool-available:" prefix.
	var toolChecks []string
	for _, c := range doc.Checks {
		if strings.HasPrefix(c.Name, "tool-available:") {
			toolChecks = append(toolChecks, c.Name)
		}
	}
	sort.Strings(toolChecks)
	wantTools := []string{
		"tool-available:az",
		"tool-available:azd",
		"tool-available:git",
		"tool-available:gh",
		"tool-available:kubectl",
		"tool-available:pwsh",
	}
	sort.Strings(wantTools)
	if !sliceEq(toolChecks, wantTools) {
		t.Errorf("tool-available checks mismatch:\nwant: %v\ngot:  %v", wantTools, toolChecks)
	}
}

// TestDoctorSessionIDFallbackWarns asserts the WARN status when no
// session env vars are set.
func TestDoctorSessionIDFallbackWarns(t *testing.T) {
	testEnv(t)
	t.Setenv("PM_SESSION_ID", "") // force PPID fallback

	stdout, _, err := runCLI(t, "doctor", "--json")
	if err != nil && CodeFor(err) != ExitError {
		t.Fatalf("err=%v", err)
	}
	var doc struct {
		Checks []CheckResult `json:"checks"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, c := range doc.Checks {
		if c.Name == "session-id-source" {
			if c.Status != StatusWarn {
				t.Errorf("expected warn, got %q", c.Status)
			}
			if c.Fix == "" {
				t.Errorf("expected non-empty fix")
			}
			return
		}
	}
	t.Fatal("session-id-source check not found")
}

// TestDoctorSessionIDOKWhenPMSession asserts OK when PM_SESSION_ID is
// set explicitly.
func TestDoctorSessionIDOKWhenPMSession(t *testing.T) {
	testEnv(t)
	t.Setenv("PM_SESSION_ID", "fixed-test-id")

	stdout, _, err := runCLI(t, "doctor", "--json")
	if err != nil && CodeFor(err) != ExitError {
		t.Fatalf("err=%v", err)
	}
	var doc struct {
		Checks []CheckResult `json:"checks"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, c := range doc.Checks {
		if c.Name == "session-id-source" {
			if c.Status != StatusOK {
				t.Errorf("expected ok, got %q", c.Status)
			}
			if !strings.Contains(c.Message, "fixed-test-id") {
				t.Errorf("message should mention id: %q", c.Message)
			}
			return
		}
	}
	t.Fatal("session-id-source check not found")
}

func TestDoctorAgentContextHasProfile(t *testing.T) {
	clearAgentEnv := func(t *testing.T) {
		t.Helper()
		for _, key := range []string{
			"PM_SESSION_ID",
			"COPILOT_SESSION_ID",
			"COPILOT_CLI_SESSION_ID",
			"CLAUDE_SESSION_ID",
			"AIDER_SESSION_ID",
			"PM_ACTIVE_PROFILE",
		} {
			t.Setenv(key, "")
		}
	}

	t.Run("not in agent context", func(t *testing.T) {
		clearAgentEnv(t)

		got := checkAgentContextHasProfile()
		if got.Status != StatusOK {
			t.Fatalf("status = %q, want %q", got.Status, StatusOK)
		}
		if !strings.Contains(got.Message, "not in agent context") {
			t.Fatalf("message should mention skipped agent context, got %q", got.Message)
		}
	})

	t.Run("pm session without active profile warns", func(t *testing.T) {
		clearAgentEnv(t)
		t.Setenv("PM_SESSION_ID", "pm-session")

		got := checkAgentContextHasProfile()
		if got.Status != StatusWarn {
			t.Fatalf("status = %q, want %q", got.Status, StatusWarn)
		}
		if !strings.Contains(got.Message, "Inside an AI agent") {
			t.Fatalf("message should mention AI agent, got %q", got.Message)
		}
	})

	t.Run("pm session with active profile ok", func(t *testing.T) {
		clearAgentEnv(t)
		t.Setenv("PM_SESSION_ID", "pm-session")
		t.Setenv("PM_ACTIVE_PROFILE", "Contoso.Foo")

		got := checkAgentContextHasProfile()
		if got.Status != StatusOK {
			t.Fatalf("status = %q, want %q", got.Status, StatusOK)
		}
		if !strings.Contains(got.Message, "Contoso.Foo") {
			t.Fatalf("message should mention active profile, got %q", got.Message)
		}
	})

	t.Run("copilot session var is reported", func(t *testing.T) {
		clearAgentEnv(t)
		t.Setenv("COPILOT_SESSION_ID", "copilot-session")

		got := checkAgentContextHasProfile()
		if got.Status != StatusWarn {
			t.Fatalf("status = %q, want %q", got.Status, StatusWarn)
		}
		if !strings.Contains(got.Message, "COPILOT_SESSION_ID") {
			t.Fatalf("message should mention detected var, got %q", got.Message)
		}
	})

	t.Run("first listed agent var wins", func(t *testing.T) {
		clearAgentEnv(t)
		t.Setenv("PM_SESSION_ID", "pm-session")
		t.Setenv("COPILOT_SESSION_ID", "copilot-session")
		t.Setenv("CLAUDE_SESSION_ID", "claude-session")

		got := checkAgentContextHasProfile()
		if got.Status != StatusWarn {
			t.Fatalf("status = %q, want %q", got.Status, StatusWarn)
		}
		if !strings.Contains(got.Message, "PM_SESSION_ID") {
			t.Fatalf("message should mention first detected var, got %q", got.Message)
		}
		if strings.Contains(got.Message, "COPILOT_SESSION_ID") {
			t.Fatalf("message should not mention later var, got %q", got.Message)
		}
	})
}

func TestDoctor_ShellInitWrapper_Pwsh_NotLoaded_Warns(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("shell-init wrapper doctor check is Windows/pwsh-only today")
	}
	t.Setenv("SHELL", "pwsh")
	t.Setenv("PM_SHELL_INIT_LOADED", "")

	got := checkShellInitWrapperLoaded()
	if got.Status != StatusWarn {
		t.Fatalf("status = %q, want %q (message=%q)", got.Status, StatusWarn, got.Message)
	}
	for _, want := range []string{
		"Add to your $PROFILE: pm shell-init pwsh | Out-String | Invoke-Expression",
		"auto-apply",
		"future tool shims",
	} {
		if !strings.Contains(got.Message, want) {
			t.Fatalf("message missing %q: %q", want, got.Message)
		}
	}
}

func TestDoctor_ShellInitWrapper_Pwsh_Loaded_OK(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("shell-init wrapper doctor check is Windows/pwsh-only today")
	}
	t.Setenv("SHELL", "pwsh")
	t.Setenv("PM_SHELL_INIT_LOADED", "1")

	got := checkShellInitWrapperLoaded()
	if got.Status != StatusOK {
		t.Fatalf("status = %q, want %q (message=%q)", got.Status, StatusOK, got.Message)
	}
	if !strings.Contains(got.Message, "loaded") {
		t.Fatalf("message should mention wrapper is loaded, got %q", got.Message)
	}
}

func TestDoctor_ShellInitWrapper_NonPwsh_Skipped(t *testing.T) {
	t.Setenv("SHELL", "bash")
	t.Setenv("PM_SHELL_INIT_LOADED", "")

	got := checkShellInitWrapperLoaded()
	if got.Status != StatusOK {
		t.Fatalf("status = %q, want %q (message=%q)", got.Status, StatusOK, got.Message)
	}
	if !strings.Contains(got.Message, "skipped") {
		t.Fatalf("message should mention skipped, got %q", got.Message)
	}
}

// TestDoctorMCPDetectsPresent puts a profilmanager entry under CWD and
// asserts the check flips to OK.
func TestDoctorMCPDetectsPresent(t *testing.T) {
	testEnv(t)
	// chdir to a fresh dir so we can place .copilot/mcp-config.json there.
	wd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(wd) })
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".copilot"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"mcpServers": {"profilmanager": {"command": "pm", "args": ["mcp"]}}}`
	if err := os.WriteFile(filepath.Join(root, ".copilot", "mcp-config.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}

	stdout, _, err := runCLI(t, "doctor", "--json")
	if err != nil && CodeFor(err) != ExitError {
		t.Fatalf("err=%v", err)
	}
	var doc struct {
		Checks []CheckResult `json:"checks"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, c := range doc.Checks {
		if c.Name == "mcp-registered" {
			if c.Status != StatusOK {
				t.Errorf("expected ok, got %q (message=%q)", c.Status, c.Message)
			}
			return
		}
	}
	t.Fatal("mcp-registered check not found")
}

// TestRegisterCheck adds and replaces an external check.
func TestRegisterCheck(t *testing.T) {
	// Save and restore the external list so we don't pollute other tests.
	saved := externalChecks
	t.Cleanup(func() { externalChecks = saved })
	externalChecks = nil

	RegisterCheck("dummy", func() CheckResult {
		return CheckResult{Name: "dummy", Status: StatusOK, Message: "v1"}
	})
	RegisterCheck("dummy", func() CheckResult {
		return CheckResult{Name: "dummy", Status: StatusWarn, Message: "v2"}
	})
	if len(externalChecks) != 1 {
		t.Fatalf("expected dedup-by-name to keep 1 entry, got %d", len(externalChecks))
	}
	r := externalChecks[0].fn()
	if r.Status != StatusWarn || r.Message != "v2" {
		t.Errorf("RegisterCheck did not replace: %+v", r)
	}
}

func sliceEq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
