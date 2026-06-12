//go:build integration

// Package-level: this file is the CLI-layer integration test suite.
// Run via:
//
//	go test ./internal/cli -tags integration -run TestIntegration
//
// It is gated by a build tag (not -short) because it shells out to a
// fresh `go build` of the pm binary into a tempdir; we don't want this
// running on every dev-loop `go test ./...`.
//
// Coverage:
//   - Builds the pm binary from source into a tempdir.
//   - Sets up fake `az`, `azd`, `gh`, `kubectl`, `git` shell scripts on
//     a PATH-prepended tempdir.
//   - Runs `pm whoami --json` end-to-end and asserts the fake values
//     come back through the cobra → providers → JSON envelope path.
//   - Runs `pm exec <profile> -- printenv AZURE_CONFIG_DIR` against a
//     profile with a literal AZURE_CONFIG_DIR and asserts env propagation.
//
// Why integration tests, not unit tests with mocks? The providers
// package already has fake-CLI unit tests (`internal/providers/fakecli_test.go`)
// that prove each Provider talks to its CLI the way we expect. The
// CLI-layer tests here prove the *wiring* — that cobra's args, the
// JSON envelope, exit codes, and the providers' actual `exec.LookPath`
// → `runCmd` path agree end-to-end on real syscalls. That's a different
// failure mode (e.g. cobra Setenv ordering, PATH lookup quirks,
// concurrency bugs in the registry).
//
// Platform note: Linux/macOS only. The fake-CLI shim uses bash. On
// Windows you'd need .cmd files with their own quoting rules — the v1
// budget didn't include that re-implementation. CI runs this job on
// ubuntu-latest only; the unit tests cover all three platforms.

package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// requireUnix skips the test on Windows. The fake CLI scripts use bash
// `case` statements that translate poorly to `.cmd`.
func requireUnix(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("CLI integration tests are Linux/macOS only — see file header")
	}
}

// requireGoToolchain skips when `go` isn't on PATH (rare but possible
// in stripped CI containers).
func requireGoToolchain(t *testing.T) string {
	t.Helper()
	p, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("`go` not on PATH: %v", err)
	}
	return p
}

// repoRoot walks parents until it finds the go.mod, so the test isn't
// fragile to where `go test` is invoked from.
func repoRoot(t *testing.T) string {
	t.Helper()
	cur, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(cur, "go.mod")); err == nil {
			return cur
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			t.Fatalf("could not find go.mod above %s", cur)
		}
		cur = parent
	}
	t.Fatalf("repoRoot: walked too far from %s", cur)
	return ""
}

// buildPMBinary compiles cmd/pm into a fresh tempdir and returns the
// absolute path to the binary. Builds happen once per test process via
// t.TempDir() under the test name — multiple Test* functions in the
// same package therefore re-build, which is intentional: tests must be
// independent.
func buildPMBinary(t *testing.T) string {
	t.Helper()
	goBin := requireGoToolchain(t)
	root := repoRoot(t)
	out := filepath.Join(t.TempDir(), "pm")
	cmd := exec.Command(goBin, "build", "-o", out, "./cmd/pm")
	cmd.Dir = root
	if buf, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, buf)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("built binary missing: %v", err)
	}
	return out
}

// fakeProvider describes one of the CLI tools we shim. The Script field
// is bash source that dispatches on $1/$2 and writes JSON to stdout.
type fakeProvider struct {
	Name   string
	Script string
}

// writeFakeProviders installs a known set of fake CLIs onto a tempdir
// then prepends that dir to PATH for the rest of the test. Each fake
// emits JSON that mirrors what the real CLI emits — just enough that
// providers.azProvider / ghProvider / etc parse it.
func writeFakeProviders(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	fakes := []fakeProvider{
		{
			Name: "az",
			// `az account show -o json` is what providers.azProvider hits.
			Script: `#!/bin/sh
case "$1 $2" in
  "account show")
    cat <<'JSON'
{
  "id": "fake-sub-id-1234",
  "name": "Fake Subscription",
  "tenantId": "fake-tenant-id-5678",
  "user": {"name": "fake-az-user@example.com", "type": "user"}
}
JSON
    exit 0
    ;;
  *)
    # Quiet default for any other invocation (e.g. config show).
    echo "{}"
    exit 0
    ;;
esac
`,
		},
		{
			Name: "azd",
			// `azd auth token --output json` is what providers.azdProvider hits.
			Script: `#!/bin/sh
case "$1 $2" in
  "auth token")
    cat <<'JSON'
{"token": "fake.jwt.token", "expiresOn": "2099-01-01T00:00:00Z"}
JSON
    exit 0
    ;;
  *)
    echo "{}"
    exit 0
    ;;
esac
`,
		},
		{
			Name: "gh",
			// `gh auth status --json hosts` — providers.ghProvider parses
			// the v2 wrapped shape first then falls back to v1.
			Script: `#!/bin/sh
case "$1 $2" in
  "auth status")
    cat <<'JSON'
{"hosts": {"github.com": [{"login": "fake-gh-user", "active": true, "tokenSource": "oauth_token"}]}}
JSON
    exit 0
    ;;
  *)
    echo "{}"
    exit 0
    ;;
esac
`,
		},
		{
			Name: "kubectl",
			// kubectl is two-call: current-context, then config view --minify.
			Script: `#!/bin/sh
case "$1 $2" in
  "config current-context")
    echo "fake-context"
    exit 0
    ;;
  "config view")
    cat <<'JSON'
{
  "contexts": [{
    "name": "fake-context",
    "context": {"cluster": "fake-cluster", "user": "fake-kube-user", "namespace": "fake-ns"}
  }]
}
JSON
    exit 0
    ;;
  *)
    echo "{}"
    exit 0
    ;;
esac
`,
		},
		{
			Name: "git",
			// git's whoami is three `git config --get` calls.
			Script: `#!/bin/sh
if [ "$1" = "config" ] && [ "$2" = "--get" ]; then
  case "$3" in
    user.name)       echo "Fake Git User"; exit 0 ;;
    user.email)      echo "fake-git@example.com"; exit 0 ;;
    user.signingkey) exit 1 ;;
  esac
fi
echo ""
exit 0
`,
		},
	}
	for _, f := range fakes {
		path := filepath.Join(dir, f.Name)
		if err := os.WriteFile(path, []byte(f.Script), 0o755); err != nil {
			t.Fatalf("write fake %s: %v", f.Name, err)
		}
	}
	// Prepend the fake dir; keep the real PATH for `sh` / `bash`.
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

// runBinary executes a built pm binary with args and a custom env,
// capturing stdout/stderr. Times out at 30s to avoid wedging CI.
func runBinary(t *testing.T, binary string, env []string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Env = env
	out, errBuf := &strings.Builder{}, &strings.Builder{}
	cmd.Stdout, cmd.Stderr = out, errBuf
	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		code := 0
		if err != nil {
			var ee *exec.ExitError
			if asExitErr(err, &ee) {
				code = ee.ExitCode()
			} else {
				t.Fatalf("wait: %v", err)
			}
		}
		return out.String(), errBuf.String(), code
	case <-time.After(30 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("binary timed out: %s %v", binary, args)
		return "", "", -1
	}
}

// asExitErr is a tiny wrapper around errors.As that keeps the test file
// free of an `errors` import for this single use.
func asExitErr(err error, target **exec.ExitError) bool {
	if ee, ok := err.(*exec.ExitError); ok {
		*target = ee
		return true
	}
	return false
}

// minimalEnv returns an env slice with just PATH, HOME-equivalent
// vars, and the standard PM session env. We deliberately do NOT
// inherit the operator's whole env — that's the point of isolation.
func minimalEnv(t *testing.T, tmp string) []string {
	t.Helper()
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + tmp,
		"XDG_CONFIG_HOME=" + filepath.Join(tmp, "xdgcfg"),
		"XDG_STATE_HOME=" + filepath.Join(tmp, "xdgstate"),
		"PM_SESSION_ID=integration-" + t.Name(),
		"NO_COLOR=1",
		// Defensive: clear anything that could redirect providers to
		// real cloud config dirs.
		"AZURE_CONFIG_DIR=" + filepath.Join(tmp, "az"),
		"AZD_CONFIG_DIR=" + filepath.Join(tmp, "azd"),
	}
	return env
}

// TestIntegrationWhoamiWithFakeProviders is the smoke test: build pm,
// shim every provider CLI, run `pm whoami --json`, assert the fakes
// reach back through the envelope.
func TestIntegrationWhoamiWithFakeProviders(t *testing.T) {
	requireUnix(t)
	if testing.Short() {
		t.Skip("integration tests skipped under -short")
	}
	tmp := t.TempDir()
	writeFakeProviders(t)
	pm := buildPMBinary(t)

	stdout, stderr, code := runBinary(t, pm, minimalEnv(t, tmp), "whoami", "--json")
	if code != 0 {
		t.Fatalf("whoami exit=%d stderr=%s stdout=%s", code, stderr, stdout)
	}
	var rep whoamiReport
	if err := json.Unmarshal([]byte(stdout), &rep); err != nil {
		t.Fatalf("parse: %v stdout=%s", err, stdout)
	}
	want := map[string]struct {
		LoggedIn bool
		// One distinctive field per provider — proves the fake's data
		// actually flowed through.
		Marker string
	}{
		"az":      {true, "fake-sub-id-1234"},
		"azd":     {true, ""}, // fake JWT doesn't decode cleanly; just LoggedIn
		"gh":      {true, "fake-gh-user"},
		"kubectl": {true, "fake-context"},
		"git":     {true, "Fake Git User"},
	}
	got := map[string]providersStatusLite{}
	for _, s := range rep.Providers {
		got[s.Provider] = providersStatusLite{
			LoggedIn: s.LoggedIn,
			Account:  s.Account,
			Sub:      s.Subscription,
		}
	}
	for prov, w := range want {
		g, ok := got[prov]
		if !ok {
			t.Errorf("provider %q missing from whoami output: %+v", prov, got)
			continue
		}
		if g.LoggedIn != w.LoggedIn {
			t.Errorf("provider %q: LoggedIn want=%v got=%v", prov, w.LoggedIn, g.LoggedIn)
		}
		if w.Marker != "" {
			joined := g.Account + " " + g.Sub
			if !strings.Contains(joined, w.Marker) {
				t.Errorf("provider %q: expected marker %q in account/sub %q", prov, w.Marker, joined)
			}
		}
	}
}

// providersStatusLite is the subset of providers.Status the
// integration tests care about. Mirroring the relevant fields without
// importing providers keeps this file independent of internal
// package layout — if a field in providers.Status is renamed, the
// test still compiles and points at the right spot to fix.
type providersStatusLite struct {
	LoggedIn bool
	Account  string
	Sub      string
}

// TestIntegrationExecPropagatesEnv exercises the full `pm exec <profile>
// -- <cmd>` path end-to-end: a profile with a literal AZURE_CONFIG_DIR
// env entry is loaded, the child process receives the merged env, and
// its output confirms the value was propagated correctly.
//
// This test runs the real pm binary (built from source) with a
// synthesised profile and verifies that runner.EnvSlice's profile-wins
// semantics survive the full cobra → runner → exec.Cmd path.
func TestIntegrationExecPropagatesEnv(t *testing.T) {
	requireUnix(t)
	if testing.Short() {
		t.Skip("integration tests skipped under -short")
	}
	tmp := t.TempDir()
	writeFakeProviders(t) // installs fake az/azd/gh/kubectl/git on PATH
	pm := buildPMBinary(t)

	// Compute the profiles directory the pm binary will use.
	// minimalEnv passes HOME=tmp (macOS uses UserHomeDir → HOME) and
	// XDG_CONFIG_HOME=${tmp}/xdgcfg (Linux). Mirror that logic here so
	// the profile file lands where pm will look for it.
	var profilesDir string
	switch runtime.GOOS {
	case "darwin":
		profilesDir = filepath.Join(tmp, "Library", "Application Support", "profilmanager", "profiles")
	default: // linux and other Unix
		profilesDir = filepath.Join(tmp, "xdgcfg", "profilmanager", "profiles")
	}
	if err := os.MkdirAll(profilesDir, 0o755); err != nil {
		t.Fatalf("create profiles dir: %v", err)
	}

	// Unique value to propagate — distinct from the placeholder
	// AZURE_CONFIG_DIR that minimalEnv injects, so we can tell them apart.
	wantAzureDir := filepath.Join(tmp, "exec-test-azure-cfg")

	// Minimal TOML profile: schema, name, one literal env entry.
	// The [[env]] array-of-tables syntax is required by go-toml/v2.
	profileTOML := fmt.Sprintf(
		"schema = \"1\"\nname = \"exec-test\"\n\n[[env]]\nkey = \"AZURE_CONFIG_DIR\"\nvalue = %q\n",
		wantAzureDir,
	)
	if err := os.WriteFile(filepath.Join(profilesDir, "exec-test.toml"), []byte(profileTOML), 0o644); err != nil {
		t.Fatalf("write profile: %v", err)
	}

	// `printenv VAR` is a POSIX standard utility — safe to call since
	// requireUnix guards Windows. It prints the named variable's value
	// then exits 0, giving us a clean single-line assert.
	env := minimalEnv(t, tmp)
	stdout, stderr, code := runBinary(t, pm, env,
		"exec", "exec-test", "--", "printenv", "AZURE_CONFIG_DIR")
	if code != 0 {
		t.Fatalf("pm exec exit=%d\nstderr: %s\nstdout: %s", code, stderr, stdout)
	}

	got := strings.TrimSpace(stdout)
	if got != wantAzureDir {
		t.Errorf("AZURE_CONFIG_DIR not propagated correctly:\n  want: %q\n   got: %q\n  stderr: %s",
			wantAzureDir, got, stderr)
	}
}

// TestIntegrationDoctorFindsAllTools verifies that with fakes on PATH,
// every tool-available check flips to OK (no warns). Belt-and-braces:
// proves both the fake harness and the doctor wiring.
func TestIntegrationDoctorFindsAllTools(t *testing.T) {
	requireUnix(t)
	if testing.Short() {
		t.Skip("integration tests skipped under -short")
	}
	tmp := t.TempDir()
	writeFakeProviders(t)
	// pwsh isn't shimmed (we never invoke it from doctor), so add a
	// pwsh stub too so the tool-available:pwsh check turns green.
	fakeDir := filepath.Dir(t.TempDir()) // just any writable; we'll write into the PATH dir
	_ = fakeDir
	// Reuse PATH from writeFakeProviders: grab the first segment.
	pathDir := strings.Split(os.Getenv("PATH"), string(os.PathListSeparator))[0]
	pwshScript := "#!/bin/sh\necho 'PowerShell 7.0.0 (fake)'\nexit 0\n"
	if err := os.WriteFile(filepath.Join(pathDir, "pwsh"), []byte(pwshScript), 0o755); err != nil {
		t.Fatalf("write pwsh fake: %v", err)
	}
	pm := buildPMBinary(t)
	stdout, stderr, code := runBinary(t, pm, minimalEnv(t, tmp), "doctor", "--json")
	// doctor may exit non-zero if a non-tool check fails; we assert
	// per-tool status only.
	if code != 0 && code != 1 {
		t.Fatalf("doctor unexpected exit=%d stderr=%s", code, stderr)
	}
	var doc struct {
		Checks []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"checks"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("parse: %v stdout=%s", err, stdout)
	}
	wantOK := []string{"az", "azd", "gh", "git", "kubectl", "pwsh"}
	got := map[string]string{}
	for _, c := range doc.Checks {
		if strings.HasPrefix(c.Name, "tool-available:") {
			got[strings.TrimPrefix(c.Name, "tool-available:")] = c.Status
		}
	}
	for _, tool := range wantOK {
		if s, ok := got[tool]; !ok {
			t.Errorf("missing tool-available check for %s", tool)
		} else if s != "ok" {
			t.Errorf("tool %s: want status ok, got %s", tool, s)
		}
	}
}

// TestIntegrationVersionPrints exercises the version path end-to-end —
// catches embed/ldflags mistakes that wouldn't show up in unit tests.
func TestIntegrationVersionPrints(t *testing.T) {
	requireUnix(t)
	if testing.Short() {
		t.Skip("integration tests skipped under -short")
	}
	tmp := t.TempDir()
	pm := buildPMBinary(t)
	stdout, stderr, code := runBinary(t, pm, minimalEnv(t, tmp), "version")
	if code != 0 {
		t.Fatalf("version exit=%d stderr=%s", code, stderr)
	}
	if !strings.HasPrefix(stdout, "pm ") {
		t.Errorf("version output should start with 'pm ', got: %q", stdout)
	}
	if !strings.Contains(stdout, runtime.GOOS) {
		t.Errorf("version output should include GOOS=%s: %q", runtime.GOOS, stdout)
	}
}

// Make `fmt` import non-dead in case future tests add Printf debugging.
// Avoid lint trouble on a quiet day.
var _ = fmt.Sprint
