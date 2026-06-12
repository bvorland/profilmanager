package state

import (
	"errors"
	"fmt"
	"os"

	"github.com/bvorland/profilmanager/internal/core"
)

// ErrNoActiveProfile is returned by [ResolveTargetProfile] when the
// caller did not pass an explicit profile name and no active profile is
// recorded for the current session.
var ErrNoActiveProfile = errors.New("no active profile for this session and no --profile/<name> given")

// ResolveTargetProfile is the shared "what profile am I working with?"
// helper for env apply / exec / shell / switch. Resolution order:
//
//  1. explicit (the user passed `--profile foo` or a positional <name>).
//  2. active profile for the current session (per [GetActiveProfile]).
//  3. error.
//
// On success, returns the loaded profile, the absolute path of the
// underlying TOML, and the source label ("explicit" or "active").
//
// The source is exposed so callers can render an honest message about
// where the profile came from (e.g. doctor warnings, `pm switch` echo).
func ResolveTargetProfile(explicit string) (p *core.Profile, path, source string, err error) {
	name := explicit
	source = "explicit"
	if name == "" {
		active, _, gerr := GetActiveProfile()
		if gerr != nil {
			return nil, "", "", fmt.Errorf("read active profile: %w", gerr)
		}
		if active == "" {
			return nil, "", "", ErrNoActiveProfile
		}
		name = active
		source = "active"
	}
	if err := core.ValidateName(name); err != nil {
		return nil, "", "", err
	}
	path, err = core.ProfilePath(name)
	if err != nil {
		return nil, "", "", err
	}
	if _, statErr := os.Stat(path); statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return nil, "", "", fmt.Errorf("profile %q not found at %s", name, path)
		}
		return nil, "", "", statErr
	}
	p, err = core.Load(path)
	if err != nil {
		return nil, "", "", err
	}
	return p, path, source, nil
}
