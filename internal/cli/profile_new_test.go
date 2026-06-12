package cli

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bvorland/profilmanager/internal/core"
	"github.com/bvorland/profilmanager/internal/wizard"
)

const profileNewUUID = "11111111-2222-3333-4444-555555555555"

func runProfileNewTest(t *testing.T, stdin string, args []string, opts profileNewOpts) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := runProfileNewWizard(&out, &out, strings.NewReader(stdin), args, opts, false)
	return out.String(), err
}

func runProfileNewTestStreams(t *testing.T, stdin string, args []string, opts profileNewOpts, interactive bool) (string, string, error) {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runProfileNewWizard(&stdout, &stderr, strings.NewReader(stdin), args, opts, interactive)
	return stdout.String(), stderr.String(), err
}

func stubProfileNewLogin(t *testing.T, fn func(profile *core.Profile, command string, args []string, stdin io.Reader, stdout, stderr io.Writer) (int, error)) {
	t.Helper()
	old := profileNewLoginRunner
	profileNewLoginRunner = fn
	t.Cleanup(func() { profileNewLoginRunner = old })
}

func TestProfileNewHappyPath(t *testing.T) {
	testEnv(t)
	stdin := strings.Join([]string{
		"Contoso.Foo",
		"",
		"Blue",
		"azure-azd",
		profileNewUUID,
		"",
		"",
		"",
		"Y",
		"n",
	}, "\n") + "\n"
	stdout, err := runProfileNewTest(t, stdin, nil, profileNewOpts{NoTemplate: true})
	if err != nil {
		t.Fatalf("profile new: %v\n%s", err, stdout)
	}
	path, err := core.ProfilePath("Contoso.Foo")
	if err != nil {
		t.Fatal(err)
	}
	p, err := core.Load(path)
	if err != nil {
		t.Fatalf("Load(%s): %v", path, err)
	}
	if p.Name != "Contoso.Foo" || p.Label != "🔷 Contoso.Foo" || p.Color != "Blue" {
		t.Fatalf("top-level mismatch: %+v", p)
	}
	if p.Azure == nil || p.Azure.TenantID != profileNewUUID || p.Azd == nil {
		t.Fatalf("provider blocks mismatch: %+v", p)
	}
	if !strings.Contains(stdout, "✓ Saved profile to") || !strings.Contains(stdout, "schema = '1'") || !strings.Contains(stdout, "label = '🔷 Contoso.Foo'") {
		t.Fatalf("stdout missing preview/created: %s", stdout)
	}
}

func TestProfileNewFromTemplate(t *testing.T) {
	home := testEnv(t)
	template := &core.Profile{
		Schema: core.SchemaVersion,
		Name:   "Contoso.MainDev",
		Label:  "Main",
		Color:  "Cyan",
		Azure: &core.AzureProfile{
			TenantID:       profileNewUUID,
			SubscriptionID: "66666666-7777-8888-9999-000000000000",
			ConfigDir:      filepath.Join(home, ".azure-Contoso.MainDev"),
		},
		Azd: &core.AzdProfile{ConfigDir: filepath.Join(home, ".azd-Contoso.MainDev")},
	}
	templatePath, err := core.ProfilePath(template.Name)
	if err != nil {
		t.Fatal(err)
	}
	if err := template.Save(templatePath); err != nil {
		t.Fatal(err)
	}

	stdout, err := runProfileNewTest(t, "Y\nn\n", []string{"Contoso.NewProj"}, profileNewOpts{From: "Contoso.MainDev"})
	if err != nil {
		t.Fatalf("profile new --from: %v\n%s", err, stdout)
	}
	p, err := core.Load(mustProfilePath(t, "Contoso.NewProj"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Color != "Cyan" || p.Azure == nil || !strings.Contains(p.Azure.ConfigDir, "Contoso.NewProj") {
		t.Fatalf("template fields not copied/substituted: %+v", p)
	}
	if p.Name != "Contoso.NewProj" || p.Label != "🔵 Contoso.NewProj" {
		t.Fatalf("template name/label not reset: %+v", p)
	}
}

func TestProfileNewValidationRetry(t *testing.T) {
	testEnv(t)
	stdin := strings.Join([]string{
		"bad name",
		"Brand.Foo",
		"",
		"Cyan",
		"azure-only",
		"not-a-uuid",
		profileNewUUID,
		"",
		"",
		"Y",
		"n",
	}, "\n") + "\n"
	stdout, err := runProfileNewTest(t, stdin, nil, profileNewOpts{NoTemplate: true})
	if err != nil {
		t.Fatalf("profile new retry: %v\n%s", err, stdout)
	}
	if !strings.Contains(stdout, "✗") {
		t.Fatalf("stdout missing validation error marker: %s", stdout)
	}
	if _, err := os.Stat(mustProfilePath(t, "Brand.Foo")); err != nil {
		t.Fatalf("expected profile after retry: %v", err)
	}
}

func TestProfileNewFirstLoginPromptSkippedWithoutTenant(t *testing.T) {
	testEnv(t)
	profile := &core.Profile{Schema: core.SchemaVersion, Name: "No.Azure"}
	state := wizardStateNoAzure()
	var out bytes.Buffer
	err := maybePromptFirstLogin(&out, scriptedWizardReader(""), profile, &state, profileNewOpts{})
	if err != nil {
		t.Fatalf("maybePromptFirstLogin: %v", err)
	}
	if strings.Contains(out.String(), "Run first-time sign-in") {
		t.Fatalf("first-login prompt should be skipped without tenant/subscription:\n%s", out.String())
	}
}

func TestProfileNewFirstLoginPromptAppearsWithTenant(t *testing.T) {
	testEnv(t)
	called := false
	stubProfileNewLogin(t, func(profile *core.Profile, command string, args []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
		called = true
		if profile.Name != "Tenant.Profile" || command != "az" || strings.Join(args, " ") != "login --use-device-code" {
			t.Fatalf("unexpected login invocation: profile=%s command=%s args=%v", profile.Name, command, args)
		}
		if profile.Azure == nil || profile.Azure.ConfigDir == "" {
			t.Fatalf("login runner did not receive profile sandbox fields: %+v", profile)
		}
		return 0, nil
	})
	profile := &core.Profile{
		Schema: core.SchemaVersion,
		Name:   "Tenant.Profile",
		Azure:  &core.AzureProfile{TenantID: profileNewUUID, ConfigDir: "sandbox"},
	}
	state := wizardStateWithTenant()
	var out bytes.Buffer
	if err := maybePromptFirstLogin(&out, scriptedWizardReader("\n"), profile, &state, profileNewOpts{}); err != nil {
		t.Fatalf("maybePromptFirstLogin: %v", err)
	}
	if !strings.Contains(out.String(), "Run first-time sign-in now?") {
		t.Fatalf("first-login prompt missing:\n%s", out.String())
	}
	if !called {
		t.Fatal("expected default login runner to be called")
	}
}

func TestProfileNewNoLoginSkipsPromptWithTenant(t *testing.T) {
	testEnv(t)
	stubProfileNewLogin(t, func(profile *core.Profile, command string, args []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
		t.Fatal("login runner should not be called with --no-login")
		return 1, nil
	})
	profile := &core.Profile{
		Schema: core.SchemaVersion,
		Name:   "Tenant.Profile",
		Azure:  &core.AzureProfile{TenantID: profileNewUUID, ConfigDir: "sandbox"},
	}
	state := wizardStateWithTenant()
	var out bytes.Buffer
	if err := maybePromptFirstLogin(&out, scriptedWizardReader(""), profile, &state, profileNewOpts{NoLogin: true}); err != nil {
		t.Fatalf("maybePromptFirstLogin: %v", err)
	}
	if strings.Contains(out.String(), "Run first-time sign-in") {
		t.Fatalf("--no-login should skip prompt:\n%s", out.String())
	}
}

func TestProfileNewApplyFlagEmitsFinalMarkerWhenShellInitLoaded(t *testing.T) {
	testEnv(t)
	t.Setenv("PM_SHELL_INIT_LOADED", "1")
	writeProfileNewTemplate(t, "Template.ApplyFlag")

	stdout, stderr, err := runProfileNewTestStreams(t, "Y\n", []string{"Brand.ApplyFlag"}, profileNewOpts{
		From:    "Template.ApplyFlag",
		NoLogin: true,
		Apply:   true,
	}, false)
	if err != nil {
		t.Fatalf("profile new --apply: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	assertProfileApplyMarkerFinal(t, stdout, "Brand.ApplyFlag")
}

func TestProfileNewApplyFlagWithoutShellInitPrintsFallback(t *testing.T) {
	testEnv(t)
	t.Setenv("PM_SHELL_INIT_LOADED", "")
	writeProfileNewTemplate(t, "Template.ApplyNoShellInit")

	stdout, stderr, err := runProfileNewTestStreams(t, "Y\n", []string{"Brand.ApplyNoShellInit"}, profileNewOpts{
		From:    "Template.ApplyNoShellInit",
		NoLogin: true,
		Apply:   true,
	}, false)
	if err != nil {
		t.Fatalf("profile new --apply without shell-init: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if !strings.Contains(stderr, "Profile saved") || !strings.Contains(stderr, "NOT applied") {
		t.Fatalf("stderr missing loud fallback banner:\n%s", stderr)
	}
	if !strings.Contains(stderr, "pm env apply Brand.ApplyNoShellInit | Invoke-Expression") ||
		!strings.Contains(stderr, "pm shell-init pwsh | Out-String | Invoke-Expression") {
		t.Fatalf("stderr missing fallback instructions:\n%s", stderr)
	}
	if strings.Contains(stdout, "##pm-apply:") {
		t.Fatalf("stdout should not contain apply marker without shell-init:\n%s", stdout)
	}
}

func TestProfileNewNoApplyFlagSkipsPromptAndMarker(t *testing.T) {
	testEnv(t)
	t.Setenv("PM_SHELL_INIT_LOADED", "1")
	writeProfileNewTemplate(t, "Template.NoApply")

	stdout, stderr, err := runProfileNewTestStreams(t, "Y\n", []string{"Brand.NoApply"}, profileNewOpts{
		From:    "Template.NoApply",
		NoLogin: true,
		NoApply: true,
	}, true)
	if err != nil {
		t.Fatalf("profile new --no-apply: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if strings.Contains(stderr, "Apply this profile to your current shell now?") {
		t.Fatalf("--no-apply should not ask the apply prompt:\n%s", stderr)
	}
	if strings.Contains(stdout, "##pm-apply:") {
		t.Fatalf("stdout should not contain apply marker with --no-apply:\n%s", stdout)
	}
	if !strings.Contains(stderr, "Apply later with") {
		t.Fatalf("stderr missing apply-later hint:\n%s", stderr)
	}
}

func TestProfileNewApplyFlagsAreMutuallyExclusive(t *testing.T) {
	testEnv(t)

	_, _, err := runProfileNewTestStreams(t, "", nil, profileNewOpts{Apply: true, NoApply: true}, false)
	if err == nil {
		t.Fatal("expected --apply and --no-apply to error")
	}
	if !strings.Contains(err.Error(), "--apply and --no-apply are mutually exclusive") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProfileNewInteractiveApplyYesEmitsMarker(t *testing.T) {
	testEnv(t)
	t.Setenv("PM_SHELL_INIT_LOADED", "1")
	writeProfileNewTemplate(t, "Template.InteractiveApply")

	stdout, stderr, err := runProfileNewTestStreams(t, "Y\nY\n", []string{"Brand.InteractiveApply"}, profileNewOpts{
		From:    "Template.InteractiveApply",
		NoLogin: true,
	}, true)
	if err != nil {
		t.Fatalf("profile new interactive apply yes: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	assertProfileApplyMarkerFinal(t, stdout, "Brand.InteractiveApply")
}

func TestProfileNewInteractiveApplyNoSkipsMarker(t *testing.T) {
	testEnv(t)
	t.Setenv("PM_SHELL_INIT_LOADED", "1")
	writeProfileNewTemplate(t, "Template.InteractiveNoApply")

	stdout, stderr, err := runProfileNewTestStreams(t, "Y\nn\n", []string{"Brand.InteractiveNoApply"}, profileNewOpts{
		From:    "Template.InteractiveNoApply",
		NoLogin: true,
	}, true)
	if err != nil {
		t.Fatalf("profile new interactive apply no: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if strings.Contains(stdout, "##pm-apply:") {
		t.Fatalf("stdout should not contain apply marker after choosing no:\n%s", stdout)
	}
	if !strings.Contains(stderr, "Apply later with") {
		t.Fatalf("stderr missing apply-later hint:\n%s", stderr)
	}
}

func bufioReader(s string) *bufio.Reader {
	return bufio.NewReader(strings.NewReader(s))
}

func scriptedWizardReader(s string) *wizardReader {
	return &wizardReader{buf: bufioReader(s)}
}

func wizardStateNoAzure() wizard.State {
	return wizard.State{}
}

func wizardStateWithTenant() wizard.State {
	return wizard.State{Tenant: profileNewUUID}
}

func mustProfilePath(t *testing.T, name string) string {
	t.Helper()
	path, err := core.ProfilePath(name)
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func writeProfileNewTemplate(t *testing.T, name string) {
	t.Helper()
	home := os.Getenv("HOME")
	profile := &core.Profile{
		Schema: core.SchemaVersion,
		Name:   name,
		Label:  name,
		Color:  "Cyan",
		Azure: &core.AzureProfile{
			TenantID:       profileNewUUID,
			SubscriptionID: "66666666-7777-8888-9999-000000000000",
			ConfigDir:      filepath.Join(home, ".azure-"+name),
		},
		Azd: &core.AzdProfile{
			SubscriptionID: "66666666-7777-8888-9999-000000000000",
			ConfigDir:      filepath.Join(home, ".azd-"+name),
		},
	}
	if err := profile.Save(mustProfilePath(t, name)); err != nil {
		t.Fatal(err)
	}
}

func assertProfileApplyMarkerFinal(t *testing.T, stdout, name string) {
	t.Helper()
	marker := "##pm-apply:" + name
	trimmed := strings.TrimRight(stdout, "\r\n")
	if !strings.HasSuffix(stdout, marker+"\n") || !strings.HasSuffix(trimmed, marker) {
		t.Fatalf("stdout marker must be the final line %q:\n%s", marker, stdout)
	}
}
