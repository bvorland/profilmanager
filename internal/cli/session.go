package cli

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

// newSessionID is overridable by tests to make output deterministic.
var newSessionID = generateUUIDv4

// generateUUIDv4 returns a fresh v4 UUID using crypto/rand. We avoid a
// dependency on github.com/google/uuid for this one use site.
func generateUUIDv4() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("read random: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// ---------- pm session ----------

func newSessionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "session",
		Short: "Session ID helpers (init)",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(newSessionInitCmd())
	return cmd
}

func newSessionInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Emit shell code that sets PM_SESSION_ID for the current shell",
		Long: `Emit shell code that exports a fresh UUIDv4 as PM_SESSION_ID.

PM_SESSION_ID is the canonical session identifier pm uses to scope the
"active profile" state file (see internal/state/session.go). Setting it
explicitly avoids the PPID-fallback path that pm doctor warns about.

Typical usage:

  bash / zsh:   eval "$(pm session init --shell bash)"
  fish:         pm session init --shell fish | source
  PowerShell:   pm session init --shell pwsh | Invoke-Expression
  cmd.exe:      pm session init --shell cmd > %TEMP%\pm-session.bat & call %TEMP%\pm-session.bat

For PowerShell profile setup, prefer "pm shell-init pwsh"; it is a strict
superset that also installs completion and the profile-new auto-apply wrapper.

With no --shell, the shell is detected from $SHELL / $PSModulePath / OS
defaults.`,
		Args: cobra.NoArgs,
		RunE: runSessionInit,
	}
	cmd.Flags().String("shell", "", "shell flavor: bash|zsh|pwsh|fish|cmd (auto-detected if empty)")
	return cmd
}

func runSessionInit(cmd *cobra.Command, _ []string) error {
	shell, _ := cmd.Flags().GetString("shell")
	shell = strings.ToLower(strings.TrimSpace(shell))
	if shell == "" {
		shell = detectShell()
	}

	id, err := newSessionID()
	if err != nil {
		return emitError(cmd, err)
	}

	line, err := sessionInitLine(shell, id)
	if err != nil {
		return emitError(cmd, errInvalidUsage("%v", err))
	}
	fmt.Fprintln(cmd.OutOrStdout(), line)
	return nil
}

// sessionInitLine returns the shell-specific export statement for
// PM_SESSION_ID. Exported so shell-init can reuse it.
func sessionInitLine(shell, id string) (string, error) {
	switch shell {
	case "bash", "zsh":
		return fmt.Sprintf(`export PM_SESSION_ID=%q`, id), nil
	case "pwsh", "powershell":
		// Single-quote in PowerShell is a literal string — UUIDs have no
		// special chars but we keep the discipline anyway.
		return fmt.Sprintf(`$env:PM_SESSION_ID = '%s'`, id), nil
	case "fish":
		return fmt.Sprintf(`set -gx PM_SESSION_ID %s`, id), nil
	case "cmd", "cmd.exe":
		return fmt.Sprintf(`set PM_SESSION_ID=%s`, id), nil
	default:
		return "", fmt.Errorf("unsupported shell %q (want one of: bash, zsh, pwsh, fish, cmd)", shell)
	}
}

// detectShell makes a best-effort guess. Precedence:
//
//  1. $SHELL (Unix-ish) — trust its basename.
//  2. $PSModulePath set and $SHELL empty — pwsh (PowerShell session).
//  3. Windows → pwsh.
//  4. Otherwise → bash.
//
// Operators who want a specific shell should pass --shell explicitly.
func detectShell() string {
	if sh := strings.TrimSpace(os.Getenv("SHELL")); sh != "" {
		switch strings.ToLower(filepath.Base(sh)) {
		case "bash":
			return "bash"
		case "zsh":
			return "zsh"
		case "fish":
			return "fish"
		case "pwsh":
			return "pwsh"
		}
	}
	if os.Getenv("PSModulePath") != "" {
		return "pwsh"
	}
	if runtime.GOOS == "windows" {
		return "pwsh"
	}
	return "bash"
}
