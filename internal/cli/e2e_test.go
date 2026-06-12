package cli

import (
	"bytes"
	"errors"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/bvorland/profilmanager/internal/core"
)

const (
	e2eTenantUUID       = "11111111-2222-3333-4444-555555555555"
	e2eSubscriptionUUID = "66666666-7777-8888-9999-000000000000"
)

func e2eEnv(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	t.Setenv("APPDATA", filepath.Join(tmp, "appdata"))
	t.Setenv("LOCALAPPDATA", filepath.Join(tmp, "localappdata"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "xdgcfg"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(tmp, "xdgstate"))
	t.Setenv("PM_SESSION_ID", "e2e-session-"+t.Name())
	t.Setenv("COPILOT_AGENT_SESSION_ID", "")
	t.Setenv("WT_SESSION", "")
	t.Setenv("NO_COLOR", "1")
	return tmp
}

func e2eWriteProfile(t *testing.T, p *core.Profile) string {
	t.Helper()
	if p.Schema == "" {
		p.Schema = core.SchemaVersion
	}
	path, err := core.ProfilePath(p.Name)
	if err != nil {
		t.Fatalf("ProfilePath(%q): %v", p.Name, err)
	}
	if err := p.Save(path); err != nil {
		t.Fatalf("Save(%q): %v", p.Name, err)
	}
	return path
}

func e2eRunRoot(args ...string) (string, string, error) {
	root := newRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetIn(strings.NewReader(""))
	root.SetArgs(args)
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

func TestE2E_NewProfile_NonTTY_Errors(t *testing.T) {
	t.Run("profile new refuses non-interactive stdin", func(t *testing.T) {
		e2eEnv(t)
		withNonTTYStdin(t)

		stdout, stderr, err := e2eRunRoot("profile", "new")
		if err == nil {
			t.Fatalf("expected non-TTY error, stdout=%s stderr=%s", stdout, stderr)
		}
		if !strings.Contains(stderr, "pm profile new requires an interactive terminal") ||
			!strings.Contains(stderr, "profile add") {
			t.Fatalf("unexpected error output:\nstdout=%s\nstderr=%s", stdout, stderr)
		}
	})
}

func TestE2E_ResolvePrefix_Unambiguous(t *testing.T) {
	t.Run("core resolver accepts unique profile prefixes", func(t *testing.T) {
		e2eEnv(t)
		e2eWriteProfile(t, &core.Profile{Name: "Contoso.Foo"})
		e2eWriteProfile(t, &core.Profile{Name: "Fabrikam.Bar"})

		got, err := core.ResolveProfileName("Con")
		if err != nil {
			t.Fatalf("ResolveProfileName(Con): %v", err)
		}
		if got != "Contoso.Foo" {
			t.Fatalf("ResolveProfileName(Con) = %q", got)
		}
		got, err = core.ResolveProfileName("Fab")
		if err != nil {
			t.Fatalf("ResolveProfileName(Fab): %v", err)
		}
		if got != "Fabrikam.Bar" {
			t.Fatalf("ResolveProfileName(Fab) = %q", got)
		}
	})
}

func TestE2E_ResolvePrefix_Ambiguous(t *testing.T) {
	t.Run("core resolver reports all ambiguous prefix candidates", func(t *testing.T) {
		e2eEnv(t)
		e2eWriteProfile(t, &core.Profile{Name: "Contoso.Foo"})
		e2eWriteProfile(t, &core.Profile{Name: "Contoso.Bar"})

		_, err := core.ResolveProfileName("Con")
		if !errors.Is(err, core.ErrAmbiguous) {
			t.Fatalf("got %v, want ErrAmbiguous", err)
		}
		if !strings.Contains(err.Error(), "Contoso.Foo") || !strings.Contains(err.Error(), "Contoso.Bar") {
			t.Fatalf("ambiguous error should list both candidates, got %v", err)
		}
	})
}

func TestE2E_DidYouMean_NoMatch(t *testing.T) {
	t.Run("fuzzy suggestions include close profile name", func(t *testing.T) {
		e2eEnv(t)
		e2eWriteProfile(t, &core.Profile{Name: "Contoso.Prod-Pilot"})

		suggestions := core.SuggestNames("Contso.Prod-Pilot", 3)
		for _, suggestion := range suggestions {
			if suggestion == "Contoso.Prod-Pilot" {
				return
			}
		}
		t.Fatalf("expected Contoso.Prod-Pilot suggestion, got %#v", suggestions)
	})
}

func TestE2E_Whoami_ActiveProfile(t *testing.T) {
	t.Run("whoami renders active profile banner from env", func(t *testing.T) {
		e2eEnv(t)
		t.Setenv("PATH", "")
		t.Setenv("PM_ACTIVE_PROFILE", "Contoso.Foo")

		cmd := newWhoamiCmd()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("whoami: %v\n%s", err, out.String())
		}
		if !strings.HasPrefix(out.String(), "── active profile: Contoso.Foo ──") {
			t.Fatalf("unexpected whoami output:\n%s", out.String())
		}
	})
}

func TestE2E_Whoami_NoActiveProfile(t *testing.T) {
	t.Run("whoami renders host config banner without active profile", func(t *testing.T) {
		e2eEnv(t)
		t.Setenv("PATH", "")
		t.Setenv("PM_ACTIVE_PROFILE", "")

		cmd := newWhoamiCmd()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("whoami: %v\n%s", err, out.String())
		}
		if !strings.HasPrefix(out.String(), "── active profile: (none — host config) ──") {
			t.Fatalf("unexpected whoami output:\n%s", out.String())
		}
	})
}

func TestE2E_Completion_PwshOutput(t *testing.T) {
	t.Run("pwsh completion emits cobra script", func(t *testing.T) {
		e2eEnv(t)

		stdout, stderr, err := e2eRunRoot("completion", "pwsh")
		if err != nil {
			t.Fatalf("completion pwsh: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
		}
		if strings.TrimSpace(stdout) == "" || !strings.Contains(stdout, "__pm_debug") {
			t.Fatalf("completion output missing cobra sentinel:\n%s", stdout)
		}
	})
}

func TestE2E_Picker_NonTTY_Errors(t *testing.T) {
	t.Run("env apply without profile refuses non-interactive picker", func(t *testing.T) {
		e2eEnv(t)
		withNonTTYStdin(t)
		e2eWriteProfile(t, &core.Profile{Name: "Contoso.Foo"})

		stdout, stderr, err := e2eRunRoot("env", "apply", "--shell", "bash")
		if err == nil {
			t.Fatalf("expected non-TTY error, stdout=%s stderr=%s", stdout, stderr)
		}
		if !strings.Contains(stderr, "stdin is not a TTY") {
			t.Fatalf("expected stdin TTY error, got:\nstdout=%s\nstderr=%s", stdout, stderr)
		}
	})
}

func TestE2E_ProfileNew_NoLogin_SkipsLoginPrompt(t *testing.T) {
	t.Run("profile new full wizard saves profile without login prompt", func(t *testing.T) {
		home := e2eEnv(t)
		stdin := strings.Join([]string{
			"Contoso.Foo",
			"Contoso Foo",
			"Blue",
			"full-devops",
			e2eTenantUUID,
			e2eSubscriptionUUID,
			"",
			"",
			"bvorland",
			"github.com",
			"aks-contoso-foo",
			"default",
			"Bjørn Vorland",
			"bvorland@example.com",
			"Y",
		}, "\n") + "\n"

		stdout, err := runProfileNewTest(t, stdin, nil, profileNewOpts{NoTemplate: true, NoLogin: true})
		if err != nil {
			t.Fatalf("profile new --no-login: %v\n%s", err, stdout)
		}
		if strings.Contains(stdout, "Run first-time sign-in") {
			t.Fatalf("--no-login should skip first-time sign-in prompt:\n%s", stdout)
		}

		p, err := core.Load(mustProfilePath(t, "Contoso.Foo"))
		if err != nil {
			t.Fatal(err)
		}
		if p.Name != "Contoso.Foo" || p.Label != "🔷 Contoso Foo" || p.Color != "Blue" {
			t.Fatalf("top-level mismatch: %+v", p)
		}
		if p.Azure == nil || p.Azure.TenantID != e2eTenantUUID || p.Azure.SubscriptionID != e2eSubscriptionUUID {
			t.Fatalf("azure mismatch: %+v", p.Azure)
		}
		if p.Azd == nil || !strings.HasSuffix(p.Azd.ConfigDir, ".azd-Contoso.Foo") {
			t.Fatalf("azd sandbox mismatch: %+v", p.Azd)
		}
		if p.GitHub == nil || p.GitHub.Account != "bvorland" || p.Kube == nil || p.Git == nil {
			t.Fatalf("full-devops blocks missing: %+v", p)
		}
		if !strings.HasPrefix(p.Azure.ConfigDir, home) {
			t.Fatalf("azure config dir %q should live under temp home %q", p.Azure.ConfigDir, home)
		}
	})
}

func TestE2E_ProfileNew_FromTemplate(t *testing.T) {
	t.Run("profile new from template copies shared fields and resandboxes dirs", func(t *testing.T) {
		home := e2eEnv(t)
		e2eWriteProfile(t, &core.Profile{
			Name:  "Contoso.Foo",
			Label: "Contoso Foo",
			Color: "Cyan",
			Azure: &core.AzureProfile{
				TenantID:       e2eTenantUUID,
				SubscriptionID: e2eSubscriptionUUID,
				ConfigDir:      filepath.Join(home, ".azure-Contoso.Foo"),
			},
			Azd: &core.AzdProfile{
				SubscriptionID: e2eSubscriptionUUID,
				ConfigDir:      filepath.Join(home, ".azd-Contoso.Foo"),
			},
		})

		stdout, err := runProfileNewTest(t, "Y\n", []string{"Contoso.Bar"}, profileNewOpts{From: "Contoso.Foo", NoLogin: true})
		if err != nil {
			t.Fatalf("profile new --from --no-login: %v\n%s", err, stdout)
		}

		p, err := core.Load(mustProfilePath(t, "Contoso.Bar"))
		if err != nil {
			t.Fatal(err)
		}
		if p.Name != "Contoso.Bar" || p.Label != "🔵 Contoso.Bar" {
			t.Fatalf("name/label not reset: %+v", p)
		}
		if p.Azure == nil || p.Azure.TenantID != e2eTenantUUID || p.Azure.SubscriptionID != e2eSubscriptionUUID {
			t.Fatalf("azure fields not copied: %+v", p.Azure)
		}
		if p.Azure.ConfigDir != filepath.Join(home, ".azure-Contoso.Bar") ||
			strings.Contains(p.Azure.ConfigDir, "Contoso.Foo") {
			t.Fatalf("azure config dir not resandboxed: %q", p.Azure.ConfigDir)
		}
		if p.Azd == nil || p.Azd.ConfigDir != filepath.Join(home, ".azd-Contoso.Bar") ||
			strings.Contains(p.Azd.ConfigDir, "Contoso.Foo") {
			t.Fatalf("azd config dir not resandboxed: %+v", p.Azd)
		}
	})
}

func TestE2E_ProfileNew_Apply_WithShellInit_EmitsMarker(t *testing.T) {
	e2eEnv(t)
	t.Setenv("PM_SHELL_INIT_LOADED", "1")
	writeProfileNewTemplate(t, "Template.E2EApply")

	name := "Brand.E2EApply"
	stdout, stderr, err := runProfileNewTestStreams(t, "Y\n", []string{name}, profileNewOpts{
		From:    "Template.E2EApply",
		NoLogin: true,
		Apply:   true,
	}, false)
	if err != nil {
		t.Fatalf("profile new --apply: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	if !regexp.MustCompile(`^##pm-apply:` + regexp.QuoteMeta(name) + `$`).MatchString(last) {
		t.Fatalf("last non-empty stdout line = %q, stdout:\n%s", last, stdout)
	}
}

func TestE2E_ProfileNew_Apply_NoShellInit_FallbackHint(t *testing.T) {
	e2eEnv(t)
	t.Setenv("PM_SHELL_INIT_LOADED", "")
	writeProfileNewTemplate(t, "Template.E2EApplyFallback")

	stdout, stderr, err := runProfileNewTestStreams(t, "Y\n", []string{"Brand.E2EApplyFallback"}, profileNewOpts{
		From:    "Template.E2EApplyFallback",
		NoLogin: true,
		Apply:   true,
	}, false)
	if err != nil {
		t.Fatalf("profile new --apply without shell-init: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	for _, want := range []string{
		"Profile saved",
		"NOT applied",
		"pm env apply Brand.E2EApplyFallback | Invoke-Expression",
		"pm shell-init pwsh | Out-String | Invoke-Expression",
	} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr missing %q:\n%s", want, stderr)
		}
	}
	if strings.Contains(stdout, "##pm-apply:") {
		t.Fatalf("stdout should not contain apply marker without shell-init:\n%s", stdout)
	}
}

func TestE2E_ProfileNew_NoApply_NoMarker(t *testing.T) {
	e2eEnv(t)
	t.Setenv("PM_SHELL_INIT_LOADED", "1")
	writeProfileNewTemplate(t, "Template.E2ENoApply")

	stdout, stderr, err := runProfileNewTestStreams(t, "Y\n", []string{"Brand.E2ENoApply"}, profileNewOpts{
		From:    "Template.E2ENoApply",
		NoLogin: true,
		NoApply: true,
	}, false)
	if err != nil {
		t.Fatalf("profile new --no-apply: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if strings.Contains(stdout, "##pm-apply:") {
		t.Fatalf("stdout should not contain apply marker with --no-apply:\n%s", stdout)
	}
	if !strings.Contains(stderr, "Apply later with") {
		t.Fatalf("stderr missing deferred apply hint:\n%s", stderr)
	}
}

func TestE2E_ProfileNew_ApplyAndNoApply_Mutex(t *testing.T) {
	e2eEnv(t)

	_, _, err := runProfileNewTestStreams(t, "", nil, profileNewOpts{Apply: true, NoApply: true}, false)
	if err == nil {
		t.Fatal("expected --apply and --no-apply to error")
	}
	for _, want := range []string{"--apply", "--no-apply", "mutually exclusive"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error missing %q: %v", want, err)
		}
	}
}

func TestE2E_Doctor_AgentContext_NoProfile_Warns(t *testing.T) {
	e2eEnv(t)
	e2eClearAgentContext(t)
	t.Setenv("PM_SESSION_ID", "test-session-id")
	t.Setenv("PM_ACTIVE_PROFILE", "")

	stdout, stderr, err := runCLI(t, "doctor")
	if err != nil {
		t.Fatalf("doctor: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	for _, want := range []string{"agent-context-has-profile", "WARN", "PM_SESSION_ID"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, stdout)
		}
	}
}

func TestE2E_Doctor_AgentContext_WithProfile_OK(t *testing.T) {
	e2eEnv(t)
	e2eClearAgentContext(t)
	t.Setenv("PM_SESSION_ID", "test-session-id")
	t.Setenv("PM_ACTIVE_PROFILE", "Contoso.Foo")

	stdout, stderr, err := runCLI(t, "doctor")
	if err != nil {
		t.Fatalf("doctor: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	for _, want := range []string{"agent-context-has-profile", "OK", "Contoso.Foo"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, stdout)
		}
	}
}

func TestE2E_Doctor_NotInAgent_Skips(t *testing.T) {
	e2eEnv(t)
	e2eClearAgentContext(t)

	stdout, stderr, err := runCLI(t, "doctor")
	if err != nil {
		t.Fatalf("doctor: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	for _, want := range []string{"agent-context-has-profile", "OK", "not in agent context"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, stdout)
		}
	}
}

func TestE2E_Doctor_ShellInitWrapper_JSONIncludesCheck(t *testing.T) {
	e2eEnv(t)

	stdout, stderr, err := runCLI(t, "doctor", "--json")
	if err != nil && CodeFor(err) != ExitError {
		t.Fatalf("doctor --json: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "shell-init-wrapper-loaded") {
		t.Fatalf("doctor JSON missing shell-init-wrapper-loaded check:\n%s", stdout)
	}
}

func TestE2E_Dashboard_AgentContext_NoProfile_ShowsBanner(t *testing.T) {
	e2eEnv(t)
	e2eClearAgentContext(t)
	t.Setenv("COPILOT_SESSION_ID", "x")
	t.Setenv("PM_ACTIVE_PROFILE", "")

	stdout, stderr, err := runCLI(t)
	if err != nil {
		t.Fatalf("bare pm: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	for _, want := range []string{"Inside an AI agent", "COPILOT_SESSION_ID", "pm env apply"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("dashboard output missing %q:\n%s", want, stdout)
		}
	}
}

func TestE2E_Dashboard_AgentContext_WithProfile_NoBanner(t *testing.T) {
	e2eEnv(t)
	e2eClearAgentContext(t)
	t.Setenv("COPILOT_SESSION_ID", "x")
	t.Setenv("PM_ACTIVE_PROFILE", "Contoso.Foo")

	stdout, stderr, err := runCLI(t)
	if err != nil {
		t.Fatalf("bare pm: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if strings.Contains(stdout, "Inside an AI agent") {
		t.Fatalf("dashboard should not warn with active profile:\n%s", stdout)
	}
}

func TestE2E_Copilot_NonTTY_NoArg_Errors(t *testing.T) {
	e2eEnv(t)
	withNonTTYStdin(t)

	stdout, stderr, err := runCLI(t, "copilot")
	if err == nil {
		t.Fatalf("expected non-TTY copilot picker error, stdout=%s stderr=%s", stdout, stderr)
	}
	if !strings.Contains(stderr, "stdin is not a TTY") && !strings.Contains(err.Error(), "stdin is not a TTY") {
		t.Fatalf("expected stdin TTY error, got err=%v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
}

func TestE2E_Copilot_ResolvesViaPrefix(t *testing.T) {
	e2eEnv(t)
	e2eWriteProfile(t, &core.Profile{Name: "Contoso.Prod-Pilot"})

	// `pm copilot` delegates profile selection to the shared resolver before
	// execing the external copilot binary, so avoid launching copilot in CI.
	got, err := core.ResolveProfileName("Contoso.Prod")
	if err != nil {
		t.Fatalf("ResolveProfileName: %v", err)
	}
	if got != "Contoso.Prod-Pilot" {
		t.Fatalf("ResolveProfileName = %q", got)
	}
}

func TestE2E_ShellInit_Pwsh_ContainsMarkerRegex(t *testing.T) {
	e2eEnv(t)

	stdout, stderr, err := runCLI(t, "shell-init", "pwsh")
	if err != nil {
		t.Fatalf("shell-init pwsh: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	for _, want := range []string{
		"PM_SHELL_INIT_LOADED",
		"function global:pm",
		"##pm-apply:",
		"pm env apply",
		// v0.7: env-apply auto-apply branch
		"$args[0] -eq 'env' -and $args[1] -eq 'apply'",
		"& $exe @args | Invoke-Expression",
		"'--help'",
		"'--shell'",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("shell-init output missing %q:\n%s", want, stdout)
		}
	}
}

// v0.7: `pm env apply <name>` emits the pwsh script + no marker (wrapper
// handles auto-apply by piping stdout directly; no marker pattern needed).
// Banner is suppressed because stdout is captured to bytes.Buffer (not a TTY).
func TestE2E_EnvApply_Pwsh_NoMarker_NoBannerOnNonTTY(t *testing.T) {
	e2eEnv(t)
	t.Setenv("PM_SHELL_INIT_LOADED", "1")
	e2eWriteProfile(t, &core.Profile{Name: "Brand.E2EEnvApplyPwsh"})

	stdout, stderr, err := runCLI(t, "env", "apply", "Brand.E2EEnvApplyPwsh", "--shell", "pwsh")
	if err != nil {
		t.Fatalf("env apply: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if strings.Contains(stdout, "##pm-apply:") {
		t.Fatalf("env apply must not emit profile-new marker:\n%s", stdout)
	}
	for _, want := range []string{
		"# pm env apply Brand.E2EEnvApplyPwsh",
		"$env:PM_ACTIVE_PROFILE = 'Brand.E2EEnvApplyPwsh'",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("env apply output missing %q:\n%s", want, stdout)
		}
	}
	// Captured stdout (bytes.Buffer) is not a TTY → banner suppressed on stderr.
	if strings.Contains(stderr, "NOT applied") {
		t.Fatalf("banner should be suppressed on non-TTY stdout, stderr:\n%s", stderr)
	}
}

func TestE2E_EnvApply_EmitsProfileEmojiEnv(t *testing.T) {
	e2eEnv(t)
	e2eWriteProfile(t, &core.Profile{Name: "Brand.E2EEmoji", Color: "Cyan"})

	stdout, _, err := runCLI(t, "env", "apply", "Brand.E2EEmoji", "--shell", "pwsh")
	if err != nil {
		t.Fatalf("env apply: %v", err)
	}

	if !strings.Contains(stdout, "$env:PM_ACTIVE_PROFILE_EMOJI = '🔵'") {
		t.Fatalf("missing emoji env var line:\n%s", stdout)
	}
	if !strings.Contains(stdout, "$env:PM_ACTIVE_PROFILE = 'Brand.E2EEmoji'") {
		t.Fatalf("missing profile env var line:\n%s", stdout)
	}
	if !strings.Contains(stdout, "$env:PM_ACTIVE_PROFILE_BG = '"+core.ColorHex("Cyan")+"'") {
		t.Fatalf("missing PM_ACTIVE_PROFILE_BG env var line:\n%s", stdout)
	}
}

func TestE2E_EnvApply_NoColor_EmitsEmptyEmoji(t *testing.T) {
	e2eEnv(t)
	e2eWriteProfile(t, &core.Profile{Name: "Brand.E2ENoColor"})

	stdout, _, err := runCLI(t, "env", "apply", "Brand.E2ENoColor", "--shell", "pwsh")
	if err != nil {
		t.Fatalf("env apply: %v", err)
	}

	if !strings.Contains(stdout, "$env:PM_ACTIVE_PROFILE_EMOJI = ''") {
		t.Fatalf("uncolored profile should set empty emoji:\n%s", stdout)
	}
	if !strings.Contains(stdout, "$env:PM_ACTIVE_PROFILE_BG = ''") {
		t.Fatalf("uncolored profile should set empty bg:\n%s", stdout)
	}
}

func TestE2E_EnvApply_Unset_ClearsEmoji(t *testing.T) {
	e2eEnv(t)
	e2eWriteProfile(t, &core.Profile{Name: "Brand.E2EUnsetEmoji", Color: "Green"})

	if _, _, err := runCLI(t, "env", "apply", "Brand.E2EUnsetEmoji", "--shell", "pwsh"); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	stdout, _, err := runCLI(t, "env", "apply", "--unset", "--shell", "pwsh")
	if err != nil {
		t.Fatalf("unset: %v", err)
	}
	if !strings.Contains(stdout, "Remove-Item -ErrorAction SilentlyContinue env:PM_ACTIVE_PROFILE_EMOJI") {
		t.Fatalf("unset should clear PM_ACTIVE_PROFILE_EMOJI:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Remove-Item -ErrorAction SilentlyContinue env:PM_ACTIVE_PROFILE_BG") {
		t.Fatalf("unset should clear PM_ACTIVE_PROFILE_BG:\n%s", stdout)
	}
}

func e2eClearAgentContext(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"PM_SESSION_ID",
		"COPILOT_SESSION_ID",
		"COPILOT_CLI_SESSION_ID",
		"CLAUDE_SESSION_ID",
		"AIDER_SESSION_ID",
	} {
		t.Setenv(key, "")
	}
}
