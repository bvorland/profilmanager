package cli

import (
	"github.com/spf13/cobra"

	"github.com/bvorland/profilmanager/internal/state"
)

func newCopilotCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "copilot [name]",
		Short: "Launch GitHub Copilot CLI inside a pm profile",
		Long: `Launch the GitHub Copilot CLI with a profile's environment applied.

This is a shortcut for:

  pm exec <name> -- copilot

Use this when Copilot CLI should see profile-specific Azure, GitHub, kube,
git, and env settings instead of the host shell's default config. It avoids
the common trap where raw ` + "`copilot`" + ` starts against host config even though
you meant to work inside a sandboxed pm profile.

With no [name] in an interactive terminal, pm opens the profile picker. In
non-interactive mode, pass a profile name explicitly.`,
		Args:              cobra.MaximumNArgs(1),
		RunE:              runCopilot,
		ValidArgsFunction: profileNameCompletions,
	}
	return cmd
}

func runCopilot(cmd *cobra.Command, args []string) error {
	name, err := resolveProfileArg(cmd, args)
	if err != nil {
		return emitProfileArgError(cmd, err)
	}
	profile, _, _, err := state.ResolveTargetProfile(name)
	if err != nil {
		return emitError(cmd, err)
	}
	return runChildInProfile(
		cmd,
		profile,
		[]string{"copilot"},
		0,
		"copilot CLI not found in PATH. Install from https://github.com/github/copilot-cli",
	)
}
