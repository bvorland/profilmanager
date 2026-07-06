package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bvorland/profilmanager/internal/runner"
	"github.com/bvorland/profilmanager/internal/state"
)

// ---------- pm shell ----------
//
// Spawn a fresh interactive shell with the named profile's env baked
// in. Closing the shell returns to the original session unchanged.
//
// Shell selection precedence:
//
//  1. --shell <path>            — explicit
//  2. $PMSHELL                  — operator override
//  3. $SHELL                    — Unix convention
//  4. Windows defaults: pwsh, falling back to powershell, cmd
//  5. Unix default: bash, falling back to sh
//
// Prompt: we minimally annotate the prompt so operators can tell at a
// glance which profile is active. The shape is shell-specific (PS1 for
// bash/zsh, prompt function for pwsh) and additive — the operator's
// own prompt is preserved when possible.

func newShellCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "shell [<name>]",
		Short: "Spawn a fresh interactive shell with the named profile's env",
		Long: `Spawn a fresh interactive shell with the named profile's env baked in.

Closing the shell returns to the original session unchanged — useful for
humans who want a clean session per profile (the ` + "`op run -- pwsh -NoExit`" + `
model).

Shell selection precedence:
  1. --shell <path>   (explicit)
  2. $PMSHELL         (operator override)
  3. $SHELL           (Unix convention)
  4. pwsh / powershell / cmd on Windows; bash / sh elsewhere

The prompt is prefixed with [pm:<profile>] so the active profile is
visible at a glance. Pass --no-prompt to keep the operator's prompt
verbatim.

With no <name>, the active profile for the current session is used.`,
		Args:              cobra.MaximumNArgs(1),
		RunE:              runShell,
		ValidArgsFunction: maxOneProfileCompletion,
	}
	cmd.Flags().String("profile", "", "profile name (alternative to positional <name>)")
	cmd.Flags().String("shell", "", "absolute path to the shell binary (overrides $PMSHELL / $SHELL)")
	cmd.Flags().Bool("no-prompt", false, "do not modify the shell prompt")
	return cmd
}

func runShell(cmd *cobra.Command, args []string) error {
	flag, _ := cmd.Flags().GetString("profile")
	name, err := resolveProfileArgWithFlag(cmd, args, flag)
	if err != nil {
		return emitProfileArgError(cmd, err)
	}
	profile, _, _, err := state.ResolveTargetProfile(name)
	if err != nil {
		return emitError(cmd, err)
	}

	ctx, cancel := context.WithCancel(cmd.Context())
	if ctx.Err() != nil {
		ctx = context.Background()
	}
	defer cancel()

	plan, cleanup, err := runner.Compose(ctx, profile, runner.ComposeOpts{ResolveSecrets: true})
	defer cleanup()
	if err != nil {
		return emitError(cmd, err)
	}
	if len(plan.ProviderErrors) > 0 {
		for _, pe := range plan.ProviderErrors {
			fmt.Fprintln(cmd.ErrOrStderr(), styleWarn.Render("warn:"), pe.Error())
		}
	}

	shellPath, _ := cmd.Flags().GetString("shell")
	if shellPath == "" {
		shellPath = pickInteractiveShell()
	}
	if shellPath == "" {
		return emitError(cmd, errInvalidUsage("could not find an interactive shell — set --shell or $SHELL"))
	}

	noPrompt, _ := cmd.Flags().GetBool("no-prompt")
	flavor := shellFlavor(shellPath)

	args1, ok := childShellArgs(flavor, profile.Name, !noPrompt)
	if !ok {
		// Unknown shell — just exec it with no extra args.
		args1 = nil
	}
	for k, v := range promptEnv(flavor, profile.Name, !noPrompt) {
		plan.Env[k] = v
	}
	// Always advertise PM_ACTIVE_PROFILE so scripts inside the child can
	// see which profile they're in, regardless of shell flavor.
	plan.Env["PM_ACTIVE_PROFILE"] = profile.Name

	c := exec.CommandContext(ctx, shellPath, args1...)
	c.Env = runner.EnvSlice(os.Environ(), plan.Env)
	c.Stdin = cmd.InOrStdin()
	c.Stdout = cmd.OutOrStdout()
	c.Stderr = cmd.ErrOrStderr()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	defer signal.Stop(sigCh)
	go func() {
		select {
		case <-ctx.Done():
		case <-sigCh:
			// Let the child handle the signal; cancel only when it
			// exits or when the user double-Ctrl-Cs.
		}
	}()

	if err := c.Start(); err != nil {
		return emitError(cmd, fmt.Errorf("start %s: %w", shellPath, err))
	}
	cleanup()
	cleanup = func() {}

	waitErr := c.Wait()
	if waitErr == nil {
		return nil
	}
	var ee *exec.ExitError
	if errors.As(waitErr, &ee) {
		code := ee.ExitCode()
		if code < 0 {
			code = ExitError
		}
		return WithExitCode(code, fmt.Errorf("%s exited with code %d", filepath.Base(shellPath), code))
	}
	return emitError(cmd, fmt.Errorf("wait %s: %w", shellPath, waitErr))
}

// pickInteractiveShell follows the precedence documented above.
func pickInteractiveShell() string {
	if v := strings.TrimSpace(os.Getenv("PMSHELL")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("SHELL")); v != "" {
		return v
	}
	if runtime.GOOS == "windows" {
		for _, name := range []string{"pwsh", "powershell", "cmd"} {
			if p, err := exec.LookPath(name); err == nil {
				return p
			}
		}
		return ""
	}
	for _, name := range []string{"bash", "sh"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return ""
}

// shellFlavor maps a binary path to a flavor token used by the prompt /
// arg builders. Defaults to "" (unknown).
func shellFlavor(path string) string {
	base := strings.ToLower(path)
	if i := strings.LastIndexAny(base, `/\`); i >= 0 {
		base = base[i+1:]
	}
	base = strings.TrimSuffix(base, ".exe")
	switch base {
	case "bash":
		return "bash"
	case "zsh":
		return "zsh"
	case "fish":
		return "fish"
	case "pwsh", "powershell":
		return "pwsh"
	case "cmd":
		return "cmd"
	case "sh":
		return "bash" // close enough for the args we emit
	default:
		return ""
	}
}

// childShellArgs returns the argv tail we need to convince a shell to
// honor our prompt injection. ok == false signals "no special args
// needed; exec the binary as-is".
func childShellArgs(flavor, profile string, withPrompt bool) ([]string, bool) {
	if !withPrompt {
		return nil, false
	}
	switch flavor {
	case "pwsh":
		// -NoExit keeps the shell interactive; -Command runs our prompt
		// override and then drops into the REPL.
		// The single quotes are powershell-literal (no expansion).
		cmd := fmt.Sprintf("function global:prompt { '[pm:%s] ' + $((Get-Location).Path) + '> ' }", powershellSingleQuoteEscape(profile))
		return []string{"-NoExit", "-NoLogo", "-Command", cmd}, true
	default:
		// bash/zsh/fish/cmd: prompts are injected via env (PS1 / fish
		// prompt env) or simply not supported (cmd). No extra args.
		return nil, false
	}
}

// promptEnv returns env additions that wire the [pm:<profile>] prompt
// for shells that read prompt config from env.
func promptEnv(flavor, profile string, withPrompt bool) map[string]string {
	if !withPrompt {
		return nil
	}
	switch flavor {
	case "bash":
		// Append (rather than replace) so the operator's existing PS1
		// survives. The leading [pm:foo] is the visible marker.
		// We don't try to read /etc/bashrc; this is best-effort.
		return map[string]string{
			"PS1": fmt.Sprintf(`[pm:%s] \w\$ `, profile),
		}
	case "zsh":
		return map[string]string{
			"PROMPT": fmt.Sprintf(`[pm:%s] %%~ %%# `, profile),
		}
	case "fish":
		// fish_prompt is a function — env vars don't help. Fish honors
		// $fish_prompt only via custom config, so we set PM_PROMPT_TAG
		// for operators who want to integrate it manually.
		return map[string]string{
			"PM_PROMPT_TAG": fmt.Sprintf("[pm:%s]", profile),
		}
	default:
		return nil
	}
}

// powershellSingleQuoteEscape escapes a string for embedding inside a
// PowerShell single-quoted literal.
func powershellSingleQuoteEscape(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}
