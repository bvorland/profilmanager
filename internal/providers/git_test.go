package providers

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bvorland/profilmanager/internal/core"
)

func TestGitApplyAllFields(t *testing.T) {
	p := &core.Profile{
		Schema: "1", Name: "g",
		Git: &core.GitIdentity{
			UserName:   "Alice Doe",
			UserEmail:  "alice@example.com",
			SigningKey: "~/.ssh/id_ed25519_alice",
		},
	}
	env, err := (gitProvider{}).Apply(p)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if env["GIT_AUTHOR_NAME"] != "Alice Doe" {
		t.Errorf("GIT_AUTHOR_NAME = %q", env["GIT_AUTHOR_NAME"])
	}
	if env["GIT_COMMITTER_NAME"] != "Alice Doe" {
		t.Errorf("GIT_COMMITTER_NAME = %q", env["GIT_COMMITTER_NAME"])
	}
	if env["GIT_AUTHOR_EMAIL"] != "alice@example.com" {
		t.Errorf("GIT_AUTHOR_EMAIL = %q", env["GIT_AUTHOR_EMAIL"])
	}
	if env["GIT_COMMITTER_EMAIL"] != "alice@example.com" {
		t.Errorf("GIT_COMMITTER_EMAIL = %q", env["GIT_COMMITTER_EMAIL"])
	}
	if !strings.Contains(env["GIT_SSH_COMMAND"], "id_ed25519_alice") {
		t.Errorf("GIT_SSH_COMMAND missing key path: %q", env["GIT_SSH_COMMAND"])
	}
	if !strings.Contains(env["GIT_SSH_COMMAND"], "IdentitiesOnly=yes") {
		t.Errorf("GIT_SSH_COMMAND missing IdentitiesOnly: %q", env["GIT_SSH_COMMAND"])
	}
}

func TestGitApplyEmpty(t *testing.T) {
	p := &core.Profile{Schema: "1", Name: "g"}
	env, err := (gitProvider{}).Apply(p)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(env) != 0 {
		t.Errorf("expected empty env, got %+v", env)
	}
}

func TestGitWhoami(t *testing.T) {
	dir := fakePathDir(t)
	writeGitFake(t, dir, map[string]string{
		"user.name":       "Alice Doe",
		"user.email":      "alice@example.com",
		"user.signingkey": "AAAAC3NzaC1lZDI1NTE5",
	})
	st, err := (gitProvider{}).Whoami(context.Background())
	if err != nil {
		t.Fatalf("Whoami: %v", err)
	}
	if !st.LoggedIn {
		t.Errorf("git should always be LoggedIn when Available")
	}
	if st.Account != "Alice Doe" {
		t.Errorf("Account = %q", st.Account)
	}
	if st.Extra["email"] != "alice@example.com" {
		t.Errorf("email = %q", st.Extra["email"])
	}
	if st.Extra["signing_key"] != "AAAAC3NzaC1lZDI1NTE5" {
		t.Errorf("signing_key = %q", st.Extra["signing_key"])
	}
}

func TestGitWhoamiUnset(t *testing.T) {
	dir := fakePathDir(t)
	writeGitFake(t, dir, map[string]string{})
	st, err := (gitProvider{}).Whoami(context.Background())
	if err != nil {
		t.Fatalf("Whoami: %v", err)
	}
	if !st.LoggedIn {
		t.Errorf("git should be LoggedIn when installed even without identity")
	}
	if st.Error == "" {
		t.Errorf("expected 'no git identity' error, got nothing")
	}
}

// writeGitFake installs a fake git that responds to
// `git config --get <key>` based on values. Missing keys exit 1
// (matching real git's behaviour).
func writeGitFake(t *testing.T, dir string, values map[string]string) {
	t.Helper()
	type entry struct{ key, file string }
	var entries []entry
	for k, v := range values {
		p := filepath.Join(dir, "git-"+sanitizeLabel(k)+".txt")
		if err := os.WriteFile(p, []byte(v), 0o644); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, entry{key: k, file: p})
	}
	if isWindows() {
		var sb strings.Builder
		sb.WriteString("@echo off\r\n")
		sb.WriteString("if /I not \"%~1\"==\"config\" exit /b 1\r\n")
		sb.WriteString("if /I not \"%~2\"==\"--get\" exit /b 1\r\n")
		for _, e := range entries {
			sb.WriteString("if /I \"%~3\"==\"" + e.key + "\" (\r\n")
			sb.WriteString("  type \"" + e.file + "\"\r\n")
			sb.WriteString("  exit /b 0\r\n")
			sb.WriteString(")\r\n")
		}
		sb.WriteString("exit /b 1\r\n")
		path := filepath.Join(dir, "git.cmd")
		if err := os.WriteFile(path, []byte(sb.String()), 0o755); err != nil {
			t.Fatal(err)
		}
		return
	}
	var sb strings.Builder
	sb.WriteString("#!/bin/sh\n")
	sb.WriteString("[ \"$1\" = \"config\" ] || exit 1\n")
	sb.WriteString("[ \"$2\" = \"--get\" ] || exit 1\n")
	sb.WriteString("case \"$3\" in\n")
	for _, e := range entries {
		sb.WriteString("  " + shQuote(e.key) + ")\n")
		sb.WriteString("    cat " + shQuote(e.file) + "\n")
		sb.WriteString("    exit 0\n")
		sb.WriteString("    ;;\n")
	}
	sb.WriteString("esac\n")
	sb.WriteString("exit 1\n")
	path := filepath.Join(dir, "git")
	if err := os.WriteFile(path, []byte(sb.String()), 0o755); err != nil {
		t.Fatal(err)
	}
}
