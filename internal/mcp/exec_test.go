package mcp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bvorland/profilmanager/internal/core"
	"github.com/bvorland/profilmanager/internal/state"
)

// TestMain doubles as the child process used by exec tests. When the
// MCP_TEST_CHILD env var is set, the test binary acts as a tiny
// "echo what's in $FOO" stand-in so we can drive exec_with_profile
// against a real OS process without depending on cmd.exe / sh.
//
// Co-existence with secrets/state's TestMains is fine — there's only
// one TestMain per package and this package's tests are the only
// users of it.
func TestMain(m *testing.M) {
	if os.Getenv("MCP_TEST_CHILD") == "1" {
		mode := os.Getenv("MCP_TEST_CHILD_MODE")
		switch mode {
		case "echo-env":
			// Print the named env var, terminated with a newline, to
			// stdout. Used to drive the redaction tests.
			key := os.Getenv("MCP_TEST_CHILD_KEY")
			fmt.Println(os.Getenv(key))
		case "sleep":
			// Sleep forever so the timeout path fires. The exec
			// context will SIGKILL us.
			time.Sleep(10 * time.Minute)
		case "exit":
			// Exit with the requested code.
			code := 0
			if v := os.Getenv("MCP_TEST_CHILD_EXITCODE"); v != "" {
				fmt.Sscanf(v, "%d", &code)
			}
			os.Exit(code)
		default:
			fmt.Println("child ok")
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// testChildCommand returns (command, allowlistEntry). The basename of
// the running test binary is what the allowlist needs to permit.
func testChildCommand(t *testing.T) (command, basename string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	bn := filepath.Base(exe)
	bn = strings.TrimSuffix(bn, ".exe")

	// Prepend the test binary's directory to PATH so the bare basename
	// resolves via exec.LookPath. Exec now requires a bare command name
	// (no path separators) — see the v1.0 security review fix in
	// internal/mcp/config.go::commandBasename.
	dir := filepath.Dir(exe)
	sep := string(os.PathListSeparator)
	t.Setenv("PATH", dir+sep+os.Getenv("PATH"))

	// Return the bare basename as the "command" so callers don't have
	// to do this themselves. The second return is the same value;
	// kept for backwards-compatible call sites that use both names.
	return bn, bn
}

// withFreshState plumbs ProfilesDir + StateDir + audit dir to a temp
// tree so each test runs in isolation.
func withFreshState(t *testing.T) (profilesDir, stateDir string) {
	t.Helper()
	root := t.TempDir()
	// internal/core uses LOCALAPPDATA / XDG_STATE_HOME / APPDATA for
	// pathing. Override those so the test stays in its sandbox on
	// every supported OS.
	t.Setenv("LOCALAPPDATA", filepath.Join(root, "local"))
	t.Setenv("APPDATA", filepath.Join(root, "roaming"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))

	pd, err := core.ProfilesDir()
	if err != nil {
		t.Fatalf("ProfilesDir: %v", err)
	}
	sd, err := core.StateDir()
	if err != nil {
		t.Fatalf("StateDir: %v", err)
	}
	// Route audit log into the sandbox too. SetAuditDir is package-
	// global so we restore it after the test.
	SetAuditDir(filepath.Join(sd, "audit"))
	t.Cleanup(func() { SetAuditDir("") })

	// Force PM_SESSION_ID so every audit entry has a stable session
	// field — easier to assert on.
	t.Setenv("PM_SESSION_ID", "exec-test")

	return pd, sd
}

// writeProfile constructs a profile TOML with the given env entries
// and saves it under the active ProfilesDir.
func writeProfile(t *testing.T, name string, env []core.EnvEntry) {
	t.Helper()
	p := &core.Profile{
		Schema: core.SchemaVersion,
		Name:   name,
		Env:    env,
	}
	path, err := core.ProfilePath(name)
	if err != nil {
		t.Fatalf("ProfilePath: %v", err)
	}
	if err := p.Save(path); err != nil {
		t.Fatalf("save profile: %v", err)
	}
}

// TestExec_RedactsSecretFromOutput is the headline test for the iron
// rule: a profile env var carrying a *resolved* secret value MUST be
// redacted from the child's stdout before the result returns.
//
// We use a dotenv://file#KEY ref (rather than a literal value) so the
// resolver path is exercised. Literal env values are not redacted (and
// shouldn't be — they're plain config); only refs that go through a
// Resolver get registered with the redactor.
//
// Flake investigation: this test was flagged as
// potentially order-dependent ("MCP_TEST_CHILD* env var leakage or
// SetConfig/DefaultConfig cleanup"). Ran `go test ./internal/mcp/ -run
// TestExec_ -count=50` — 50 sequential iterations, all PASS, no
// reproduction. Root-cause analysis:
//   - All env manipulation uses t.Setenv (never os.Setenv), so env vars
//     are restored by the testing framework before the next test starts.
//   - SetConfig is followed immediately by t.Cleanup(SetConfig(DefaultConfig))
//     in every test, so the global config is always restored.
//   - No test calls t.Parallel(), so execution is strictly sequential —
//     no shared-state races between tests.
// Watch-list for future CI (if flake resurfaces):
//   1. Add -race to the mcp package test runs — withAuditTempDir peeks
//      auditCfg.dir without holding the lock ("safe because sequential",
//      but -race will flag it).
//   2. If tests ever adopt t.Parallel(), the SetConfig / SetAuditDir
//      globals will need per-test isolation (e.g. a per-test server
//      instance rather than package-level state).
func TestExec_RedactsSecretFromOutput(t *testing.T) {
	withFreshState(t)

	const secret = "ULTRA-SECRET-VALUE-xyz"
	dir := t.TempDir()
	envPath := filepath.Join(dir, "secrets.env")
	if err := os.WriteFile(envPath, []byte("FOO="+secret+"\n"), 0o600); err != nil {
		t.Fatalf("write env fixture: %v", err)
	}

	writeProfile(t, "redact-test", []core.EnvEntry{
		{Key: "FOO", Ref: "dotenv://" + filepath.ToSlash(envPath) + "#FOO"},
	})

	exe, basename := testChildCommand(t)
	SetConfig(Config{
		AllowedCommands:    []string{basename},
		DefaultExecTimeout: 30 * time.Second,
	})
	t.Cleanup(func() { SetConfig(DefaultConfig()) })

	// The child needs the MCP_TEST_CHILD* env vars to pick echo-env
	// mode. We set them on the parent — Exec inherits os.Environ()
	// into the child's env block before overlaying profile values.
	t.Setenv("MCP_TEST_CHILD", "1")
	t.Setenv("MCP_TEST_CHILD_MODE", "echo-env")
	t.Setenv("MCP_TEST_CHILD_KEY", "FOO")

	res, err := Exec(context.Background(), ExecRequest{
		Profile: "redact-test",
		Command: exe,
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit code = %d, stderr=%q", res.ExitCode, res.Stderr)
	}
	if strings.Contains(res.Stdout, secret) {
		t.Fatalf("SECRET LEAKED in stdout: %q", res.Stdout)
	}
	if !strings.Contains(res.Stdout, redactedMarker) {
		t.Fatalf("redaction marker missing from stdout: %q", res.Stdout)
	}
}

// TestExec_AllowlistDeniesCommand verifies that a command outside
// the allowlist never reaches exec.CommandContext.
func TestExec_AllowlistDeniesCommand(t *testing.T) {
	withFreshState(t)
	writeProfile(t, "deny-test", nil)

	// Default allowlist is az/azd/gh/kubectl/git — the test binary's
	// basename is none of those, so we deliberately do NOT add it.
	SetConfig(DefaultConfig())
	t.Cleanup(func() { SetConfig(DefaultConfig()) })

	exe, _ := testChildCommand(t)
	_, err := Exec(context.Background(), ExecRequest{
		Profile: "deny-test",
		Command: exe,
	})
	if !errors.Is(err, ErrCommandNotAllowed) {
		t.Fatalf("want ErrCommandNotAllowed, got %v", err)
	}

	// Audit log should show a "denied" entry — the agent's attempt is
	// recorded even though the process never spawned.
	dir, _ := AuditDir()
	entries := readAuditLines(t, dir)
	if len(entries) == 0 {
		t.Fatal("no audit entries written for denied call")
	}
	last := entries[len(entries)-1]
	if last.Result != "denied" || last.Tool != "exec_with_profile" {
		t.Errorf("denial not audited: %+v", last)
	}
}

// TestExec_AllowlistRejectsAbsolutePathPrefix is the regression test
// for the v1.0 pre-publish security review finding (HIGH severity):
// even when the basename of the supplied command IS on the allowlist,
// any path-prefixed input must be rejected — otherwise an attacker
// who can prompt-inject the agent and plant a file at a predictable
// path (e.g. C:\Users\Public\az.exe) could route exec_with_profile
// through their binary while keeping the resolved profile secrets
// flowing into its env.
func TestExec_AllowlistRejectsAbsolutePathPrefix(t *testing.T) {
	withFreshState(t)
	writeProfile(t, "path-prefix-test", nil)

	// Allowlist explicitly includes "az" so the only thing rejecting
	// the call is the path-prefix check, not the basename check.
	SetConfig(Config{
		AllowedCommands:    []string{"az"},
		DefaultExecTimeout: 30 * time.Second,
	})
	t.Cleanup(func() { SetConfig(DefaultConfig()) })

	attackerPaths := []string{
		`C:\Users\Public\az.exe`,
		`/usr/bin/az`,
		`./az`,
		`..\az.exe`,
		`\\evil-share\az.exe`,
		`bin/az`,
	}
	for _, path := range attackerPaths {
		t.Run(path, func(t *testing.T) {
			_, err := Exec(context.Background(), ExecRequest{
				Profile: "path-prefix-test",
				Command: path,
			})
			if !errors.Is(err, ErrCommandNotAllowed) {
				t.Fatalf("Exec(%q): want ErrCommandNotAllowed (path-prefix bypass MUST be blocked), got %v", path, err)
			}
		})
	}
}

// TestExec_TimeoutKillsChild covers the timeout guardrail: a child
// that sleeps forever is killed within the configured budget and the
// result is marked TimedOut.
func TestExec_TimeoutKillsChild(t *testing.T) {
	if testing.Short() {
		t.Skip("timeout test uses real wall-clock sleep")
	}
	withFreshState(t)
	writeProfile(t, "timeout-test", nil)

	exe, basename := testChildCommand(t)
	SetConfig(Config{
		AllowedCommands:    []string{basename},
		DefaultExecTimeout: 1 * time.Second,
		MaxExecTimeout:     2 * time.Second,
	})
	t.Cleanup(func() { SetConfig(DefaultConfig()) })

	t.Setenv("MCP_TEST_CHILD", "1")
	t.Setenv("MCP_TEST_CHILD_MODE", "sleep")

	start := time.Now()
	res, err := Exec(context.Background(), ExecRequest{
		Profile:        "timeout-test",
		Command:        exe,
		TimeoutSeconds: 1,
	})
	dur := time.Since(start)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if !res.TimedOut {
		t.Errorf("TimedOut flag not set: %+v", res)
	}
	if dur > 5*time.Second {
		t.Errorf("timeout took too long: %v", dur)
	}
}

// TestExec_NoProfileNoActiveErrors covers the empty-profile path:
// caller didn't specify, session has no active profile.
func TestExec_NoProfileNoActiveErrors(t *testing.T) {
	withFreshState(t)
	// Belt-and-braces: ensure no active profile from a stray test.
	_ = state.ClearActiveProfile()

	_, err := Exec(context.Background(), ExecRequest{
		Command: "az", // allowed by default, but profile lookup fails first
		Args:    []string{"version"},
	})
	if !errors.Is(err, ErrNoProfile) {
		t.Fatalf("want ErrNoProfile, got %v", err)
	}
}

// TestExec_AuditWritesRedactedPreview combines the two iron-rule
// guarantees: the audit log records a preview of stdout, AND that
// preview has the same redaction applied as the returned result.
func TestExec_AuditWritesRedactedPreview(t *testing.T) {
	withFreshState(t)

	const secret = "PREVIEW-SECRET-abcdef123"
	dir := t.TempDir()
	envPath := filepath.Join(dir, "secrets.env")
	if err := os.WriteFile(envPath, []byte("BAR="+secret+"\n"), 0o600); err != nil {
		t.Fatalf("write env fixture: %v", err)
	}
	writeProfile(t, "preview-test", []core.EnvEntry{
		{Key: "BAR", Ref: "dotenv://" + filepath.ToSlash(envPath) + "#BAR"},
	})

	exe, basename := testChildCommand(t)
	SetConfig(Config{
		AllowedCommands:    []string{basename},
		DefaultExecTimeout: 30 * time.Second,
	})
	t.Cleanup(func() { SetConfig(DefaultConfig()) })

	t.Setenv("MCP_TEST_CHILD", "1")
	t.Setenv("MCP_TEST_CHILD_MODE", "echo-env")
	t.Setenv("MCP_TEST_CHILD_KEY", "BAR")

	if _, err := Exec(context.Background(), ExecRequest{
		Profile: "preview-test",
		Command: exe,
	}); err != nil {
		t.Fatalf("Exec: %v", err)
	}

	auditDir, _ := AuditDir()
	entries := readAuditLines(t, auditDir)
	if len(entries) == 0 {
		t.Fatal("no audit entries written")
	}
	last := entries[len(entries)-1]
	if strings.Contains(last.OutputPreview, secret) {
		t.Errorf("audit log contains secret in preview: %q", last.OutputPreview)
	}
	if last.Profile != "preview-test" {
		t.Errorf("Profile field wrong: %q", last.Profile)
	}
	if last.Command != exe {
		t.Errorf("Command field wrong: %q", last.Command)
	}
}

// TestCappedBuffer_TruncatesAndAnnotates is a unit test for the
// MaxOutputBytes guard: bytes beyond the cap are dropped, the writer
// claims it accepted them all (so the child does not block on EPIPE),
// and the read-out includes a TRUNCATED notice.
func TestCappedBuffer_TruncatesAndAnnotates(t *testing.T) {
	var b cappedBuffer
	b.cap = 8
	n, err := b.Write([]byte("hello world this is more than eight bytes"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != len("hello world this is more than eight bytes") {
		t.Errorf("Write returned %d, expected full length", n)
	}
	out := string(b.Bytes())
	if !strings.HasPrefix(out, "hello wo") {
		t.Errorf("buffer prefix wrong: %q", out)
	}
	if !strings.Contains(out, "TRUNCATED") {
		t.Errorf("truncation notice missing: %q", out)
	}
}
