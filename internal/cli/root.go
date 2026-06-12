// Package cli wires the Cobra command tree for the pm binary.
//
// Bare `pm` prints a friendly dashboard. `pm tui` launches the interactive
// Bubble Tea TUI. Other verbs are non-TUI surface for scripts and
// agents.
package cli

import (
	"github.com/spf13/cobra"
)

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "pm",
		Short: "profilmanager — multi-environment developer profile manager",
		Long: "pm manages multiple developer environments (Azure subs, GitHub accounts,\n" +
			"kubectl contexts, git identity, secrets) from one profile model.\n\n" +
			"Run with no subcommand to show the profile dashboard.",
		SilenceUsage:  true,
		SilenceErrors: true,
		// Bare `pm` shows the dashboard. `pm --help` and `pm --version` still
		// work because cobra short-circuits those flags before RunE.
		Args: cobra.NoArgs,
		RunE: runDashboard,
	}

	// --no-color is persistent so subcommands and the TUI agree. NO_COLOR
	// env still wins independently inside tui.Run.
	root.PersistentFlags().Bool("no-color", false, "Disable ANSI color output (NO_COLOR env also honored)")

	root.Version = Version
	root.SetVersionTemplate(versionTemplate())

	root.AddCommand(newVersionCmd())
	root.AddCommand(newImportMJCmd())
	root.AddCommand(newTUICmd())
	root.AddCommand(newWhoamiCmd())

	// CLI surface — functional verbs.
	root.AddCommand(newProfileCmd())
	root.AddCommand(newSessionCmd())
	root.AddCommand(newShellInitCmd())
	root.AddCommand(newDoctorCmd())
	root.AddCommand(newCompletionCmd())

	// CLI surface — skeleton stubs (exit 64 until wired).
	root.AddCommand(newSwitchCmd())
	root.AddCommand(newEnvCmd())
	root.AddCommand(newPromptCmd())
	root.AddCommand(newExecCmd())
	root.AddCommand(newCopilotCmd())
	root.AddCommand(newShellCmd())
	root.AddCommand(newMCPCmd())
	root.AddCommand(newSecretCmd())
	root.AddCommand(newStatusLineCmd())

	return root
}

// Execute runs the root command. Returns a non-nil error if the command
// failed; the binary's main is responsible for translating that into an
// exit code.
func Execute() error {
	initConsole()
	return newRootCmd().Execute()
}
