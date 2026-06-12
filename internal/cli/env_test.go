package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bvorland/profilmanager/internal/core"
)

// withTempDirs redirects APPDATA, LOCALAPPDATA, XDG_CONFIG_HOME, and
// XDG_STATE_HOME to a fresh tempdir so a test's profile + state writes
// don't bleed into the operator's home. Returns the tempdir for assert
// helpers and a cleanup the caller must defer.
func withTempDirs(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("APPDATA", dir)
	t.Setenv("LOCALAPPDATA", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_STATE_HOME", dir)
	// Pin a deterministic session ID so the env-keys file is the same
	// across runs (no PPID fallback).
	t.Setenv("PM_SESSION_ID", "test-session-env")
	return dir
}

func writeProfile(t *testing.T, p *core.Profile) string {
	t.Helper()
	if p.Schema == "" {
		p.Schema = core.SchemaVersion
	}
	path, err := core.ProfilePath(p.Name)
	if err != nil {
		t.Fatalf("ProfilePath: %v", err)
	}
	if err := p.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	return path
}

func TestEnvApplyBashOutput(t *testing.T) {
	withTempDirs(t)
	writeProfile(t, &core.Profile{
		Name: "demo",
		Env: []core.EnvEntry{
			{Key: "FOO", Value: "bar"},
			{Key: "TRICKY", Value: `a'b"c`},
		},
	})

	root := newRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"env", "apply", "demo", "--shell", "bash"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v -- stderr: %s", err, stderr.String())
	}
	got := stdout.String()
	if !strings.Contains(got, `export FOO='bar'`) {
		t.Fatalf("missing literal export: %s", got)
	}
	// POSIX single-quote escape: a'b"c → 'a'\''b"c'
	if !strings.Contains(got, `export TRICKY='a'\''b"c'`) {
		t.Fatalf("TRICKY not escaped correctly:\n%s", got)
	}
	// AzProvider always contributes AZURE_CORE_OUTPUT=json.
	if !strings.Contains(got, `export AZURE_CORE_OUTPUT='json'`) {
		t.Fatalf("missing provider env: %s", got)
	}
}

func TestEnvApplyPwshSingleQuoteEscape(t *testing.T) {
	withTempDirs(t)
	writeProfile(t, &core.Profile{
		Name: "psdemo",
		Env: []core.EnvEntry{
			{Key: "WEIRD", Value: "it's me"},
		},
	})

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"env", "apply", "psdemo", "--shell", "pwsh"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v -- %s", err, out.String())
	}
	got := out.String()
	// PowerShell single-quote: doubled apostrophe.
	if !strings.Contains(got, `$env:WEIRD = 'it''s me'`) {
		t.Fatalf("pwsh escape wrong:\n%s", got)
	}
}

func TestEnvApplyFishSyntax(t *testing.T) {
	withTempDirs(t)
	writeProfile(t, &core.Profile{
		Name: "fishd",
		Env:  []core.EnvEntry{{Key: "F", Value: "v"}},
	})

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"env", "apply", "fishd", "--shell", "fish"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v -- %s", err, out.String())
	}
	if !strings.Contains(out.String(), "set -gx F 'v'") {
		t.Fatalf("fish output wrong:\n%s", out.String())
	}
}

func TestEnvApplyCmdRefusesUnsafeValue(t *testing.T) {
	withTempDirs(t)
	writeProfile(t, &core.Profile{
		Name: "cmdd",
		Env:  []core.EnvEntry{{Key: "BAD", Value: "has\nnewline"}},
	})

	root := newRootCmd()
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetArgs([]string{"env", "apply", "cmdd", "--shell", "cmd"})
	if err := root.Execute(); err == nil {
		t.Fatalf("expected error for unsafe cmd value, got none")
	}
	if !strings.Contains(errBuf.String(), "cmd.exe") {
		t.Fatalf("error should mention cmd.exe limitation: %s", errBuf.String())
	}
}

func TestEnvApplyRefusesUnresolvedRefsByDefault(t *testing.T) {
	withTempDirs(t)
	writeProfile(t, &core.Profile{
		Name: "secret",
		Env:  []core.EnvEntry{{Key: "TOKEN", Ref: "op://Vault/Item/Field"}},
	})

	root := newRootCmd()
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetArgs([]string{"env", "apply", "secret", "--shell", "bash"})
	if err := root.Execute(); err == nil {
		t.Fatalf("expected refusal for ref-bearing profile, got none")
	}
	combined := errBuf.String()
	if !strings.Contains(combined, "secret refs") || !strings.Contains(combined, "pm exec") {
		t.Fatalf("refusal message missing guidance: %s", combined)
	}
}

func TestEnvApplyAllowUnresolvedRefsPassthroughLiteralRef(t *testing.T) {
	withTempDirs(t)
	writeProfile(t, &core.Profile{
		Name: "secret",
		Env:  []core.EnvEntry{{Key: "TOKEN", Ref: "op://Vault/Item/Field"}},
	})

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"env", "apply", "secret", "--shell", "bash", "--allow-unresolved-refs"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v -- %s", err, out.String())
	}
	if !strings.Contains(out.String(), `export TOKEN='op://Vault/Item/Field'`) {
		t.Fatalf("expected literal ref passthrough:\n%s", out.String())
	}
}

func TestEnvApplyEmitsPMActiveProfile(t *testing.T) {
	// PM_ACTIVE_PROFILE is the meta-var dashboard/doctor/whoami key on
	// to decide "is a profile active in this shell". env apply MUST
	// synthesize it (Compose does not), or evaluating the output will
	// leave the dashboard saying "(none)" even after a successful apply.
	withTempDirs(t)
	writeProfile(t, &core.Profile{
		Name: "p-active",
		Env:  []core.EnvEntry{{Key: "FOO", Value: "bar"}},
	})

	t.Run("bash", func(t *testing.T) {
		root := newRootCmd()
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetArgs([]string{"env", "apply", "p-active", "--shell", "bash"})
		if err := root.Execute(); err != nil {
			t.Fatalf("execute: %v -- %s", err, out.String())
		}
		if !strings.Contains(out.String(), `export PM_ACTIVE_PROFILE='p-active'`) {
			t.Fatalf("missing PM_ACTIVE_PROFILE export:\n%s", out.String())
		}
	})

	t.Run("pwsh", func(t *testing.T) {
		root := newRootCmd()
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetArgs([]string{"env", "apply", "p-active", "--shell", "pwsh"})
		if err := root.Execute(); err != nil {
			t.Fatalf("execute: %v -- %s", err, out.String())
		}
		if !strings.Contains(out.String(), `$env:PM_ACTIVE_PROFILE = 'p-active'`) {
			t.Fatalf("missing PM_ACTIVE_PROFILE export:\n%s", out.String())
		}
	})

	t.Run("switching profiles unsets old PM_ACTIVE_PROFILE", func(t *testing.T) {
		// First apply.
		root1 := newRootCmd()
		var b1 bytes.Buffer
		root1.SetOut(&b1)
		root1.SetErr(&b1)
		root1.SetArgs([]string{"env", "apply", "p-active", "--shell", "bash"})
		if err := root1.Execute(); err != nil {
			t.Fatalf("apply 1: %v -- %s", err, b1.String())
		}
		// Second apply (same profile is fine — point is the previous
		// PM_ACTIVE_PROFILE must appear in the unset block so apply is
		// idempotent and switching profiles doesn't leak stale state).
		root2 := newRootCmd()
		var b2 bytes.Buffer
		root2.SetOut(&b2)
		root2.SetErr(&b2)
		root2.SetArgs([]string{"env", "apply", "p-active", "--shell", "bash"})
		if err := root2.Execute(); err != nil {
			t.Fatalf("apply 2: %v -- %s", err, b2.String())
		}
		if !strings.Contains(b2.String(), "unset PM_ACTIVE_PROFILE") {
			t.Fatalf("expected unset PM_ACTIVE_PROFILE before re-export:\n%s", b2.String())
		}
	})
}

func TestEnvApplyEmitsUnsetForPreviouslyAppliedKeys(t *testing.T) {
	dir := withTempDirs(t)
	_ = dir
	writeProfile(t, &core.Profile{
		Name: "p1",
		Env:  []core.EnvEntry{{Key: "P1_ONLY", Value: "1"}},
	})
	writeProfile(t, &core.Profile{
		Name: "p2",
		Env:  []core.EnvEntry{{Key: "P2_ONLY", Value: "2"}},
	})

	// Apply p1.
	root1 := newRootCmd()
	var b1 bytes.Buffer
	root1.SetOut(&b1)
	root1.SetErr(&b1)
	root1.SetArgs([]string{"env", "apply", "p1", "--shell", "bash"})
	if err := root1.Execute(); err != nil {
		t.Fatalf("apply p1: %v -- %s", err, b1.String())
	}
	// Apply p2 — should emit `unset P1_ONLY` before the new exports.
	root2 := newRootCmd()
	var b2 bytes.Buffer
	root2.SetOut(&b2)
	root2.SetErr(&b2)
	root2.SetArgs([]string{"env", "apply", "p2", "--shell", "bash"})
	if err := root2.Execute(); err != nil {
		t.Fatalf("apply p2: %v -- %s", err, b2.String())
	}
	out := b2.String()
	if !strings.Contains(out, "unset P1_ONLY") {
		t.Fatalf("expected unset for previously applied key:\n%s", out)
	}
	if !strings.Contains(out, `export P2_ONLY='2'`) {
		t.Fatalf("expected new export:\n%s", out)
	}
}

func TestEnvApplyUnsetOnly(t *testing.T) {
	withTempDirs(t)
	writeProfile(t, &core.Profile{
		Name: "p1",
		Env:  []core.EnvEntry{{Key: "FOO", Value: "1"}},
	})
	// Apply once to seed the keys file.
	root1 := newRootCmd()
	var b1 bytes.Buffer
	root1.SetOut(&b1)
	root1.SetErr(&b1)
	root1.SetArgs([]string{"env", "apply", "p1", "--shell", "pwsh"})
	if err := root1.Execute(); err != nil {
		t.Fatalf("seed: %v -- %s", err, b1.String())
	}
	// Now --unset only.
	root2 := newRootCmd()
	var b2 bytes.Buffer
	root2.SetOut(&b2)
	root2.SetErr(&b2)
	root2.SetArgs([]string{"env", "apply", "--shell", "pwsh", "--unset"})
	if err := root2.Execute(); err != nil {
		t.Fatalf("unset: %v -- %s", err, b2.String())
	}
	if !strings.Contains(b2.String(), "Remove-Item -ErrorAction SilentlyContinue env:FOO") {
		t.Fatalf("expected pwsh remove-item, got:\n%s", b2.String())
	}
	if strings.Contains(b2.String(), "$env:") {
		t.Fatalf("--unset should not emit exports:\n%s", b2.String())
	}
}

func TestEnvApplyWithoutArgRequiresTTY(t *testing.T) {
	withTempDirs(t)
	withNonTTYStdin(t)
	writeProfile(t, &core.Profile{
		Name: "actv",
		Env:  []core.EnvEntry{{Key: "ACTIVE", Value: "yes"}},
	})
	// Activate via `pm switch`.
	rs := newRootCmd()
	var sb bytes.Buffer
	rs.SetOut(&sb)
	rs.SetErr(&sb)
	rs.SetArgs([]string{"switch", "actv", "--quiet"})
	if err := rs.Execute(); err != nil {
		t.Fatalf("switch: %v -- %s", err, sb.String())
	}
	// Without a profile arg, non-interactive callers must pass a name.
	r := newRootCmd()
	var b bytes.Buffer
	r.SetOut(&b)
	r.SetErr(&b)
	r.SetIn(strings.NewReader(""))
	r.SetArgs([]string{"env", "apply", "--shell", "bash"})
	if err := r.Execute(); err == nil {
		t.Fatalf("apply without arg: expected error")
	}
	if !strings.Contains(b.String(), "stdin is not a TTY") {
		t.Fatalf("expected non-TTY profile error, got:\n%s", b.String())
	}
}

// Make sure detectShell + canonicalShell don't drift apart.
func TestCanonicalShellNormalizes(t *testing.T) {
	cases := map[string]string{
		"BASH":       "bash",
		"PowerShell": "pwsh",
		"cmd.exe":    "cmd",
		"":           "",
	}
	for in, want := range cases {
		if got := canonicalShell(in); got != want {
			t.Errorf("canonicalShell(%q) = %q, want %q", in, got, want)
		}
	}
}

// Sanity check that we're not somehow leaking absolute paths into output
// (handy when refactoring the banner).
func TestEnvApplyBannerNamesProfile(t *testing.T) {
	withTempDirs(t)
	writeProfile(t, &core.Profile{
		Name: "banner",
	})
	r := newRootCmd()
	var b bytes.Buffer
	r.SetOut(&b)
	r.SetErr(&b)
	r.SetArgs([]string{"env", "apply", "banner", "--shell", "bash"})
	if err := r.Execute(); err != nil {
		t.Fatalf("execute: %v -- %s", err, b.String())
	}
	if !strings.Contains(b.String(), "pm env apply banner") {
		t.Fatalf("banner missing profile name:\n%s", b.String())
	}
}

func TestEnvApplyShouldShowBannerRejectsUnsupportedOrNonTTYStdout(t *testing.T) {
	var stdout bytes.Buffer
	cases := []struct {
		name  string
		shell string
	}{
		{name: "bash buffer", shell: "bash"},
		{name: "pwsh buffer", shell: "pwsh"},
		{name: "cmd buffer", shell: "cmd"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if envApplyShouldShowBanner(tc.shell, &stdout) {
				t.Fatalf("envApplyShouldShowBanner(%q, bytes.Buffer) = true, want false", tc.shell)
			}
		})
	}
}

func TestPrintEnvApplyFallbackBannerWrapperBypassed(t *testing.T) {
	oldColorsOn := colorsOn
	t.Cleanup(func() { colorsOn = oldColorsOn })
	colorsOn = true

	var out bytes.Buffer
	printEnvApplyFallbackBanner(&out, "Brand.Dev", true)
	got := out.String()

	for _, want := range []string{"╔", "╗", "╚", "╝", "NOT applied", "bypassed", "      pm env apply Brand.Dev\n"} {
		if !strings.Contains(got, want) {
			t.Fatalf("wrapper-bypassed banner missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "pm shell-init pwsh") {
		t.Fatalf("wrapper-bypassed banner should not include install guidance:\n%s", got)
	}
}

func TestPrintEnvApplyFallbackBannerNoWrapper(t *testing.T) {
	oldColorsOn := colorsOn
	t.Cleanup(func() { colorsOn = oldColorsOn })
	colorsOn = true

	var out bytes.Buffer
	printEnvApplyFallbackBanner(&out, "Brand.Dev", false)
	got := out.String()

	for _, want := range []string{
		"╔",
		"╗",
		"╚",
		"╝",
		"NOT applied",
		"pm env apply Brand.Dev | Invoke-Expression",
		"pm shell-init pwsh | Out-String | Invoke-Expression",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("no-wrapper banner missing %q:\n%s", want, got)
		}
	}
}

func TestPrintEnvApplyFallbackBannerASCIIWhenColorsOff(t *testing.T) {
	oldColorsOn := colorsOn
	t.Cleanup(func() { colorsOn = oldColorsOn })
	colorsOn = false

	var out bytes.Buffer
	printEnvApplyFallbackBanner(&out, "Brand.Dev", false)
	got := out.String()

	if !strings.Contains(got, "+") || !strings.Contains(got, "-") || !strings.Contains(got, "|") {
		t.Fatalf("ASCII fallback banner missing box characters:\n%s", got)
	}
	for _, unwanted := range []string{"╔", "╗", "╚", "╝", "═", "║"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("ASCII fallback banner contains box-drawing character %q:\n%s", unwanted, got)
		}
	}
}

// belt-and-braces: profile path round-trip works under withTempDirs.
func TestWriteProfileRoundtrip(t *testing.T) {
	dir := withTempDirs(t)
	path := writeProfile(t, &core.Profile{Name: "rt"})
	if !strings.HasPrefix(path, dir) {
		t.Fatalf("profile path %s should be under tempdir %s", path, dir)
	}
	rt, err := core.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if rt.Name != "rt" {
		t.Fatalf("name = %s", rt.Name)
	}
	_ = filepath.Dir(path)
}
