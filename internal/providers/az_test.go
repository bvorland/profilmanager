package providers

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bvorland/profilmanager/internal/core"
)

func TestAzAvailableWhenOnPath(t *testing.T) {
	dir := fakePathDir(t)
	writeFakeCLI(t, dir, "az", fakeCase{Stdout: `{}`, Exit: 0})
	if !(azProvider{}).Available() {
		t.Errorf("expected azProvider.Available() = true with fake az on PATH")
	}
}

func TestAzAvailableWhenMissing(t *testing.T) {
	// Empty dir on PATH; az is not present.
	dir := t.TempDir()
	t.Setenv("PATH", dir) // intentionally replace, no fallback
	if (azProvider{}).Available() {
		t.Errorf("expected azProvider.Available() = false")
	}
}

func TestAzWhoamiLoggedIn(t *testing.T) {
	dir := fakePathDir(t)
	writeFakeCLI(t, dir, "az",
		fakeCase{Stdout: `{
			"id": "sub-1234",
			"name": "My Subscription",
			"tenantId": "tenant-abcd",
			"user": {"name": "alice@example.com", "type": "user"}
		}`, Exit: 0},
	)
	st, err := (azProvider{}).Whoami(context.Background())
	if err != nil {
		t.Fatalf("Whoami: %v", err)
	}
	if !st.LoggedIn {
		t.Fatalf("expected LoggedIn=true, got %+v", st)
	}
	if st.Account != "alice@example.com" {
		t.Errorf("Account = %q", st.Account)
	}
	if st.Tenant != "tenant-abcd" {
		t.Errorf("Tenant = %q", st.Tenant)
	}
	if st.Subscription != "sub-1234" {
		t.Errorf("Subscription = %q", st.Subscription)
	}
	if st.Extra["subscription_name"] != "My Subscription" {
		t.Errorf("subscription_name = %q", st.Extra["subscription_name"])
	}
}

func TestAzWhoamiNotLoggedIn(t *testing.T) {
	dir := fakePathDir(t)
	writeFakeCLI(t, dir, "az",
		fakeCase{Stderr: `Please run 'az login' to setup account.`, Exit: 1},
	)
	st, err := (azProvider{}).Whoami(context.Background())
	if err != nil {
		t.Fatalf("Whoami: %v", err)
	}
	if st.LoggedIn {
		t.Errorf("expected LoggedIn=false, got %+v", st)
	}
	if !strings.Contains(st.Error, "az login") {
		t.Errorf("expected stderr hint in Error, got %q", st.Error)
	}
}

func TestAzWhoamiMissing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	st, err := (azProvider{}).Whoami(context.Background())
	if err != nil {
		t.Fatalf("Whoami: %v", err)
	}
	if st.LoggedIn {
		t.Errorf("expected LoggedIn=false")
	}
	if st.Error == "" {
		t.Errorf("expected error message about missing CLI")
	}
}

func TestAzApplyWritesConfigAndEnv(t *testing.T) {
	tmp := t.TempDir()
	cfgDir := filepath.Join(tmp, "azure-test")
	p := &core.Profile{
		Schema: "1", Name: "test",
		Azure: &core.AzureProfile{ConfigDir: cfgDir},
	}
	env, err := (azProvider{}).Apply(p)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if env["AZURE_CONFIG_DIR"] != cfgDir {
		t.Errorf("AZURE_CONFIG_DIR = %q want %q", env["AZURE_CONFIG_DIR"], cfgDir)
	}
	if env["AZURE_CORE_OUTPUT"] != "json" {
		t.Errorf("AZURE_CORE_OUTPUT = %q", env["AZURE_CORE_OUTPUT"])
	}
	// Config file should exist with WAM-off and output=json under [core].
	data, err := os.ReadFile(filepath.Join(cfgDir, "config"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	body := string(data)
	if !strings.Contains(body, "[core]") {
		t.Errorf("missing [core] in baseline config:\n%s", body)
	}
	if !strings.Contains(body, "enable_broker_on_windows = false") {
		t.Errorf("missing WAM-off knob in config:\n%s", body)
	}
	if !strings.Contains(body, "output = json") {
		t.Errorf("missing output=json knob in config:\n%s", body)
	}

	// Idempotency: second call leaves identical bytes.
	if _, err := (azProvider{}).Apply(p); err != nil {
		t.Fatalf("Apply (2nd): %v", err)
	}
	data2, _ := os.ReadFile(filepath.Join(cfgDir, "config"))
	if string(data2) != body {
		t.Errorf("Apply not idempotent:\nfirst:\n%s\nsecond:\n%s", body, data2)
	}
}

func TestAzApplyPreservesExistingConfig(t *testing.T) {
	tmp := t.TempDir()
	cfgDir := filepath.Join(tmp, "azure-test")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	pre := `[extension]
use_dynamic_install = yes_without_prompt

[core]
no_color = true
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config"), []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}
	p := &core.Profile{
		Schema: "1", Name: "test",
		Azure: &core.AzureProfile{ConfigDir: cfgDir},
	}
	if _, err := (azProvider{}).Apply(p); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	body, _ := os.ReadFile(filepath.Join(cfgDir, "config"))
	s := string(body)
	for _, want := range []string{
		"use_dynamic_install = yes_without_prompt",
		"no_color = true",
		"enable_broker_on_windows = false",
		"output = json",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in merged config:\n%s", want, s)
		}
	}
}

func TestAzApplyWithoutAzureBlockReturnsBaseEnv(t *testing.T) {
	p := &core.Profile{Schema: "1", Name: "p"}
	env, err := (azProvider{}).Apply(p)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if env["AZURE_CORE_OUTPUT"] != "json" {
		t.Errorf("expected AZURE_CORE_OUTPUT=json, got %q", env["AZURE_CORE_OUTPUT"])
	}
	if _, ok := env["AZURE_CONFIG_DIR"]; ok {
		t.Errorf("AZURE_CONFIG_DIR should not be set when Azure.ConfigDir is empty")
	}
}

func TestUpsertINIBehaviour(t *testing.T) {
	cases := []struct {
		name, in, wantContains string
	}{
		{
			name:         "empty",
			in:           "",
			wantContains: "[core]",
		},
		{
			name:         "no-section",
			in:           "# header comment\n",
			wantContains: "enable_broker_on_windows = false",
		},
		{
			name:         "update-existing-key",
			in:           "[core]\nenable_broker_on_windows = true\noutput = table\n",
			wantContains: "enable_broker_on_windows = false",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := upsertINI(c.in, "core", map[string]string{
				"enable_broker_on_windows": "false",
				"output":                   "json",
			})
			if !strings.Contains(out, c.wantContains) {
				t.Errorf("want %q in output:\n%s", c.wantContains, out)
			}
		})
	}
}

func TestExpandHomeTilde(t *testing.T) {
	home, _ := os.UserHomeDir()
	cases := map[string]string{
		"":              "",
		"/abs/path":     "/abs/path",
		"~":             home,
		"~/.azure":      filepath.Join(home, ".azure"),
		`~\AppData`:     filepath.Join(home, "AppData"),
		"relative/path": "relative/path",
	}
	for in, want := range cases {
		if got := expandHome(in); got != want {
			t.Errorf("expandHome(%q) = %q want %q", in, got, want)
		}
	}
}
