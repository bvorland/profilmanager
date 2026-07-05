package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/bvorland/profilmanager/internal/core"
	"github.com/bvorland/profilmanager/internal/state"
)

// ---------- pm profile rename ----------

func newProfileRenameCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rename <old> <new>",
		Short: "Rename a profile and its name-derived storage",
		Long: `Rename a profile so its storage follows the new name.

This renames the on-disk profile file (<old>.toml → <new>.toml), updates the
profile's name field, and rewrites the default-pattern azure/azd config
directories (~/.azure-<name>, ~/.azd-<name>) to match. Custom config-dir paths
are left untouched.

By default the name-derived directories that hold cached logins/state are also
moved so they follow the rename:

  AZURE_CONFIG_DIR   ~/.azure-<name>       (only when it is the default pattern)
  AZD_CONFIG_DIR     ~/.azd-<name>         (only when it is the default pattern)
  gh state dir       <state-dir>/gh/<name>
  kube state dir     <state-dir>/kube/<name>

Pass --no-move-dirs to leave those directories in place; the providers will
recreate fresh (empty) directories on the next apply, so you would then need to
re-login.

The session's active-profile marker and the operator "last profile" pointer are
updated when they referenced the old name.`,
		Args:              cobra.ExactArgs(2),
		RunE:              runProfileRename,
		ValidArgsFunction: profileNameCompletions,
	}
	cmd.Flags().Bool("no-move-dirs", false, "repoint config-dir paths without moving the on-disk directories")
	return cmd
}

func runProfileRename(cmd *cobra.Command, args []string) error {
	rawOld, newName := args[0], args[1]

	oldName, err := core.ResolveProfileName(rawOld)
	if err != nil {
		return emitError(cmd, err)
	}
	if err := core.ValidateName(newName); err != nil {
		return emitError(cmd, errInvalidUsage("%v", err))
	}
	if oldName == newName {
		fmt.Fprintln(cmd.OutOrStdout(), styleDim.Render("old and new names are identical — nothing to do"))
		return nil
	}

	noMove, _ := cmd.Flags().GetBool("no-move-dirs")

	res, err := core.RenameProfile(oldName, newName, !noMove)
	if err != nil {
		return emitError(cmd, err)
	}
	if err := state.RenameProfileMarkers(oldName, newName); err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), styleWarn.Render("warn:"), "could not update session markers:", err)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "%s %s → %s\n", styleOK.Render("✓ renamed"), oldName, newName)
	fmt.Fprintln(out, styleDim.Render("  "+res.OldPath+"  →  "+res.NewPath))
	for _, m := range res.DirMoves {
		switch m.Status {
		case core.DirMoved:
			fmt.Fprintln(out, styleDim.Render(fmt.Sprintf("  moved %s dir:  %s → %s", m.Label, m.From, m.To)))
		case core.DirTargetExists:
			fmt.Fprintln(cmd.ErrOrStderr(), styleWarn.Render("warn:"),
				fmt.Sprintf("%s target dir already exists, left in place: %s", m.Label, m.To))
		case core.DirFailed:
			fmt.Fprintln(cmd.ErrOrStderr(), styleWarn.Render("warn:"),
				fmt.Sprintf("could not move %s dir %s → %s: %v (re-login or move it manually)", m.Label, m.From, m.To, m.Err))
		}
	}
	return nil
}
