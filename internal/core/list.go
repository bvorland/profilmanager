package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ListedProfile is one successfully loaded profile plus metadata from its
// on-disk TOML file.
type ListedProfile struct {
	Profile *Profile
	Path    string
	ModTime time.Time
}

// ListProfiles scans ProfilesDir and loads every profile TOML file. Bad
// files are skipped and returned as loadErrs so callers can still render a
// useful partial list.
func ListProfiles() ([]ListedProfile, []error, error) {
	dir, err := ProfilesDir()
	if err != nil {
		return nil, nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("read profiles dir: %w", err)
	}

	var profiles []ListedProfile
	var loadErrs []error
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".toml") {
			continue
		}
		if strings.HasPrefix(name, ".pm-") {
			continue
		}
		path := filepath.Join(dir, name)
		info, statErr := e.Info()
		if statErr != nil {
			loadErrs = append(loadErrs, fmt.Errorf("stat profile %s: %w", path, statErr))
			continue
		}
		p, loadErr := Load(path)
		if loadErr != nil {
			loadErrs = append(loadErrs, loadErr)
			continue
		}
		profiles = append(profiles, ListedProfile{
			Profile: p,
			Path:    path,
			ModTime: info.ModTime(),
		})
	}
	sort.Slice(profiles, func(i, j int) bool {
		return strings.ToLower(profiles[i].Profile.Name) < strings.ToLower(profiles[j].Profile.Name)
	})
	return profiles, loadErrs, nil
}

// ListProfilesByModTime returns successfully loaded profiles, newest TOML
// modification time first. A non-positive limit returns all profiles.
func ListProfilesByModTime(limit int) ([]ListedProfile, []error, error) {
	profiles, loadErrs, err := ListProfiles()
	if err != nil {
		return nil, nil, err
	}
	sort.SliceStable(profiles, func(i, j int) bool {
		return profiles[i].ModTime.After(profiles[j].ModTime)
	})
	if limit > 0 && len(profiles) > limit {
		profiles = profiles[:limit]
	}
	return profiles, loadErrs, nil
}
