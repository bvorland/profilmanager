package wizard

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/bvorland/profilmanager/internal/core"
)

const testUUID = "11111111-2222-3333-4444-555555555555"

func isolateHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", tmp)
		t.Setenv("APPDATA", filepath.Join(tmp, "appdata"))
		t.Setenv("LOCALAPPDATA", filepath.Join(tmp, "localappdata"))
	} else {
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "xdgcfg"))
		t.Setenv("XDG_STATE_HOME", filepath.Join(tmp, "xdgstate"))
	}
	return tmp
}

func stepByID(t *testing.T, id string) Step {
	t.Helper()
	for _, step := range Steps() {
		if step.ID() == id {
			return step
		}
	}
	t.Fatalf("step %q not found", id)
	return nil
}

func TestStepsBuildRoundTrip(t *testing.T) {
	isolateHome(t)
	s := &State{}
	inputs := map[string]string{
		"name":         "Contoso.NewProj",
		"preset":       PresetAzureAzd,
		"tenant":       testUUID,
		"subscription": "",
	}
	for _, step := range Steps() {
		input, ok := inputs[step.ID()]
		if !ok {
			input = step.Default(s)
		}
		if step.Skippable(s) && input == "" {
			continue
		}
		if err := step.Validate(input); err != nil {
			t.Fatalf("%s Validate(%q): %v", step.ID(), input, err)
		}
		step.Apply(s, input)
	}

	p, err := Build(s)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("profile Validate: %v", err)
	}
	if p.Name != "Contoso.NewProj" || p.Label != "🔵 Contoso.NewProj" || p.Color == "" {
		t.Fatalf("top-level profile mismatch: %+v", p)
	}
	if p.Azure == nil || p.Azure.TenantID != testUUID || !strings.Contains(p.Azure.ConfigDir, ".azure-Contoso.NewProj") {
		t.Fatalf("azure mismatch: %+v", p.Azure)
	}
	if p.Azd == nil || !strings.Contains(p.Azd.ConfigDir, ".azd-Contoso.NewProj") {
		t.Fatalf("azd mismatch: %+v", p.Azd)
	}
	if p.GitHub != nil || p.Kube != nil || p.Git != nil {
		t.Fatalf("azure-azd should not build full-devops blocks: %+v", p)
	}
}

func TestBuild_AppliesColorEmojiPrefix(t *testing.T) {
	isolateHome(t)
	p, err := Build(minimalBuildState("contoso-demo", "contoso-demo", "Cyan"))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if p.Label != "🔵 contoso-demo" {
		t.Fatalf("Label = %q", p.Label)
	}
}

func TestBuild_PreservesUserEmojiPrefix(t *testing.T) {
	isolateHome(t)
	p, err := Build(minimalBuildState("Foo.Profile", "🔵 Foo", "Cyan"))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if p.Label != "🔵 Foo" {
		t.Fatalf("Label = %q", p.Label)
	}
}

func TestBuild_NoColorChosen(t *testing.T) {
	isolateHome(t)
	p, err := Build(minimalBuildState("Foo.Profile", "Foo", ""))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if p.Label != "Foo" {
		t.Fatalf("Label = %q", p.Label)
	}
}

func minimalBuildState(name, label, color string) *State {
	return &State{
		Name:           name,
		Label:          label,
		Color:          color,
		Preset:         PresetAzureAzd,
		Tenant:         testUUID,
		AzureConfigDir: homePath(".azure-" + name),
		AzdConfigDir:   homePath(".azd-" + name),
	}
}

func TestStepValidation(t *testing.T) {
	tests := []struct {
		id   string
		good string
		bad  string
	}{
		{"name", "Contoso.Foo", "bad name"},
		{"color", "cyan", "not-a-color"},
		{"preset", PresetAzureOnly, "everything"},
		{"tenant", testUUID, "tenant"},
		{"subscription", "", "subscription"},
		{"azure_config_dir", filepath.Join("x", "azure"), ""},
		{"azd_config_dir", filepath.Join("x", "azd"), ""},
		{"gh_account", "octocat", "-bad"},
		{"gh_host", "github.com", ""},
	}
	for _, tc := range tests {
		step := stepByID(t, tc.id)
		if err := step.Validate(tc.good); err != nil {
			t.Errorf("%s good input rejected: %v", tc.id, err)
		}
		if err := step.Validate(tc.bad); err == nil {
			t.Errorf("%s bad input accepted", tc.id)
		}
	}
}

func TestSkippableByPreset(t *testing.T) {
	azureOnly := &State{Preset: PresetAzureOnly}
	azureAzd := &State{Preset: PresetAzureAzd}
	full := &State{Preset: PresetFullDevOps}

	if stepByID(t, "tenant").Skippable(azureOnly) {
		t.Fatal("tenant should be required for azure-only")
	}
	if !stepByID(t, "azd_config_dir").Skippable(azureOnly) {
		t.Fatal("azd_config_dir should be skippable for azure-only")
	}
	if stepByID(t, "azd_config_dir").Skippable(azureAzd) {
		t.Fatal("azd_config_dir should be required for azure-azd")
	}
	for _, id := range []string{"gh_account", "gh_host", "kube_context", "kube_namespace", "git_author", "git_email"} {
		if !stepByID(t, id).Skippable(azureAzd) {
			t.Fatalf("%s should be skippable for azure-azd", id)
		}
		if stepByID(t, id).Skippable(full) {
			t.Fatalf("%s should be required for full-devops", id)
		}
	}
}

func TestColorSuggestionUsesSiblingColors(t *testing.T) {
	isolateHome(t)
	for _, p := range []*core.Profile{
		{Schema: core.SchemaVersion, Name: "Contoso.MainDev", Color: "Cyan"},
		{Schema: core.SchemaVersion, Name: "Contoso.Backend", Color: "Magenta"},
		{Schema: core.SchemaVersion, Name: "Contoso.Pipeline", Color: "Yellow"},
		{Schema: core.SchemaVersion, Name: "Contoso.Platform", Color: "Blue"},
		{Schema: core.SchemaVersion, Name: "Contoso.Personal", Color: "White"},
	} {
		path, err := core.ProfilePath(p.Name)
		if err != nil {
			t.Fatal(err)
		}
		if err := p.Save(path); err != nil {
			t.Fatal(err)
		}
	}
	got := stepByID(t, "color").Default(&State{Name: "Contoso.NewProj"})
	if got != "Green" {
		t.Fatalf("color default: got %q want Green", got)
	}
}

func TestLoadTemplate(t *testing.T) {
	home := isolateHome(t)
	p := &core.Profile{
		Schema: core.SchemaVersion,
		Name:   "Contoso.MainDev",
		Label:  "Main",
		Color:  "Cyan",
		Azure: &core.AzureProfile{
			TenantID:       testUUID,
			SubscriptionID: "66666666-7777-8888-9999-000000000000",
			ConfigDir:      filepath.Join(home, ".azure-Contoso.MainDev"),
		},
		Azd: &core.AzdProfile{
			ConfigDir: filepath.Join(home, ".azd-Contoso.MainDev"),
		},
	}
	path, err := core.ProfilePath(p.Name)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Save(path); err != nil {
		t.Fatal(err)
	}

	s, err := LoadTemplate("Contoso.MainDev", "Contoso.NewProj")
	if err != nil {
		t.Fatalf("LoadTemplate: %v", err)
	}
	if s.Name != "Contoso.NewProj" || s.Label != "Contoso.NewProj" {
		t.Fatalf("name/label not reset: %+v", s)
	}
	if s.Color != "Cyan" {
		t.Fatalf("Color: got %q want Cyan", s.Color)
	}
	if !strings.Contains(s.AzureConfigDir, "Contoso.NewProj") || strings.Contains(s.AzureConfigDir, "MainDev") {
		t.Fatalf("AzureConfigDir not substituted: %q", s.AzureConfigDir)
	}
}
