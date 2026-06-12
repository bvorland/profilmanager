package core

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SuggestTemplates returns existing profile names that share the group prefix
// before the first dot in name. For "Contoso.NewProj", this returns all
// existing "Contoso.*" profile names. Invalid or groupless names return nil.
func SuggestTemplates(name string) []string {
	group, ok := groupPrefix(name)
	if !ok {
		return nil
	}
	dir, err := ProfilesDir()
	if err != nil {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return nil
	}

	prefix := group + "."
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		fileName := e.Name()
		if strings.HasPrefix(fileName, ".pm-") || !strings.HasSuffix(strings.ToLower(fileName), ".toml") {
			continue
		}
		p, err := Load(filepath.Join(dir, fileName))
		if err != nil {
			continue
		}
		if strings.HasPrefix(p.Name, prefix) {
			names = append(names, p.Name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	sort.Strings(names)
	return names
}

func groupPrefix(name string) (string, bool) {
	if err := ValidateName(name); err != nil {
		return "", false
	}
	i := strings.IndexByte(name, '.')
	if i <= 0 {
		return "", false
	}
	group := name[:i]
	if group == "" {
		return "", false
	}
	return group, true
}
