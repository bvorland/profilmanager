package providers

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/bvorland/profilmanager/internal/core"
)

func stubCoreDirs(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	switch runtime.GOOS {
	case "windows":
		t.Setenv("APPDATA", filepath.Join(tmp, "appdata"))
		t.Setenv("LOCALAPPDATA", filepath.Join(tmp, "localappdata"))
	default:
		t.Setenv("HOME", filepath.Join(tmp, "home"))
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "xdgcfg"))
		t.Setenv("XDG_STATE_HOME", filepath.Join(tmp, "xdgstate"))
	}
	return tmp
}

func TestGhWhoamiLoggedIn(t *testing.T) {
	dir := fakePathDir(t)
	body := `{"github.com": {
		"user": "octocat",
		"active": true,
		"git_protocol": "ssh",
		"scopes": ["repo", "workflow"],
		"token_source": "keyring"
	}}`
	writeFakeCLI(t, dir, "gh", fakeCase{Stdout: body, Exit: 0})
	st, err := (ghProvider{}).Whoami(context.Background())
	if err != nil {
		t.Fatalf("Whoami: %v", err)
	}
	if !st.LoggedIn {
		t.Fatalf("expected LoggedIn=true: %+v", st)
	}
	if st.Account != "octocat" {
		t.Errorf("Account = %q", st.Account)
	}
	if st.Subscription != "github.com" {
		t.Errorf("Subscription (primary host) = %q", st.Subscription)
	}
	if st.Extra["scopes"] != "repo,workflow" {
		t.Errorf("scopes = %q", st.Extra["scopes"])
	}
	if st.Extra["git_protocol"] != "ssh" {
		t.Errorf("git_protocol = %q", st.Extra["git_protocol"])
	}
}

func TestGhWhoamiLoggedInV2(t *testing.T) {
	dir := fakePathDir(t)
	// Real gh ≥ 2.40 shape: wrapped, array of accounts per host,
	// camelCase fields.
	body := `{"hosts": {"github.com": [
		{"state":"success","active":true,"host":"github.com","login":"bvorland","tokenSource":"keyring","scopes":"repo, workflow","gitProtocol":"https"},
		{"state":"success","active":false,"host":"github.com","login":"other","tokenSource":"oauth_token","scopes":"repo","gitProtocol":"https"}
	]}}`
	writeFakeCLI(t, dir, "gh", fakeCase{Stdout: body, Exit: 0})
	st, err := (ghProvider{}).Whoami(context.Background())
	if err != nil {
		t.Fatalf("Whoami: %v", err)
	}
	if !st.LoggedIn {
		t.Fatalf("expected LoggedIn=true: %+v", st)
	}
	if st.Account != "bvorland" {
		t.Errorf("expected active=true account, got %q", st.Account)
	}
	if st.Extra["accounts_on_host"] != "2" {
		t.Errorf("accounts_on_host = %q", st.Extra["accounts_on_host"])
	}
	if st.Extra["scopes"] == "" {
		t.Errorf("expected scopes from active account")
	}
}

func TestGhWhoamiNotLoggedIn(t *testing.T) {
	dir := fakePathDir(t)
	writeFakeCLI(t, dir, "gh",
		fakeCase{Stderr: "You are not logged into any GitHub hosts. To log in, run: gh auth login", Exit: 1},
	)
	st, err := (ghProvider{}).Whoami(context.Background())
	if err != nil {
		t.Fatalf("Whoami: %v", err)
	}
	if st.LoggedIn {
		t.Errorf("expected LoggedIn=false")
	}
	if !strings.Contains(st.Error, "gh auth login") {
		t.Errorf("expected stderr hint, got %q", st.Error)
	}
}

func TestGhApplyDirsUnderState(t *testing.T) {
	stubCoreDirs(t)
	p := &core.Profile{Schema: "1", Name: "Contoso.MainDev"}
	env, err := (ghProvider{}).Apply(p)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got := env["GH_CONFIG_DIR"]
	if got == "" {
		t.Fatalf("GH_CONFIG_DIR not set: %+v", env)
	}
	if !strings.HasSuffix(filepath.ToSlash(got), "/gh/Contoso.MainDev") {
		t.Errorf("GH_CONFIG_DIR suffix unexpected: %q", got)
	}
	if fi, err := os.Stat(got); err != nil || !fi.IsDir() {
		t.Errorf("dir not created: %v %v", fi, err)
	}
}
