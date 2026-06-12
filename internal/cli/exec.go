package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bvorland/profilmanager/internal/state"
)

// ---------- pm exec ----------
//
// `pm exec` is the workhorse: launch a child process with profile env
// applied. Secrets are resolved IN MEMORY via internal/secrets, copied
// into the child's env block, then zeroed once the child exits.
//
// The verb takes the profile name positionally (or via --profile, for
// compatibility with the old stub help), followed by `--` and the
// command + args. We refuse to invoke through a shell — no `sh -c`, no
// `cmd /c` — exactly the args you pass are exactly the args the child
// sees, so secrets cannot leak via word-splitting / glob expansion.
//
// Exit propagation: the child's exit code is the verb's exit code.
// Timeouts: --timeout accepts any time.ParseDuration value; 0 (the
// default) means "no timeout" so interactive commands aren't surprise-killed.

func newExecCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "exec [<profile>] -- <cmd> [args...]",
		Short: "Run a subprocess with the named profile's env applied",
		Long: `Run <cmd> with the named profile's env vars (literal + resolved secret
refs) injected into its environment.

The first positional argument (before --) is the profile name. If
omitted in an interactive terminal, pm prompts for a profile. Pass
--profile <name> as an alternative.

Examples:

  pm exec dev -- az group list
  pm exec dev --timeout 60s -- az account show
  pm exec --profile dev -- az group list      # same as above

Refs are resolved in this process only — the resolved plaintext is
written into the child's env block and zeroed in our memory once the
child exits. The verb never invokes a shell, so word-splitting cannot
leak secrets via the command line.

If no command is given (no -- separator, no positional after a profile),
the verb errors out — use ` + "`pm shell`" + ` to launch an interactive shell.`,
		Args: cobra.ArbitraryArgs,
		// Cobra needs this to keep the `--` and what's after it in args.
		DisableFlagParsing: false,
		RunE:               runExec,
		ValidArgsFunction:  execProfileCompletions,
	}
	cmd.Flags().String("profile", "", "profile name (alternative to positional <profile>)")
	cmd.Flags().Duration("timeout", 0, "kill the child after this duration; 0 (default) means no timeout")
	return cmd
}

func runExec(cmd *cobra.Command, args []string) error {
	profileFlag, _ := cmd.Flags().GetString("profile")
	timeout, _ := cmd.Flags().GetDuration("timeout")

	// Cobra removes the literal `--` from args but exposes its original
	// position via ArgsLenAtDash() (number of args BEFORE `--`).
	// Returns -1 when no `--` was present. We require `--` to
	// unambiguously separate "profile name" from "child argv".
	dashAt := cmd.ArgsLenAtDash()
	profileArg, child, err := splitExecArgs(args, dashAt)
	if err != nil {
		return emitError(cmd, errInvalidUsage("%v", err))
	}

	if len(child) == 0 {
		return emitError(cmd, errInvalidUsage("no command given — use `pm exec <profile> -- <cmd> [args...]` (or `pm shell <profile>` for an interactive shell)"))
	}

	var profileArgs []string
	if profileArg != "" {
		profileArgs = []string{profileArg}
	}
	name, err := resolveProfileArgWithFlag(cmd, profileArgs, profileFlag)
	if err != nil {
		return emitProfileArgError(cmd, err)
	}
	profile, _, _, err := state.ResolveTargetProfile(name)
	if err != nil {
		return emitError(cmd, err)
	}

	return runChildInProfile(cmd, profile, child, timeout, "")
}

// splitExecArgs separates the optional <profile> positional from the
// child command + args. Cobra removes the literal `--` from args but
// reports its position via [cobra.Command.ArgsLenAtDash] — that's
// dashAt below. When dashAt >= 0, args before dashAt are profile-args
// (0 or 1 element) and args from dashAt onward are the child command.
// When dashAt is -1 (no `--`), we cannot distinguish profile vs.
// child-name reliably and require the operator to add `--`.
func splitExecArgs(args []string, dashAt int) (profile string, child []string, err error) {
	if dashAt < 0 {
		if len(args) == 0 {
			return "", nil, nil
		}
		return "", nil, fmt.Errorf("missing `--` separator before the child command")
	}
	if dashAt > len(args) {
		dashAt = len(args)
	}
	pre := args[:dashAt]
	post := args[dashAt:]
	switch len(pre) {
	case 0:
		return "", post, nil
	case 1:
		return pre[0], post, nil
	default:
		return "", nil, fmt.Errorf("expected at most one positional <profile> before `--`, got %d (%s)", len(pre), strings.Join(pre, " "))
	}
}
