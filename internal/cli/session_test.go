package cli

import (
	"strings"
	"testing"
)

// TestSessionInitSnapshot exercises both bash and pwsh forms with a
// deterministic UUID injected via the package-level newSessionID hook.
// The output must match a fixed snapshot byte-for-byte so any future
// shell-quoting change is caught.
func TestSessionInitSnapshot(t *testing.T) {
	testEnv(t)
	const fakeID = "00000000-0000-4000-8000-000000000001"
	orig := newSessionID
	newSessionID = func() (string, error) { return fakeID, nil }
	t.Cleanup(func() { newSessionID = orig })

	cases := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "bash",
			args: []string{"session", "init", "--shell", "bash"},
			want: `export PM_SESSION_ID="` + fakeID + `"` + "\n",
		},
		{
			name: "zsh",
			args: []string{"session", "init", "--shell", "zsh"},
			want: `export PM_SESSION_ID="` + fakeID + `"` + "\n",
		},
		{
			name: "pwsh",
			args: []string{"session", "init", "--shell", "pwsh"},
			want: `$env:PM_SESSION_ID = '` + fakeID + `'` + "\n",
		},
		{
			name: "fish",
			args: []string{"session", "init", "--shell", "fish"},
			want: `set -gx PM_SESSION_ID ` + fakeID + "\n",
		},
		{
			name: "cmd",
			args: []string{"session", "init", "--shell", "cmd"},
			want: `set PM_SESSION_ID=` + fakeID + "\n",
		},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			stdout, _, err := runCLI(t, c.args...)
			if err != nil {
				t.Fatalf("err: %v stdout=%q", err, stdout)
			}
			if stdout != c.want {
				t.Errorf("snapshot mismatch\nwant: %q\ngot:  %q", c.want, stdout)
			}
			ensureNoColor(t, stdout)
		})
	}
}

// TestSessionInitUnsupportedShell asserts the exit code path for a bad
// --shell value.
func TestSessionInitUnsupportedShell(t *testing.T) {
	testEnv(t)
	_, _, err := runCLI(t, "session", "init", "--shell", "tcsh")
	if err == nil {
		t.Fatal("expected error")
	}
	if CodeFor(err) != ExitUsage {
		t.Errorf("expected ExitUsage, got %d", CodeFor(err))
	}
}

// TestShellInitPwshWithShims pins the pwsh + --with-shims snapshot.
// This is the most subtle case — it has to dodge function-vs-binary
// recursion via Get-Command -CommandType Application.
func TestShellInitPwshWithShims(t *testing.T) {
	testEnv(t)
	const fakeID = "11111111-2222-4333-8444-555566667777"
	orig := newSessionID
	newSessionID = func() (string, error) { return fakeID, nil }
	t.Cleanup(func() { newSessionID = orig })

	stdout, _, err := runCLI(t, "shell-init", "--shell", "pwsh", "--with-shims")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	ensureNoColor(t, stdout)

	want := `# pm shell-init (pwsh) — generated; do not edit by hand
if (-not $env:PM_SESSION_ID) {
  $env:PM_SESSION_ID = '` + fakeID + `'
}

` + pwshPMWrapper() + `

# >>> pm completion >>>
if (Get-Command pm -ErrorAction SilentlyContinue) {
  (& pm completion pwsh | Out-String) | Invoke-Expression
}
# <<< pm completion <<<

function az {
  $__pm_profile = (& pm whoami --profile-name 2>$null)
  if ($__pm_profile) {
    & pm exec --profile $__pm_profile -- az @args
  } else {
    & (Get-Command -CommandType Application az | Select-Object -First 1).Source @args
  }
}
function azd {
  $__pm_profile = (& pm whoami --profile-name 2>$null)
  if ($__pm_profile) {
    & pm exec --profile $__pm_profile -- azd @args
  } else {
    & (Get-Command -CommandType Application azd | Select-Object -First 1).Source @args
  }
}
function gh {
  $__pm_profile = (& pm whoami --profile-name 2>$null)
  if ($__pm_profile) {
    & pm exec --profile $__pm_profile -- gh @args
  } else {
    & (Get-Command -CommandType Application gh | Select-Object -First 1).Source @args
  }
}
function kubectl {
  $__pm_profile = (& pm whoami --profile-name 2>$null)
  if ($__pm_profile) {
    & pm exec --profile $__pm_profile -- kubectl @args
  } else {
    & (Get-Command -CommandType Application kubectl | Select-Object -First 1).Source @args
  }
}
function git {
  $__pm_profile = (& pm whoami --profile-name 2>$null)
  if ($__pm_profile) {
    & pm exec --profile $__pm_profile -- git @args
  } else {
    & (Get-Command -CommandType Application git | Select-Object -First 1).Source @args
  }
}
`
	if stdout != want {
		t.Errorf("snapshot mismatch\nwant:\n%s\n---\ngot:\n%s", want, stdout)
	}
}

// TestShellInitBashNoShims verifies the minimal bash form: just the
// session-id guard, no shim functions.
func TestShellInitBashNoShims(t *testing.T) {
	testEnv(t)
	const fakeID = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	orig := newSessionID
	newSessionID = func() (string, error) { return fakeID, nil }
	t.Cleanup(func() { newSessionID = orig })

	stdout, _, err := runCLI(t, "shell-init", "--shell", "bash")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := `# pm shell-init (bash/zsh) — generated; do not edit by hand
if [ -z "${PM_SESSION_ID:-}" ]; then
  export PM_SESSION_ID="` + fakeID + `"
fi
`
	if stdout != want {
		t.Errorf("snapshot mismatch\nwant:\n%s\n---\ngot:\n%s", want, stdout)
	}
	// Sanity: shim text must NOT appear without --with-shims.
	if strings.Contains(stdout, "command pm exec") {
		t.Error("--without --with-shims should not emit shim bodies")
	}
}
