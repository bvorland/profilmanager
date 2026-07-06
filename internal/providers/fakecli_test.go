package providers

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeCase describes one match arm of a fake CLI. When the fake CLI is
// invoked, its first argument is matched against Match; the
// corresponding Stdout file is typed out and the process exits with
// Exit. Match == "" is the default arm.
//
// The Stdout payload lives in a sibling file (one per case) — that
// sidesteps the cross-platform quoting nightmare for JSON braces,
// quotes, and backslashes.
type fakeCase struct {
	Match  string
	Stdout string
	Stderr string
	Exit   int
}

// fakePathDir creates a fresh dir, prepends it to PATH, and returns the
// dir. Auto-cleaned via t.TempDir().
func fakePathDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	sep := string(os.PathListSeparator)
	t.Setenv("PATH", dir+sep+os.Getenv("PATH"))
	return dir
}

// writeFakeCLI installs a fake CLI named `name` in dir with a
// dispatching script. Each case's Stdout/Stderr is written to a
// sibling file; the script `type`s (Windows) or `cat`s (Unix) the
// matched file, then exits with the case's Exit code.
//
// The fake CLI matches against *only* the first argument. That's
// sufficient for our providers (az.account.show, azd.auth, gh.auth,
// kubectl.config, git.config — every adapter dispatches on arg #1 or
// arg #1+#2 of the underlying CLI, and we ignore arg #2 because the
// first arg already disambiguates).
func writeFakeCLI(t *testing.T, dir, name string, cases ...fakeCase) {
	t.Helper()
	if len(cases) == 0 {
		t.Fatalf("writeFakeCLI(%s): at least one case required", name)
	}

	// Write payload files; track filename per case.
	type payload struct {
		stdoutFile, stderrFile string
	}
	pays := make([]payload, len(cases))
	for i, c := range cases {
		if c.Stdout != "" {
			p := filepath.Join(dir, fmt.Sprintf("%s.%d.stdout", name, i))
			if err := os.WriteFile(p, []byte(c.Stdout), 0o644); err != nil {
				t.Fatalf("write fake payload %s: %v", p, err)
			}
			pays[i].stdoutFile = p
		}
		if c.Stderr != "" {
			p := filepath.Join(dir, fmt.Sprintf("%s.%d.stderr", name, i))
			if err := os.WriteFile(p, []byte(c.Stderr), 0o644); err != nil {
				t.Fatalf("write fake payload %s: %v", p, err)
			}
			pays[i].stderrFile = p
		}
	}

	if runtime.GOOS == "windows" {
		scriptPath := filepath.Join(dir, name+".cmd")
		var b strings.Builder
		b.WriteString("@echo off\r\n")
		// Branch on %1 for each non-default case.
		for i, c := range cases {
			if c.Match == "" {
				continue
			}
			fmt.Fprintf(&b, "if \"%%~1\"==\"%s\" goto :c%d\r\n", c.Match, i)
		}
		// Default branch: pick the case with Match == "" if any,
		// else the first case.
		def := 0
		for i, c := range cases {
			if c.Match == "" {
				def = i
				break
			}
		}
		writeCmdLabelBody(&b, "default", cases[def], pays[def])
		// Per-case labels.
		for i, c := range cases {
			if c.Match == "" {
				continue
			}
			fmt.Fprintf(&b, ":c%d\r\n", i)
			writeCmdLabelBody(&b, fmt.Sprintf("c%d", i), c, pays[i])
		}
		if err := os.WriteFile(scriptPath, []byte(b.String()), 0o755); err != nil {
			t.Fatalf("write fake script %s: %v", scriptPath, err)
		}
		return
	}

	// Unix shell script.
	scriptPath := filepath.Join(dir, name)
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	b.WriteString("case \"$1\" in\n")
	hasDefault := false
	for i, c := range cases {
		if c.Match == "" {
			hasDefault = true
			continue
		}
		fmt.Fprintf(&b, "  %s)\n", shQuote(c.Match))
		writeShCaseBody(&b, c, pays[i])
		b.WriteString("    ;;\n")
	}
	// Default
	if hasDefault {
		for i, c := range cases {
			if c.Match == "" {
				b.WriteString("  *)\n")
				writeShCaseBody(&b, c, pays[i])
				b.WriteString("    ;;\n")
				break
			}
		}
	} else {
		b.WriteString("  *)\n")
		writeShCaseBody(&b, cases[0], pays[0])
		b.WriteString("    ;;\n")
	}
	b.WriteString("esac\n")
	if err := os.WriteFile(scriptPath, []byte(b.String()), 0o755); err != nil {
		t.Fatalf("write fake script %s: %v", scriptPath, err)
	}
}

func writeCmdLabelBody(b *strings.Builder, _ string, c fakeCase, p struct {
	stdoutFile, stderrFile string
}) {
	if p.stdoutFile != "" {
		fmt.Fprintf(b, "type \"%s\"\r\n", p.stdoutFile)
	}
	if p.stderrFile != "" {
		fmt.Fprintf(b, "type \"%s\" 1>&2\r\n", p.stderrFile)
	}
	fmt.Fprintf(b, "exit /b %d\r\n", c.Exit)
}

func writeShCaseBody(b *strings.Builder, c fakeCase, p struct {
	stdoutFile, stderrFile string
}) {
	if p.stdoutFile != "" {
		fmt.Fprintf(b, "    cat %s\n", shQuote(p.stdoutFile))
	}
	if p.stderrFile != "" {
		fmt.Fprintf(b, "    cat %s 1>&2\n", shQuote(p.stderrFile))
	}
	fmt.Fprintf(b, "    exit %d\n", c.Exit)
}

// shQuote wraps s in single quotes, escaping any embedded ' as '\”.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// sanitizeLabel returns s with non-[A-Za-z0-9] runes replaced by '_'.
// Used to derive safe filenames and Windows .cmd labels from arbitrary
// match strings.
func sanitizeLabel(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z',
			c >= 'A' && c <= 'Z',
			c >= '0' && c <= '9':
			out = append(out, c)
		default:
			out = append(out, '_')
		}
	}
	return string(out)
}
