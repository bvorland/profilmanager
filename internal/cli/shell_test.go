package cli

import (
	"runtime"
	"strings"
	"testing"
)

func TestPickInteractiveShellHonorsPMSHELL(t *testing.T) {
	t.Setenv("PMSHELL", "/custom/sh")
	t.Setenv("SHELL", "/should/not/win")
	if got := pickInteractiveShell(); got != "/custom/sh" {
		t.Fatalf("PMSHELL = %q, want /custom/sh", got)
	}
}

func TestPickInteractiveShellHonorsSHELL(t *testing.T) {
	t.Setenv("PMSHELL", "")
	t.Setenv("SHELL", "/usr/bin/zsh")
	if got := pickInteractiveShell(); got != "/usr/bin/zsh" {
		t.Fatalf("SHELL = %q, want /usr/bin/zsh", got)
	}
}

func TestShellFlavorMappings(t *testing.T) {
	cases := map[string]string{
		"/usr/bin/bash":                "bash",
		`C:\Program Files\zsh\zsh.exe`: "zsh",
		"/usr/local/bin/fish":          "fish",
		"pwsh.exe":                     "pwsh",
		"powershell":                   "pwsh",
		"/bin/sh":                      "bash",
		"cmd.exe":                      "cmd",
		"weird":                        "",
	}
	for in, want := range cases {
		if got := shellFlavor(in); got != want {
			t.Errorf("shellFlavor(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestChildShellArgsPwsh(t *testing.T) {
	args, ok := childShellArgs("pwsh", "demo", true)
	if !ok {
		t.Fatalf("pwsh should yield args")
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-NoExit") {
		t.Fatalf("missing -NoExit: %s", joined)
	}
	if !strings.Contains(joined, "[pm:demo]") {
		t.Fatalf("prompt marker missing: %s", joined)
	}
}

func TestChildShellArgsBashNoExtras(t *testing.T) {
	_, ok := childShellArgs("bash", "demo", true)
	if ok {
		t.Fatalf("bash should not need extra args (uses PS1 env)")
	}
}

func TestPromptEnvBash(t *testing.T) {
	env := promptEnv("bash", "demo", true)
	ps1, ok := env["PS1"]
	if !ok {
		t.Fatalf("missing PS1")
	}
	if !strings.Contains(ps1, "[pm:demo]") {
		t.Fatalf("PS1 missing marker: %s", ps1)
	}
}

func TestPromptEnvFish(t *testing.T) {
	env := promptEnv("fish", "demo", true)
	if env["PM_PROMPT_TAG"] != "[pm:demo]" {
		t.Fatalf("fish PM_PROMPT_TAG = %q", env["PM_PROMPT_TAG"])
	}
}

func TestPromptEnvOmittedWhenDisabled(t *testing.T) {
	if env := promptEnv("bash", "demo", false); env != nil {
		t.Fatalf("expected nil env when prompt disabled, got %v", env)
	}
}

func TestPowershellSingleQuoteEscape(t *testing.T) {
	if got := powershellSingleQuoteEscape("a'b"); got != "a''b" {
		t.Fatalf("got %q", got)
	}
	if got := powershellSingleQuoteEscape("none"); got != "none" {
		t.Fatalf("got %q", got)
	}
}

// On Windows, pickInteractiveShell should fall through to pwsh /
// powershell / cmd if neither env var is set. We can't reliably check
// which one wins without poking PATH, but we can at least ensure the
// function returns *something* on a typical CI Windows runner.
func TestPickInteractiveShellWindowsFallback(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only fallback")
	}
	t.Setenv("PMSHELL", "")
	t.Setenv("SHELL", "")
	got := pickInteractiveShell()
	if got == "" {
		t.Skip("no pwsh/powershell/cmd on this Windows runner; nothing to assert")
	}
	// Just sanity: returned an executable that looks like a shell.
	low := strings.ToLower(got)
	if !(strings.Contains(low, "pwsh") || strings.Contains(low, "powershell") || strings.Contains(low, "cmd")) {
		t.Fatalf("unexpected fallback shell: %s", got)
	}
}
