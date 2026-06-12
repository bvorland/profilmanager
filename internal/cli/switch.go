package cli

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/bvorland/profilmanager/internal/state"
)

// ---------- pm switch ----------
//
// Records `name` as the active profile for the current session. Does
// NOT mutate any current shell — the printed message tells the operator
// exactly how to actually activate the env (the four-mode honest model
// from the architecture memo).
//
// Also updates the operator-global "last profile" pointer so `pm switch
// -` (future UX) can flip back without a name.

func newSwitchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "switch <name>",
		Short: "Set the active profile for this session (metadata only)",
		Long: `Set <name> as the active profile for the current session.

This writes a session-scoped marker file (see ` + "`pm doctor`" + ` →
session-id-source). It does NOT mutate the calling shell's environment —
that is impossible for a child process to do, and pretending otherwise
is what the four-mode honest model exists to prevent.

To actually apply the profile env, pick one:

  pm exec <name> -- <cmd>         # one-shot, agents/scripts/CI
  pm shell <name>                 # fresh interactive shell
  eval "$(pm env apply <name>)"   # mutate current shell (bash/zsh)
  pm shell-init --with-shims      # route raw az/azd/gh/kubectl/git`,
		Args:              cobra.MaximumNArgs(1),
		RunE:              runSwitch,
		ValidArgsFunction: profileNameCompletions,
	}
	cmd.Flags().Bool("quiet", false, "skip the activation hint (script-friendly)")
	return cmd
}

func runSwitch(cmd *cobra.Command, args []string) error {
	name, err := resolveProfileArg(cmd, args)
	if err != nil {
		return emitProfileArgError(cmd, err)
	}

	// Validate + load the profile so we fail fast on typos rather than
	// store a dangling pointer.
	if _, _, _, err := state.ResolveTargetProfile(name); err != nil {
		return emitError(cmd, errInvalidUsage("%v", err))
	}

	if err := state.SetActiveProfile(name); err != nil {
		return emitError(cmd, err)
	}
	if err := state.SetLastProfile(name); err != nil {
		// non-fatal: last-profile is convenience, not correctness
		fmt.Fprintln(cmd.ErrOrStderr(), styleWarn.Render("warn:"), "could not update last-profile:", err)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintln(out, styleOK.Render("active:"), name)

	quiet, _ := cmd.Flags().GetBool("quiet")
	if quiet {
		return nil
	}

	// Honest activation hint. We pick one example tailored to the OS so
	// the most common path is obvious; the rest live in --help.
	hint := activationHint(name)
	for _, line := range hint {
		fmt.Fprintln(out, styleDim.Render(line))
	}
	return nil
}

// activationHint returns the platform-appropriate "now do this" lines.
// Kept short — operators tune it out fast if it's a wall of text.
func activationHint(name string) []string {
	common := []string{
		"  switch is metadata only — your current shell is unchanged.",
		"  to apply the profile env, pick one:",
		"    pm exec " + name + " -- <cmd>           # one-shot",
		"    pm shell " + name + "                   # fresh interactive shell",
	}
	if runtime.GOOS == "windows" {
		return append(common,
			"    pm env apply "+name+" --shell pwsh | Invoke-Expression",
		)
	}
	return append(common,
		`    eval "$(pm env apply `+name+`)"   # mutate this shell`,
	)
}
