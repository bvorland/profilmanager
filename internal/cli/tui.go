package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"

	"github.com/spf13/cobra"

	"github.com/bvorland/profilmanager/internal/tui"
)

// runTUI is the shared entry point for both bare `pm` and `pm tui`.
// Reads --no-color and --doctor from the calling command's flag set and
// translates errors into operator-friendly messages.
func runTUI(cmd *cobra.Command, _ []string) error {
	noColor, _ := cmd.Flags().GetBool("no-color")
	doctor, _ := cmd.Flags().GetBool("doctor")

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	opts := tui.RunOptions{ForceNoColor: noColor}
	if doctor {
		opts.StartView = "doctor"
	}
	if err := tui.Run(ctx, opts); err != nil {
		if errors.Is(err, tui.ErrNotATerminal) {
			fmt.Fprintln(cmd.ErrOrStderr(),
				"pm tui requires an interactive terminal. Run `pm --help` to see non-TUI commands.")
			return err
		}
		return err
	}
	return nil
}

func newTUICmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "tui",
		Short: "Launch the interactive TUI",
		Long: "Launches the interactive Bubble Tea TUI for browsing, editing, and\n" +
			"switching profiles. `pm` with no subcommand shows a non-interactive dashboard.\n\n" +
			"Honors NO_COLOR (env) and --no-color. Refuses to start when stdout is\n" +
			"not a TTY (so piped invocations don't get garbled escape sequences).",
		Args: cobra.NoArgs,
		RunE: runTUI,
	}
	c.Flags().Bool("doctor", false, "Land on the doctor view instead of the profile list")
	return c
}
