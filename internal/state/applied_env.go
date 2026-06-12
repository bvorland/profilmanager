package state

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// AppliedEnvFile returns the path of the session-scoped record of env
// keys most recently emitted by `pm env apply`. We track the keys (not
// the values) so a follow-up `pm env apply --unset` knows what to
// clear in the operator's shell without us caching any secret material.
func AppliedEnvFile() (string, error) {
	dir, err := sessionsDir()
	if err != nil {
		return "", err
	}
	id, _ := SessionID()
	return filepath.Join(dir, sanitizeID(id)+".env-keys"), nil
}

// GetAppliedEnvKeys returns the sorted, deduplicated list of env keys
// previously applied for this session. Empty (nil, nil) if nothing has
// been applied yet.
func GetAppliedEnvKeys() ([]string, error) {
	path, err := AppliedEnvFile()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read applied env keys: %w", err)
	}
	var keys []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			keys = append(keys, line)
		}
	}
	return dedupSorted(keys), nil
}

// SetAppliedEnvKeys records keys as the env vars most recently applied
// for this session. Pass an empty slice (or call ClearAppliedEnvKeys) to
// signal "nothing applied".
func SetAppliedEnvKeys(keys []string) error {
	path, err := AppliedEnvFile()
	if err != nil {
		return err
	}
	keys = dedupSorted(keys)
	if len(keys) == 0 {
		return ClearAppliedEnvKeys()
	}
	return lockedAtomicWrite(path, []byte(strings.Join(keys, "\n")+"\n"))
}

// ClearAppliedEnvKeys removes the applied-env-keys marker for the
// current session. No-op if the file is already absent.
func ClearAppliedEnvKeys() error {
	path, err := AppliedEnvFile()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove applied env keys: %w", err)
	}
	return nil
}

func dedupSorted(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
