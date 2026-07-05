package cli

import (
	"os"
	"strings"

	"github.com/bvorland/profilmanager/internal/state"
)

type activeProfileSource string

const (
	activeProfileSourceShell   activeProfileSource = "shell"
	activeProfileSourceSession activeProfileSource = "session"
)

// resolveActiveProfile returns the profile name currently in effect for pm
// commands. PM_ACTIVE_PROFILE (shell-applied) wins; otherwise we fall back to
// the session-scoped marker set by `pm switch`.
func resolveActiveProfile() (name string, source activeProfileSource, err error) {
	if name := strings.TrimSpace(os.Getenv("PM_ACTIVE_PROFILE")); name != "" {
		return name, activeProfileSourceShell, nil
	}
	name, _, err = state.GetActiveProfile()
	if err != nil {
		return "", "", err
	}
	if name == "" {
		return "", "", nil
	}
	return name, activeProfileSourceSession, nil
}
