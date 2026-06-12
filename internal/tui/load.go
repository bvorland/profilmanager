package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bvorland/profilmanager/internal/core"
)

// loadProfiles scans the profile directory and returns every valid TOML
// profile, sorted by Name. Invalid files are skipped but their errors
// are returned alongside the valid ones so the UI can surface them in a
// toast instead of failing the whole view.
func loadProfiles() ([]*core.Profile, []error, error) {
	dir, err := core.ProfilesDir()
	if err != nil {
		return nil, nil, fmt.Errorf("locate profiles dir: %w", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("read profiles dir: %w", err)
	}
	var profiles []*core.Profile
	var loadErrs []error
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".toml") {
			continue
		}
		// Skip pm temp files left over from a crashed atomic write.
		if strings.HasPrefix(name, ".pm-") {
			continue
		}
		path := filepath.Join(dir, name)
		p, err := core.Load(path)
		if err != nil {
			loadErrs = append(loadErrs, err)
			continue
		}
		profiles = append(profiles, p)
	}
	sort.Slice(profiles, func(i, j int) bool {
		return strings.ToLower(profiles[i].Name) < strings.ToLower(profiles[j].Name)
	})
	return profiles, loadErrs, nil
}
