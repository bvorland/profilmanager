package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/bvorland/profilmanager/internal/core"
	"github.com/bvorland/profilmanager/internal/tui/picker"
)

func resolveProfileArg(cmd *cobra.Command, args []string) (string, error) {
	if len(args) > 1 {
		return "", fmt.Errorf("expected at most one profile name, got %d", len(args))
	}
	if len(args) == 0 {
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return "", fmt.Errorf("profile name required (stdin is not a TTY; provide a name or use --help)")
		}
		name, cancelled, err := picker.PickProfile(cmd.Context())
		if err != nil {
			return "", err
		}
		if cancelled {
			return "", core.ErrCancelled
		}
		return name, nil
	}

	name, err := core.ResolveProfileName(args[0])
	if err == nil {
		return name, nil
	}
	if errors.Is(err, core.ErrAmbiguous) {
		return "", err
	}
	if errors.Is(err, core.ErrNotFound) {
		suggestions := core.SuggestNames(args[0], 3)
		if len(suggestions) > 0 {
			return "", fmt.Errorf("profile %q not found. Did you mean: %s?", args[0], strings.Join(suggestions, ", "))
		}
		return "", fmt.Errorf("profile %q not found", args[0])
	}
	return "", err
}

func resolveProfileArgWithFlag(cmd *cobra.Command, args []string, flag string) (string, error) {
	if flag == "" {
		return resolveProfileArg(cmd, args)
	}
	flagName, err := resolveProfileArg(cmd, []string{flag})
	if err != nil {
		return "", err
	}
	if len(args) == 0 {
		return flagName, nil
	}
	argName, err := resolveProfileArg(cmd, args)
	if err != nil {
		return "", err
	}
	if argName != flagName {
		return "", fmt.Errorf("pass either a positional <name> or --profile, not both with different values")
	}
	return flagName, nil
}

func emitProfileArgError(cmd *cobra.Command, err error) error {
	if errors.Is(err, core.ErrCancelled) {
		return err
	}
	return emitError(cmd, errInvalidUsage("%v", err))
}
