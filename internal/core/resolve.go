package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ResolveProfileName returns the canonical profile name for input.
// 1. Exact match (case-sensitive) returns input as-is.
// 2. Exact match (case-insensitive) returns canonical form.
// 3. Unique prefix match (case-insensitive) returns canonical form.
// 4. Multiple prefix matches returns ErrAmbiguous.
// 5. No match returns ErrNotFound.
func ResolveProfileName(input string) (string, error) {
	names, err := profileNames()
	if err != nil {
		return "", err
	}
	for _, name := range names {
		if name == input {
			return input, nil
		}
	}

	inputFold := strings.ToLower(input)
	var exact []string
	for _, name := range names {
		if strings.ToLower(name) == inputFold {
			exact = append(exact, name)
		}
	}
	if len(exact) == 1 {
		return exact[0], nil
	}
	if len(exact) > 1 {
		return "", ambiguousProfileError(input, exact)
	}

	var prefix []string
	for _, name := range names {
		if strings.HasPrefix(strings.ToLower(name), inputFold) {
			prefix = append(prefix, name)
		}
	}
	if len(prefix) == 1 {
		return prefix[0], nil
	}
	if len(prefix) > 1 {
		return "", ambiguousProfileError(input, prefix)
	}
	return "", fmt.Errorf("%w: %s", ErrNotFound, input)
}

func ambiguousProfileError(input string, matches []string) error {
	sort.Strings(matches)
	return fmt.Errorf("%w: %s matches %s", ErrAmbiguous, input, strings.Join(matches, ", "))
}

func profileNames() ([]string, error) {
	dir, err := ProfilesDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read profiles dir: %w", err)
	}

	var names []string
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
		p, err := Load(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		names = append(names, p.Name)
	}
	sort.Slice(names, func(i, j int) bool {
		return strings.ToLower(names[i]) < strings.ToLower(names[j])
	})
	return names, nil
}
