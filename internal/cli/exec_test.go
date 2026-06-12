package cli

import (
	"bytes"
	"os/exec"
	"runtime"
	"strings"
	"testing"

	"github.com/bvorland/profilmanager/internal/core"
)

// pickEchoCommand returns a tiny command + args that prints "hello"
// without depending on a shell. We prefer `cmd /c` on Windows because
// echo.exe isn't always on PATH; we don't pass anything through a shell
// in the test subject itself — only here as a child of pm exec.
//
// (pm exec under test still calls the binary directly, not a shell.)
func pickEchoCommand(t *testing.T) (string, []string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		path, err := exec.LookPath("cmd")
		if err != nil {
			t.Skip("cmd not on PATH; cannot run exec smoke test")
		}
		return path, []string{"/c", "echo", "hello"}
	}
	for _, name := range []string{"echo", "/bin/echo"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, []string{"hello"}
		}
	}
	t.Skip("echo not on PATH")
	return "", nil
}

func TestExecRunsChildWithProfileEnv(t *testing.T) {
	withTempDirs(t)
	writeProfile(t, &core.Profile{
		Name: "execp",
		Env:  []core.EnvEntry{{Key: "PM_EXEC_TEST", Value: "yes"}},
	})

	prog, progArgs := pickEchoCommand(t)
	args := append([]string{"exec", "execp", "--"}, append([]string{prog}, progArgs...)...)

	root := newRootCmd()
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		t.Fatalf("exec: %v -- stderr: %s", err, errBuf.String())
	}
	if !strings.Contains(out.String(), "hello") {
		t.Fatalf("child stdout not captured: %q", out.String())
	}
}

func TestExecRefusesMissingSeparator(t *testing.T) {
	withTempDirs(t)
	writeProfile(t, &core.Profile{Name: "p"})

	root := newRootCmd()
	var errBuf bytes.Buffer
	root.SetOut(&errBuf)
	root.SetErr(&errBuf)
	// No `--` — must refuse.
	root.SetArgs([]string{"exec", "p", "echo", "hi"})
	if err := root.Execute(); err == nil {
		t.Fatalf("expected error when -- separator missing")
	}
	if !strings.Contains(errBuf.String(), "--") {
		t.Fatalf("error message should reference `--`: %s", errBuf.String())
	}
}

func TestExecRequiresProfileOrActive(t *testing.T) {
	withTempDirs(t)
	withNonTTYStdin(t)
	root := newRootCmd()
	var errBuf bytes.Buffer
	root.SetOut(&errBuf)
	root.SetErr(&errBuf)
	root.SetIn(strings.NewReader(""))
	root.SetArgs([]string{"exec", "--", "echo", "x"})
	if err := root.Execute(); err == nil {
		t.Fatalf("expected error when neither profile nor active is set")
	}
	if !strings.Contains(strings.ToLower(errBuf.String()), "stdin is not a tty") {
		t.Fatalf("expected non-TTY profile message, got: %s", errBuf.String())
	}
}

func TestExecRefusesUnknownBinary(t *testing.T) {
	withTempDirs(t)
	writeProfile(t, &core.Profile{Name: "p"})

	root := newRootCmd()
	var errBuf bytes.Buffer
	root.SetOut(&errBuf)
	root.SetErr(&errBuf)
	root.SetArgs([]string{"exec", "p", "--", "definitely-not-a-real-binary-xyzzy"})
	if err := root.Execute(); err == nil {
		t.Fatalf("expected lookpath error")
	}
	if !strings.Contains(errBuf.String(), "not found") {
		t.Fatalf("error should mention 'not found': %s", errBuf.String())
	}
}

func TestExecPropagatesNonzeroExit(t *testing.T) {
	withTempDirs(t)
	writeProfile(t, &core.Profile{Name: "p"})

	// `cmd /c exit 7` on Windows; `false` on Unix exits 1, so we use a
	// custom invocation to get exit 7 cross-platform.
	var prog string
	var args []string
	if runtime.GOOS == "windows" {
		p, err := exec.LookPath("cmd")
		if err != nil {
			t.Skip("cmd not on PATH")
		}
		prog = p
		args = []string{"/c", "exit", "7"}
	} else {
		// `sh -c 'exit 7'` is shell, but here it's the *child* — pm
		// exec itself doesn't use a shell.
		p, err := exec.LookPath("sh")
		if err != nil {
			t.Skip("sh not on PATH")
		}
		prog = p
		args = []string{"-c", "exit 7"}
	}

	all := append([]string{"exec", "p", "--", prog}, args...)
	root := newRootCmd()
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetArgs(all)
	err := root.Execute()
	if err == nil {
		t.Fatalf("expected non-nil error for nonzero exit")
	}
	if code := CodeFor(err); code != 7 {
		t.Fatalf("exit code = %d, want 7 (err=%v stderr=%s)", code, err, errBuf.String())
	}
}

func TestSplitExecArgs(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		dashAt  int
		profile string
		child   []string
		wantErr bool
	}{
		{"no args no dash", nil, -1, "", nil, false},
		{"profile + child", []string{"prof", "echo", "hi"}, 1, "prof", []string{"echo", "hi"}, false},
		{"no profile", []string{"echo", "hi"}, 0, "", []string{"echo", "hi"}, false},
		{"two positionals before --", []string{"a", "b", "echo"}, 2, "", nil, true},
		{"no separator", []string{"echo", "hi"}, -1, "", nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, c, err := splitExecArgs(tc.args, tc.dashAt)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want err, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("err: %v", err)
			}
			if p != tc.profile {
				t.Fatalf("profile = %q, want %q", p, tc.profile)
			}
			if strings.Join(c, " ") != strings.Join(tc.child, " ") {
				t.Fatalf("child = %v, want %v", c, tc.child)
			}
		})
	}
}
