package cli

import (
	"strings"
	"testing"
)

func TestCompletionCommandEmitsPowerShellScript(t *testing.T) {
	testEnv(t)
	stdout, _, err := runCLI(t, "completion", "pwsh")
	if err != nil {
		t.Fatalf("completion pwsh: %v", err)
	}
	if !strings.Contains(stdout, "Register-ArgumentCompleter") {
		t.Fatalf("PowerShell completion missing Register-ArgumentCompleter:\n%s", stdout)
	}
	if !strings.Contains(stdout, "__pm_debug") {
		t.Fatalf("PowerShell completion does not look like cobra output:\n%s", stdout)
	}
}

func TestProfileNameArgCompletion(t *testing.T) {
	testEnv(t)
	_, _, err := runCLI(t, "profile", "add", "alpha", "--color", "cyan")
	if err != nil {
		t.Fatalf("add alpha: %v", err)
	}
	_, _, err = runCLI(t, "profile", "add", "bravo", "--color", "magenta")
	if err != nil {
		t.Fatalf("add bravo: %v", err)
	}

	stdout, _, err := runCLI(t, "__complete", "profile", "show", "")
	if err != nil {
		t.Fatalf("__complete profile show: %v stdout=%s", err, stdout)
	}
	for _, want := range []string{"alpha", "bravo", ":4"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("completion missing %q:\n%s", want, stdout)
		}
	}

	stdout, _, err = runCLI(t, "__complete", "exec", "")
	if err != nil {
		t.Fatalf("__complete exec: %v stdout=%s", err, stdout)
	}
	for _, want := range []string{"alpha", "bravo", ":4"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("exec completion missing %q:\n%s", want, stdout)
		}
	}
}
