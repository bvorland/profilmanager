package mcp

import "testing"

// TestIsAllowedCommand_AcceptsBareBasenames verifies the happy path: a
// bare command name on the default allowlist passes the check, with and
// without the case-insensitive .exe suffix.
func TestIsAllowedCommand_AcceptsBareBasenames(t *testing.T) {
	cases := []string{
		"az", "azd", "gh", "kubectl", "git",
		"AZ", "Az", "GH",
		"az.exe", "AZ.EXE", "kubectl.EXE",
		"  az  ", // whitespace trimmed
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			if !IsAllowedCommand(name) {
				t.Fatalf("IsAllowedCommand(%q) = false, want true", name)
			}
		})
	}
}

// TestIsAllowedCommand_RejectsPathTraversal is the regression test for
// the v1.0 pre-publish security review finding: stripping the path
// before allowlist comparison would let "C:\Users\Public\az.exe" pass
// as "az", then exec.CommandContext would run the planted binary.
// commandBasename now rejects any input containing a path separator,
// an absolute-path marker, or a shell metacharacter.
func TestIsAllowedCommand_RejectsPathTraversal(t *testing.T) {
	rejects := []string{
		`C:\Users\Public\az.exe`,
		`C:/Users/Public/az.exe`,
		`/usr/bin/az`,
		`/usr/local/bin/gh`,
		`./az`,
		`.\az`,
		`..\az.exe`,
		`../bin/az`,
		`\\evil-share\az.exe`,
		`//evil-share/az.exe`,
		`bin/az`,
		`subdir\az.exe`,
	}
	for _, name := range rejects {
		t.Run(name, func(t *testing.T) {
			if IsAllowedCommand(name) {
				t.Fatalf("IsAllowedCommand(%q) = true, want false — path-containing input must be rejected even when basename is allowlisted", name)
			}
		})
	}
}

// TestIsAllowedCommand_RejectsShellMetacharacters guards the existing
// metacharacter check from regression. exec.CommandContext doesn't
// invoke a shell, but allowing these would either be a programming bug
// or evidence of attempted shell injection in a misconfigured caller.
func TestIsAllowedCommand_RejectsShellMetacharacters(t *testing.T) {
	rejects := []string{
		`az;rm`,
		`az&calc`,
		"az|cat",
		"az`whoami`",
		"az$VAR",
		"az<in",
		"az>out",
		`az"x`,
		"az'x",
		"az\nrm",
		"az\rrm",
		"az\trm",
		"az\x00rm",
	}
	for _, name := range rejects {
		t.Run(name, func(t *testing.T) {
			if IsAllowedCommand(name) {
				t.Fatalf("IsAllowedCommand(%q) = true, want false", name)
			}
		})
	}
}

// TestIsAllowedCommand_RejectsNonAllowlisted confirms the basic
// allowlist semantics still hold after the path-rejection tightening.
func TestIsAllowedCommand_RejectsNonAllowlisted(t *testing.T) {
	rejects := []string{
		"",
		"cmd",
		"powershell",
		"pwsh",
		"sh",
		"bash",
		"node",
		"python",
		"curl",
		"wget",
		"unknown-tool",
	}
	for _, name := range rejects {
		t.Run(name, func(t *testing.T) {
			if IsAllowedCommand(name) {
				t.Fatalf("IsAllowedCommand(%q) = true, want false", name)
			}
		})
	}
}
