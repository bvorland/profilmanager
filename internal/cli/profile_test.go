package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/bvorland/profilmanager/internal/core"
)

// testEnv stubs OS env vars so core.ProfilesDir / core.StateDir / state
// resolution all land in a fresh tmp dir, and disables color so output
// is golden-friendly. Mirrors isolateHome in import_mj_test.go plus a
// session-id reset and NO_COLOR.
func testEnv(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	switch runtime.GOOS {
	case "windows":
		t.Setenv("USERPROFILE", tmp)
		t.Setenv("APPDATA", filepath.Join(tmp, "appdata"))
		t.Setenv("LOCALAPPDATA", filepath.Join(tmp, "localappdata"))
	default:
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "xdgcfg"))
		t.Setenv("XDG_STATE_HOME", filepath.Join(tmp, "xdgstate"))
	}
	t.Setenv("PM_SESSION_ID", "test-session-"+t.Name())
	t.Setenv("COPILOT_AGENT_SESSION_ID", "")
	t.Setenv("WT_SESSION", "")
	t.Setenv("NO_COLOR", "1")
	return tmp
}

// runCLI builds a fresh root command, points stdout/stderr at buffers,
// invokes with args, and returns (stdout, stderr, err). Using a fresh
// root per call keeps cobra flag state clean between cases.
func runCLI(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	root := newRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetIn(strings.NewReader(""))
	root.SetArgs(args)
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

// runCLIWithStdin is like runCLI but supplies stdin contents (used by
// `pm profile rm` confirmation prompt tests).
func runCLIWithStdin(t *testing.T, stdin string, args ...string) (string, string, error) {
	t.Helper()
	root := newRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetIn(strings.NewReader(stdin))
	root.SetArgs(args)
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

// silenceWhoami ensures `pm whoami` doesn't probe real providers during
// tests. whoami is tolerant of missing tools, but we still
// don't want a hung gh prompt or 30s timeout in CI.
func silenceWhoami(t *testing.T) {
	t.Helper()
	// PATH=<empty> would also break other things; instead we just let
	// the providers' Available() returns false guard us. No action needed.
	_ = io.Discard
}

// ---------- profile add / list / show / rm ----------

func TestProfileAddListShowRm(t *testing.T) {
	tmp := testEnv(t)
	silenceWhoami(t)

	// add: creates a file at the expected path.
	stdout, _, err := runCLI(t, "profile", "add", "example", "--label", "Example Profile", "--color", "cyan")
	if err != nil {
		t.Fatalf("add: err=%v stdout=%s", err, stdout)
	}
	if !strings.Contains(stdout, "created") {
		t.Errorf("add stdout missing 'created': %s", stdout)
	}
	dir, err := core.ProfilesDir()
	if err != nil {
		t.Fatalf("ProfilesDir: %v", err)
	}
	if !strings.HasPrefix(dir, tmp) {
		t.Fatalf("ProfilesDir %q not under tmp %q", dir, tmp)
	}
	path := filepath.Join(dir, "example.toml")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected profile file at %s: %v", path, err)
	}

	// add again: fails as invalid usage.
	_, _, err = runCLI(t, "profile", "add", "example")
	if err == nil {
		t.Fatal("add duplicate: expected error")
	}
	if CodeFor(err) != ExitUsage {
		t.Errorf("add duplicate: expected ExitUsage(%d), got %d", ExitUsage, CodeFor(err))
	}

	// add bad name: invalid usage.
	_, _, err = runCLI(t, "profile", "add", "bad name")
	if err == nil {
		t.Fatal("add bad name: expected error")
	}
	if CodeFor(err) != ExitUsage {
		t.Errorf("add bad name: expected ExitUsage, got %d", CodeFor(err))
	}

	// list (human): example appears.
	stdout, _, err = runCLI(t, "profile", "list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(stdout, "example") {
		t.Errorf("list stdout missing 'example': %s", stdout)
	}
	if !strings.Contains(stdout, "Example Profile") {
		t.Errorf("list stdout missing label: %s", stdout)
	}

	// list --json: stable shape.
	stdout, _, err = runCLI(t, "profile", "list", "--json")
	if err != nil {
		t.Fatalf("list --json: %v", err)
	}
	var lst profileListJSON
	if err := json.Unmarshal([]byte(stdout), &lst); err != nil {
		t.Fatalf("list --json parse: %v stdout=%s", err, stdout)
	}
	if len(lst.Profiles) != 1 {
		t.Fatalf("list --json: want 1 profile, got %d", len(lst.Profiles))
	}
	got := lst.Profiles[0]
	if got.Name != "example" || got.Label != "Example Profile" || got.Color != "cyan" {
		t.Errorf("list --json meta wrong: %+v", got)
	}
	if got.HasAzure || got.HasAzd || got.HasGh || got.HasKube || got.HasGit || got.EnvCount != 0 {
		t.Errorf("list --json: empty profile should have no providers/env, got %+v", got)
	}
	if !strings.HasSuffix(got.Path, "example.toml") {
		t.Errorf("list --json path: %q", got.Path)
	}

	// show (human).
	stdout, _, err = runCLI(t, "profile", "show", "example")
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	if !strings.Contains(stdout, "example") {
		t.Errorf("show stdout missing name: %s", stdout)
	}
	if !strings.Contains(stdout, "Example Profile") {
		t.Errorf("show stdout missing label: %s", stdout)
	}

	// show --json: maps to a JSON object with schema/name/label/color.
	stdout, _, err = runCLI(t, "profile", "show", "example", "--json")
	if err != nil {
		t.Fatalf("show --json: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(stdout), &m); err != nil {
		t.Fatalf("show --json parse: %v stdout=%s", err, stdout)
	}
	if m["name"] != "example" {
		t.Errorf("show --json name: %v", m["name"])
	}
	if m["schema"] != core.SchemaVersion {
		t.Errorf("show --json schema: %v", m["schema"])
	}

	// show missing: invalid usage.
	_, _, err = runCLI(t, "profile", "show", "ghost")
	if err == nil {
		t.Fatal("show ghost: expected error")
	}
	if CodeFor(err) != ExitUsage {
		t.Errorf("show ghost: code %d", CodeFor(err))
	}

	// rm without --force or TTY: invalid usage.
	_, _, err = runCLI(t, "profile", "rm", "example")
	if err == nil {
		t.Fatal("rm without --force: expected error")
	}
	if CodeFor(err) != ExitUsage {
		t.Errorf("rm without --force: code %d", CodeFor(err))
	}

	// rm with confirmation y via stdin.
	_, _, err = runCLIWithStdin(t, "y\n", "profile", "rm", "example")
	// even with stdin, our stdinIsTTY check returns false on a bytes
	// reader; rm should still refuse without --force in that case.
	if err == nil {
		t.Fatal("rm with stdin but no TTY: still expected error")
	}

	// rm --force: succeeds, file gone.
	_, _, err = runCLI(t, "profile", "rm", "example", "--force")
	if err != nil {
		t.Fatalf("rm --force: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("after rm --force, file still present: stat err=%v", err)
	}

	// rm --force idempotent on absent profile.
	_, _, err = runCLI(t, "profile", "rm", "example", "--force")
	if err != nil {
		t.Fatalf("rm --force idempotent: %v", err)
	}
}

// TestProfileShowRedacted verifies that --redacted masks subscription /
// tenant / git email and rewrites secret refs to "<ref>".
func TestProfileShowRedacted(t *testing.T) {
	testEnv(t)
	dir, err := core.ProfilesDir()
	if err != nil {
		t.Fatalf("ProfilesDir: %v", err)
	}
	path := filepath.Join(dir, "redact.toml")
	if err := os.WriteFile(path, []byte(`schema = "1"
name = "redact"

[azure]
subscription = "deadbeef-aaaa-bbbb-cccc-1234567890ab"
tenant = "11111111-2222-3333-4444-555555555555"

[git]
user_email = "operator@example.com"

[[env]]
key = "API_KEY"
ref = "op://Vault/Item/password"

[[env]]
key = "FOO"
value = "bar"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, _, err := runCLI(t, "profile", "show", "redact", "--json", "--redacted")
	if err != nil {
		t.Fatalf("show --redacted --json: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(stdout), &m); err != nil {
		t.Fatalf("parse: %v stdout=%s", err, stdout)
	}
	az := m["azure"].(map[string]any)
	if sub, _ := az["subscription"].(string); !strings.HasPrefix(sub, "dead") || !strings.Contains(sub, "*") {
		t.Errorf("subscription not masked: %v", sub)
	}
	if tn, _ := az["tenant"].(string); !strings.Contains(tn, "*") {
		t.Errorf("tenant not masked: %v", tn)
	}
	git := m["git"].(map[string]any)
	if em, _ := git["user_email"].(string); !strings.HasPrefix(em, "*") || !strings.HasSuffix(em, "@example.com") {
		t.Errorf("user_email not masked: %v", em)
	}
	envs := m["env"].([]any)
	if len(envs) != 2 {
		t.Fatalf("envs len: %d", len(envs))
	}
	e0 := envs[0].(map[string]any)
	if e0["ref"] != "<ref>" {
		t.Errorf("env[0] ref not redacted: %v", e0["ref"])
	}
	e1 := envs[1].(map[string]any)
	if e1["value"] != "bar" {
		t.Errorf("env[1] literal value should not be touched: %v", e1["value"])
	}
}

// ensureNoColor double-checks that NO_COLOR is honored — without it,
// snapshot diffs would be polluted by ANSI escapes.
func ensureNoColor(t *testing.T, s string) {
	t.Helper()
	if strings.Contains(s, "\x1b[") {
		t.Errorf("output contains ANSI escape — NO_COLOR not honored:\n%q", s)
	}
}

// drainBytes is a tiny helper so we can reuse newRootCmd assertions
// without re-running through cobra (used by a future expansion).
func drainBytes(buf *bytes.Buffer) string { return buf.String() }

// cobra-friendly accessor — keeps cobra import used even if all tests
// stop using it directly in a refactor.
var _ = (&cobra.Command{}).Name

// ---------- profile set-color ----------

// writeTestProfileToml writes a profile TOML with the given color + label
// and returns the path.
func writeTestProfileToml(t *testing.T, name, color, label string) string {
	t.Helper()
	dir, err := core.ProfilesDir()
	if err != nil {
		t.Fatalf("ProfilesDir: %v", err)
	}
	path := filepath.Join(dir, name+".toml")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "schema = \"1\"\nname = \"" + name + "\"\n"
	if color != "" {
		body += "color = \"" + color + "\"\n"
	}
	if label != "" {
		body += "label = \"" + label + "\"\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestProfileSetColor_UpdatesColorAndSwapsLabelEmoji(t *testing.T) {
	testEnv(t)
	path := writeTestProfileToml(t, "Contoso.Prod-Pilot", "Blue", "🔷 Contoso Prod Pilot")

	stdout, _, err := runCLI(t, "profile", "set-color", "Contoso.Prod-Pilot", "White")
	if err != nil {
		t.Fatalf("set-color: %v\nstdout=%s", err, stdout)
	}
	if !strings.Contains(stdout, "updated") {
		t.Errorf("expected 'updated' in stdout: %s", stdout)
	}

	p, err := core.Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if p.Color != "White" {
		t.Errorf("color = %q, want White", p.Color)
	}
	if p.Label != "⚪ Contoso Prod Pilot" {
		t.Errorf("label = %q, want \"⚪ Contoso Prod Pilot\"", p.Label)
	}
}

func TestProfileSetColor_RejectsUnknownColor(t *testing.T) {
	testEnv(t)
	writeTestProfileToml(t, "foo", "Blue", "Foo")

	_, _, err := runCLI(t, "profile", "set-color", "foo", "Purple")
	if err == nil {
		t.Fatal("expected error for unknown color")
	}
	if CodeFor(err) != ExitUsage {
		t.Errorf("expected ExitUsage, got %d", CodeFor(err))
	}
}

func TestProfileSetColor_MissingProfileErrors(t *testing.T) {
	testEnv(t)

	_, _, err := runCLI(t, "profile", "set-color", "nope", "Cyan")
	if err == nil {
		t.Fatal("expected error for missing profile")
	}
}

func TestProfileSetColor_NoopWhenColorUnchanged(t *testing.T) {
	testEnv(t)
	writeTestProfileToml(t, "foo", "Cyan", "🔵 Foo")

	stdout, _, err := runCLI(t, "profile", "set-color", "foo", "Cyan")
	if err != nil {
		t.Fatalf("noop: %v", err)
	}
	if !strings.Contains(stdout, "already") {
		t.Errorf("expected 'already' in stdout: %s", stdout)
	}
}

func TestProfileSetColor_EmptyColorClearsAndStripsLabelEmoji(t *testing.T) {
	testEnv(t)
	path := writeTestProfileToml(t, "foo", "Cyan", "🔵 Foo")

	stdout, _, err := runCLI(t, "profile", "set-color", "foo", "")
	if err != nil {
		t.Fatalf("clear: %v\nstdout=%s", err, stdout)
	}

	p, err := core.Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if p.Color != "" {
		t.Errorf("color = %q, want empty", p.Color)
	}
	if p.Label != "Foo" {
		t.Errorf("label = %q, want \"Foo\"", p.Label)
	}
}
