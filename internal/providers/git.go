package providers

import (
	"context"
	"fmt"
	"strings"

	"github.com/bvorland/profilmanager/internal/core"
)

// gitProvider integrates git identity.
//
// git's "auth" is a misnomer — git itself doesn't auth, it delegates to
// SSH, https credential helpers, or signed-commit tooling. The profile
// model only pins identity (name/email/signing key); pushing/pulling
// continues to use whatever credential helper the operator has wired
// up. If a profile sets SigningKey, we point GIT_SSH_COMMAND at the
// matching ssh identity so signed commits use the right key.
type gitProvider struct{}

func GitProvider() Provider { return gitProvider{} }

func (gitProvider) Name() string { return "git" }

func (gitProvider) Available() bool {
	_, err := lookPath("git")
	return err == nil
}

func (gitProvider) Apply(p *core.Profile) (map[string]string, error) {
	env := map[string]string{}
	if p == nil || p.Git == nil {
		return env, nil
	}
	g := p.Git
	if g.UserName != "" {
		env["GIT_AUTHOR_NAME"] = g.UserName
		env["GIT_COMMITTER_NAME"] = g.UserName
	}
	if g.UserEmail != "" {
		env["GIT_AUTHOR_EMAIL"] = g.UserEmail
		env["GIT_COMMITTER_EMAIL"] = g.UserEmail
	}
	if g.SigningKey != "" {
		// `signing_key` in the profile is treated as a path to a
		// private SSH key. We expand ~ and feed it to GIT_SSH_COMMAND;
		// git will use that ssh invocation for fetch/push, and any
		// signed-commit flows that go through ssh-keygen pick the same
		// identity.
		keyPath := expandHome(g.SigningKey)
		// IdentitiesOnly=yes prevents ssh from offering the operator's
		// default keys; we want the profile's key, exclusively.
		env["GIT_SSH_COMMAND"] = fmt.Sprintf(`ssh -i %s -o IdentitiesOnly=yes`, shellQuote(keyPath))
	}
	return env, nil
}

func (gitProvider) Whoami(ctx context.Context) (Status, error) {
	st := Status{Provider: "git"}
	if _, err := lookPath("git"); err != nil {
		st.Error = "git not installed"
		return st, nil
	}
	ctx, cancel := withDefaultTimeout(ctx)
	defer cancel()
	// `git config --get user.name` exits 1 when unset; treat that as
	// "no identity configured" and surface it gracefully.
	st.LoggedIn = true // git is "available" if installed; identity is separate
	name, _, err := runCmd(ctx, "git", "config", "--get", "user.name")
	if err == nil {
		st.Account = strings.TrimSpace(string(name))
	}
	email, _, err := runCmd(ctx, "git", "config", "--get", "user.email")
	if err == nil {
		st.Extra = map[string]string{"email": strings.TrimSpace(string(email))}
	}
	signingKey, _, err := runCmd(ctx, "git", "config", "--get", "user.signingkey")
	if err == nil {
		if st.Extra == nil {
			st.Extra = map[string]string{}
		}
		if v := strings.TrimSpace(string(signingKey)); v != "" {
			st.Extra["signing_key"] = v
		}
	}
	if st.Account == "" && (st.Extra == nil || st.Extra["email"] == "") {
		st.Error = "no git identity configured (user.name / user.email unset)"
	}
	return st, nil
}

// shellQuote produces a string safe to embed in a shell command. We use
// single quotes on Unix, double on Windows. GIT_SSH_COMMAND is parsed by
// git via shell-style splitting, so quoting matters when paths contain
// spaces (very common on Windows).
func shellQuote(s string) string {
	if strings.ContainsAny(s, " \t\"'") {
		// Use double quotes for cross-platform parsing; escape inner ".
		escaped := strings.ReplaceAll(s, `"`, `\"`)
		return `"` + escaped + `"`
	}
	return s
}
