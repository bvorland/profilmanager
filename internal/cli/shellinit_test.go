package cli

import (
	"bytes"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

// TestShellInitBashShimSyntax pipes the bash shim through `bash -n`
// (syntax-check only, no execution). Skipped if bash isn't on PATH —
// most Windows CI runners have Git-for-Windows bash available so this
// usually runs anyway.
func TestShellInitBashShimSyntax(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not on PATH; cannot syntax-check bash shim")
	}
	out := runShellInitToBuffer(t, "bash", true)
	checkSyntax(t, bash, []string{"-n"}, out)
}

// TestShellInitZshShimSyntax — same idea via `zsh -n`. Usually skipped
// on Windows runners.
func TestShellInitZshShimSyntax(t *testing.T) {
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh not on PATH")
	}
	// shell-init emits the same code for bash and zsh.
	out := runShellInitToBuffer(t, "zsh", true)
	checkSyntax(t, zsh, []string{"-n"}, out)
}

// TestShellInitFishShimSyntax — `fish --no-execute` parses without
// running.
func TestShellInitFishShimSyntax(t *testing.T) {
	fish, err := exec.LookPath("fish")
	if err != nil {
		t.Skip("fish not on PATH")
	}
	out := runShellInitToBuffer(t, "fish", true)
	checkSyntax(t, fish, []string{"--no-execute"}, out)
}

// TestShellInitPwshShimSyntax — PowerShell can parse a script via
// [scriptblock]::Create($s); it throws on syntax errors. We pipe the
// shim into a tiny -Command harness.
func TestShellInitPwshShimSyntax(t *testing.T) {
	pwsh, err := exec.LookPath("pwsh")
	if err != nil {
		pwsh, err = exec.LookPath("powershell")
		if err != nil {
			t.Skip("pwsh/powershell not on PATH")
		}
	}
	out := runShellInitToBuffer(t, "pwsh", true)
	// Use stdin via [scriptblock]::Create((Get-Content -Raw -Path -)) —
	// but simpler: pipe to pwsh -NoProfile -Command "[scriptblock]::Create($input -join "`n") | Out-Null"
	cmd := exec.Command(pwsh, "-NoProfile", "-NonInteractive", "-Command",
		`$src = [Console]::In.ReadToEnd(); $null = [scriptblock]::Create($src)`)
	cmd.Stdin = bytes.NewReader(out)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("pwsh syntax check failed: %v\nstderr: %s\nshim output was:\n%s", err, stderr.String(), out)
	}
}

// runShellInitToBuffer invokes the `pm shell-init` command tree
// in-process and returns the produced shell snippet.
func runShellInitToBuffer(t *testing.T, shell string, withShims bool) []byte {
	t.Helper()
	root := newRootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	args := []string{"shell-init", "--shell", shell}
	if withShims {
		args = append(args, "--with-shims")
	}
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		t.Fatalf("shell-init %s: %v\noutput:\n%s", shell, err, buf.String())
	}
	return buf.Bytes()
}

// checkSyntax shells out to `tool args... < input` and fails the test
// if the tool exits non-zero.
func checkSyntax(t *testing.T, tool string, args []string, input []byte) {
	t.Helper()
	cmd := exec.Command(tool, args...)
	cmd.Stdin = bytes.NewReader(input)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s %v syntax check failed: %v\noutput:\n%s\nscript was:\n%s",
			tool, args, err, out.String(), input)
	}
}

// TestBashShimUsesCommandKeywordToBreakRecursion is a static check on
// the shim string — the `command` prefix is load-bearing (without it, a
// bashShim for `az` would recurse forever). If someone refactors this
// to drop `command`, the test catches it.
func TestBashShimUsesCommandKeywordToBreakRecursion(t *testing.T) {
	got := bashShim("az")
	if !strings.Contains(got, "command az") {
		t.Fatalf("bash shim must use `command az` to break recursion:\n%s", got)
	}
	if !strings.Contains(got, "command pm") {
		t.Fatalf("bash shim must use `command pm` so a pm function (if any) doesn't recurse:\n%s", got)
	}
}

func TestFishShimUsesCommandKeyword(t *testing.T) {
	got := fishShim("az")
	if !strings.Contains(got, "command az") {
		t.Fatalf("fish shim must use `command az`:\n%s", got)
	}
}

// TestPwshShimUsesGetCommandApplication is the equivalent for pwsh —
// without -CommandType Application, Get-Command finds our function and
// the shim recurses.
func TestPwshShimUsesGetCommandApplication(t *testing.T) {
	got := pwshShim("az")
	if !strings.Contains(got, "Get-Command -CommandType Application az") {
		t.Fatalf("pwsh shim must use Get-Command -CommandType Application to break recursion:\n%s", got)
	}
}

func TestShellInitPwshIncludesProfileNewAutoApplyWrapper(t *testing.T) {
	out := string(runShellInitToBuffer(t, "pwsh", false))
	for _, want := range []string{
		"[Console]::OutputEncoding",
		"$OutputEncoding",
		"CodePage -ne 65001",
		"$env:PM_SHELL_INIT_LOADED = '1'",
		"function global:pm {",
		"##pm-apply:",
		"pm env apply",
		"$args[0] -eq 'env' -and $args[1] -eq 'apply'",
		"Invoke-Expression",
		"'--help'",
		"'-h'",
		"'--shell'",
		"'pwsh'",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("pwsh shell-init missing %q:\n%s", want, out)
		}
	}
}

func TestShellInitAcceptsPositionalShell(t *testing.T) {
	root := newRootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"shell-init", "pwsh"})
	if err := root.Execute(); err != nil {
		t.Fatalf("shell-init pwsh: %v\n%s", err, buf.String())
	}
	if !strings.Contains(buf.String(), "function global:pm {") {
		t.Fatalf("positional pwsh shell-init missing wrapper:\n%s", buf.String())
	}
}

// On Windows-only runners we usually only have pwsh. This keeps a
// build-time check that the function compiles on all OSes.
var _ = runtime.GOOS
