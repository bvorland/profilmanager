package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/bvorland/profilmanager/internal/core"
	"github.com/bvorland/profilmanager/internal/runner"
	"github.com/bvorland/profilmanager/internal/state"
)

// ---------- pm env apply ----------
//
// Emits shell-evaluable env for the operator's current shell. The
// emitted block has two sections:
//
//  1. UNSET — clears every key previously applied for this session (so
//     re-applying is idempotent and switching profiles cleans up).
//  2. EXPORT — sets every key contributed by the target profile.
//
// Refs (op://, wincred://, dotenv://) are NEVER resolved into the
// printed output: writing secrets to a shell's history (or shouldering
// into a process listing) is a per-host privacy hazard. The default
// behaviour is to refuse with a clear error directing the user to
// `pm exec` / `pm shell`. `--allow-unresolved-refs` lets a power user
// emit the literal ref string instead, which the child process can
// resolve itself with `op run` or similar.
//
// `--unset` skips the export block and emits only the cleanup — useful
// when leaving a profile.

func newEnvCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "env",
		Short: "Profile env helpers (apply)",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(newEnvApplyCmd())
	return cmd
}

func newEnvApplyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apply [<name>]",
		Short: "Emit shell-evaluable env for the current shell",
		Long: `Emit shell-evaluable env that sets every var the profile contributes.

The emitted block begins with an UNSET stanza that clears every key
previously applied for this session, so re-evaluating apply (or applying
a different profile) is idempotent.

Resolved secret refs are NEVER written to the output — apply refuses if
the profile contains any [[env]] entry with a ref. Use ` + "`pm exec`" + ` or
` + "`pm shell`" + ` when you need refs resolved into a process env.

Usage:

  bash / zsh:   eval "$(pm env apply <name> --shell bash)"
  fish:         pm env apply <name> --shell fish | source
  pwsh:         pm env apply <name> --shell pwsh | Invoke-Expression
  cmd:          pm env apply <name> --shell cmd > %TEMP%\pm-env.bat && call %TEMP%\pm-env.bat

With --unset, emits ONLY the cleanup block — useful when leaving a
profile.

With no <name>, an interactive terminal prompts for a profile.`,
		Args:              cobra.MaximumNArgs(1),
		RunE:              runEnvApply,
		ValidArgsFunction: maxOneProfileCompletion,
	}
	cmd.Flags().String("shell", "", "shell flavor: bash|zsh|pwsh|fish|cmd (auto-detected if empty)")
	cmd.Flags().String("profile", "", "profile name (alias for the positional argument)")
	cmd.Flags().Bool("unset", false, "emit ONLY the unset stanza (no export block)")
	cmd.Flags().Bool("allow-unresolved-refs", false, "do not refuse on secret refs; emit the literal ref string as the value")
	return cmd
}

func runEnvApply(cmd *cobra.Command, args []string) error {
	shell, _ := cmd.Flags().GetString("shell")
	shell = canonicalShell(shell)
	if shell == "" {
		shell = detectShell()
	}

	unsetOnly, _ := cmd.Flags().GetBool("unset")
	allowRefs, _ := cmd.Flags().GetBool("allow-unresolved-refs")

	// --unset is independent of any profile: it only needs the
	// previously-applied key list to know what to clear.
	if unsetOnly {
		prev, err := state.GetAppliedEnvKeys()
		if err != nil {
			return emitError(cmd, err)
		}
		if err := emitUnsetBlock(cmd.OutOrStdout(), shell, prev); err != nil {
			return emitError(cmd, errInvalidUsage("%v", err))
		}
		if err := state.ClearAppliedEnvKeys(); err != nil {
			return emitError(cmd, err)
		}
		return nil
	}

	flag, _ := cmd.Flags().GetString("profile")
	name, err := resolveProfileArgWithFlag(cmd, args, flag)
	if err != nil {
		return emitProfileArgError(cmd, err)
	}
	profile, _, _, err := state.ResolveTargetProfile(name)
	if err != nil {
		return emitError(cmd, err)
	}

	plan, cleanup, err := runner.Compose(context.Background(), profile, runner.ComposeOpts{ResolveSecrets: false})
	defer cleanup()
	if err != nil {
		return emitError(cmd, err)
	}

	if len(plan.Refs) > 0 && !allowRefs {
		keys := make([]string, 0, len(plan.Refs))
		for _, r := range plan.Refs {
			keys = append(keys, r.Key)
		}
		return emitError(cmd, errInvalidUsage(
			"profile %q has secret refs (%s); refusing to print them to a shell — use `pm exec --profile %s -- <cmd>` or `pm shell %s` instead, or pass --allow-unresolved-refs to emit literal ref strings",
			profile.Name, strings.Join(keys, ", "), profile.Name, profile.Name,
		))
	}

	// Stitch in the literal-ref passthrough if the caller opted in.
	export := copyMap(plan.Env)
	if allowRefs {
		for _, r := range plan.Refs {
			export[r.Key] = r.Ref
		}
	}

	// Synthesize PM_ACTIVE_PROFILE so dashboard/doctor/whoami can detect
	// that a profile is active in this shell. (`pm shell` and `pm exec`
	// already inject this into the child process env — we do the same
	// here so eval'ing the env-apply block from a host shell has the
	// same effect.) Included in the tracked key set so --unset clears
	// it and switching profiles is idempotent.
	export["PM_ACTIVE_PROFILE"] = profile.Name
	export["PM_ACTIVE_PROFILE_EMOJI"] = core.ColorEmoji(profile.Color)
	export["PM_ACTIVE_PROFILE_BG"] = core.ColorHex(profile.Color)

	// cmd has no robust escape for arbitrary string values — refuse
	// any value that would break the BAT file. Switching to pwsh is the
	// documented mitigation.
	if shell == "cmd" {
		for k, v := range export {
			if strings.ContainsAny(v, "\r\n\"%^&<>|") {
				return emitError(cmd, errInvalidUsage(
					"value of %s contains characters cmd.exe cannot safely quote (%q); rerun with --shell pwsh",
					k, v,
				))
			}
			if strings.ContainsAny(k, "= \t") {
				return emitError(cmd, errInvalidUsage("env key %q is not safe for cmd.exe", k))
			}
		}
	}

	out := cmd.OutOrStdout()

	// Header comment so operators see what generated the block.
	if err := writeBanner(out, shell, profile.Name); err != nil {
		return emitError(cmd, err)
	}

	prev, err := state.GetAppliedEnvKeys()
	if err != nil {
		return emitError(cmd, err)
	}
	if err := emitUnsetBlock(out, shell, prev); err != nil {
		return emitError(cmd, errInvalidUsage("%v", err))
	}

	// Export block — stable key order so diffs across runs are quiet.
	keys := make([]string, 0, len(export))
	for k := range export {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		line, err := exportLine(shell, k, export[k])
		if err != nil {
			return emitError(cmd, errInvalidUsage("%v", err))
		}
		fmt.Fprintln(out, line)
	}

	if err := state.SetAppliedEnvKeys(keys); err != nil {
		return emitError(cmd, err)
	}
	if envApplyShouldShowBanner(shell, out) {
		wrapperLoaded := os.Getenv("PM_SHELL_INIT_LOADED") != ""
		printEnvApplyFallbackBanner(cmd.ErrOrStderr(), profile.Name, wrapperLoaded)
	}
	return nil
}

func envApplyShouldShowBanner(shell string, stdout io.Writer) bool {
	if shell != "pwsh" && shell != "powershell" {
		return false
	}
	f, ok := stdout.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

func printEnvApplyFallbackBanner(errOut io.Writer, name string, wrapperLoaded bool) {
	top, mid, bottom, side := "╔", "═", "╚", "║"
	topRight, bottomRight := "╗", "╝"
	if !colorsOn {
		top, mid, bottom, side = "+", "-", "+", "|"
		topRight, bottomRight = "+", "+"
	}
	line := strings.Repeat(mid, 68)

	var headline string
	if wrapperLoaded {
		headline = styleWarn.Render("⚠️   Wrapper loaded but bypassed — script NOT applied.")
	} else {
		headline = styleWarn.Render("⚠️   Script emitted but NOT applied to your current shell.")
	}

	fmt.Fprintln(errOut)
	fmt.Fprintf(errOut, "%s%s%s\n", top, line, topRight)
	fmt.Fprintf(errOut, "%s  %s       %s\n", side, headline, side)
	fmt.Fprintf(errOut, "%s%s%s\n", bottom, line, bottomRight)
	fmt.Fprintln(errOut)
	if wrapperLoaded {
		fmt.Fprintln(errOut, "  The pm shell wrapper IS loaded, but you invoked pm.exe directly")
		fmt.Fprintln(errOut, "  (bypassing the function). The generated script was printed but")
		fmt.Fprintln(errOut, "  NOT applied to your shell.")
		fmt.Fprintln(errOut)
		fmt.Fprintln(errOut, "  Re-run via the wrapper (no `.exe`, no path):")
		fmt.Fprintf(errOut, "      pm env apply %s\n", name)
		fmt.Fprintln(errOut)
		fmt.Fprintln(errOut, "  Or apply this output manually:")
		fmt.Fprintf(errOut, "      pm env apply %s | Invoke-Expression\n", name)
	} else {
		fmt.Fprintln(errOut, "  The generated script was printed but NOT applied. Tools (az, gh,")
		fmt.Fprintln(errOut, "  kubectl, copilot, ...) will still see your host config.")
		fmt.Fprintln(errOut)
		fmt.Fprintln(errOut, "  To apply NOW (one-time):")
		fmt.Fprintf(errOut, "      pm env apply %s | Invoke-Expression\n", name)
		fmt.Fprintln(errOut)
		fmt.Fprintln(errOut, "  To make `pm env apply` automatic, add to your $PROFILE:")
		fmt.Fprintln(errOut, "      pm shell-init pwsh | Out-String | Invoke-Expression")
		fmt.Fprintln(errOut, "  Then reload:  . $PROFILE   (or open a fresh pwsh tab)")
	}
	fmt.Fprintln(errOut)
}

func writeBanner(out io.Writer, shell, profileName string) error {
	switch shell {
	case "bash", "zsh", "fish":
		_, err := fmt.Fprintf(out, "# pm env apply %s (shell=%s) — generated; eval to apply\n", profileName, shell)
		return err
	case "pwsh", "powershell":
		_, err := fmt.Fprintf(out, "# pm env apply %s (shell=%s) — generated; Invoke-Expression to apply\n", profileName, shell)
		return err
	case "cmd", "cmd.exe":
		_, err := fmt.Fprintf(out, "REM pm env apply %s (shell=%s) -- generated\n", profileName, shell)
		return err
	default:
		return fmt.Errorf("unsupported shell %q (want one of: bash, zsh, pwsh, fish, cmd)", shell)
	}
}

func emitUnsetBlock(out io.Writer, shell string, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	switch shell {
	case "bash", "zsh":
		for _, k := range keys {
			fmt.Fprintf(out, "unset %s\n", k)
		}
	case "fish":
		for _, k := range keys {
			fmt.Fprintf(out, "set -e %s\n", k)
		}
	case "pwsh", "powershell":
		for _, k := range keys {
			fmt.Fprintf(out, "Remove-Item -ErrorAction SilentlyContinue env:%s\n", k)
		}
	case "cmd", "cmd.exe":
		for _, k := range keys {
			// `set FOO=` (no value) clears FOO in cmd.exe.
			fmt.Fprintf(out, "set %s=\n", k)
		}
	default:
		return fmt.Errorf("unsupported shell %q (want one of: bash, zsh, pwsh, fish, cmd)", shell)
	}
	return nil
}

// exportLine renders a single env export for the given shell. Quoting
// rules per shell — kept centralized so we have one place to audit when
// a new bug surfaces.
func exportLine(shell, key, value string) (string, error) {
	switch shell {
	case "bash", "zsh":
		return fmt.Sprintf("export %s=%s", key, posixSingleQuote(value)), nil
	case "fish":
		return fmt.Sprintf("set -gx %s %s", key, fishQuote(value)), nil
	case "pwsh", "powershell":
		return fmt.Sprintf("$env:%s = %s", key, pwshSingleQuote(value)), nil
	case "cmd", "cmd.exe":
		return fmt.Sprintf("set %s=%s", key, value), nil
	default:
		return "", fmt.Errorf("unsupported shell %q (want one of: bash, zsh, pwsh, fish, cmd)", shell)
	}
}

// posixSingleQuote wraps v in single quotes, escaping any inner ' via
// the classic POSIX `'\”` dance. The result is safe for bash/zsh.
func posixSingleQuote(v string) string {
	return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'"
}

// fishQuote wraps v in single quotes, escaping inner ' and \. Fish
// single-quoted strings only need these two escapes; \n inside the
// literal is fine (becomes a literal newline in the resulting var).
func fishQuote(v string) string {
	s := strings.ReplaceAll(v, `\`, `\\`)
	s = strings.ReplaceAll(s, "'", `\'`)
	return "'" + s + "'"
}

// pwshSingleQuote wraps v in single quotes. PowerShell single-quoted
// strings only escape ' (doubled). $ and backtick are literals inside
// single quotes — that's the whole point of using them here.
func pwshSingleQuote(v string) string {
	return "'" + strings.ReplaceAll(v, "'", "''") + "'"
}

// canonicalShell normalises operator-supplied shell names. Returns ""
// when unset.
func canonicalShell(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "powershell":
		return "pwsh"
	case "cmd.exe":
		return "cmd"
	default:
		return s
	}
}

func copyMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
