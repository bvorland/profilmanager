package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Prefixes for the default, home-rooted per-profile config directories.
// These mirror the wizard defaults (`~/.azure-<name>`, `~/.azd-<name>`).
const (
	azureDirPrefix = ".azure-"
	azdDirPrefix   = ".azd-"
)

// DirMoveStatus describes the outcome of one name-derived directory move
// attempted during a profile rename.
type DirMoveStatus string

const (
	DirMoved        DirMoveStatus = "moved"         // source renamed onto target
	DirAbsent       DirMoveStatus = "absent"        // source did not exist (skipped)
	DirTargetExists DirMoveStatus = "target-exists" // target already present (skipped)
	DirUnchanged    DirMoveStatus = "unchanged"     // source == target (skipped)
	DirFailed       DirMoveStatus = "failed"        // os.Rename returned an error
)

// DirMoveResult records one name-derived directory move for user feedback.
type DirMoveResult struct {
	Label  string // "azure" | "azd" | "gh" | "kube"
	From   string
	To     string
	Status DirMoveStatus
	Err    error
}

// RenameResult summarizes a completed profile rename.
type RenameResult struct {
	OldName  string
	NewName  string
	OldPath  string
	NewPath  string
	DirMoves []DirMoveResult
}

// DefaultAzureConfigDir returns the canonical absolute default AZURE_CONFIG_DIR
// for a profile name (<home>/.azure-<name>). Empty if home cannot be resolved.
func DefaultAzureConfigDir(name string) string { return defaultHomeDir(azureDirPrefix, name) }

// DefaultAzdConfigDir returns the canonical absolute default AZD_CONFIG_DIR for
// a profile name (<home>/.azd-<name>). Empty if home cannot be resolved.
func DefaultAzdConfigDir(name string) string { return defaultHomeDir(azdDirPrefix, name) }

func defaultHomeDir(prefix, name string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, prefix+name)
}

// isDefaultConfigDir reports whether path is the default home-rooted config dir
// (tilde or absolute form) for name, e.g. "~/.azure-Foo" or "<home>/.azure-Foo".
func isDefaultConfigDir(path, prefix, name string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	target := prefix + name
	for _, tilde := range []string{"~/" + target, `~\` + target} {
		if pathEqual(path, tilde) {
			return true
		}
	}
	if abs := defaultHomeDir(prefix, name); abs != "" {
		if pathEqual(expandTilde(path), abs) {
			return true
		}
	}
	return false
}

// rewriteDefaultConfigDir returns the config dir for newName when path is the
// default dir for oldName (preserving tilde-vs-absolute style) plus true.
// Custom paths are returned unchanged with false.
func rewriteDefaultConfigDir(path, prefix, oldName, newName string) (string, bool) {
	if !isDefaultConfigDir(path, prefix, oldName) {
		return path, false
	}
	trimmed := strings.TrimSpace(path)
	if strings.HasPrefix(trimmed, "~/") {
		return "~/" + prefix + newName, true
	}
	if strings.HasPrefix(trimmed, `~\`) {
		return `~\` + prefix + newName, true
	}
	return defaultHomeDir(prefix, newName), true
}

// pathEqual compares two paths after cleaning, case-insensitively on Windows.
func pathEqual(a, b string) bool {
	a = filepath.Clean(strings.TrimSpace(a))
	b = filepath.Clean(strings.TrimSpace(b))
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// expandTilde resolves a leading "~", "~/" or "~\" to the operator's home dir.
func expandTilde(p string) string {
	p = strings.TrimSpace(p)
	if p == "~" {
		if h, err := os.UserHomeDir(); err == nil {
			return h
		}
		return p
	}
	if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, p[2:])
		}
	}
	return p
}

// stateSubdir returns <StateDir>/<kind>/<name>, the per-profile gh/kube state
// dir computed by the providers at Apply time. Empty if StateDir fails.
func stateSubdir(kind, name string) string {
	root, err := StateDir()
	if err != nil {
		return ""
	}
	return filepath.Join(root, kind, name)
}

// MoveRenamedProfileDirs moves the name-derived state dirs for a rename,
// best-effort. azure/azd are moved only when their OLD value was the default
// home-rooted pattern for oldName (custom paths are the operator's to manage).
// gh/kube dirs are computed from the names and always considered. Each result
// records what happened so callers can report moves, skips, and failures.
func MoveRenamedProfileDirs(oldName, newName, azureOld, azureNew, azdOld, azdNew string) []DirMoveResult {
	var results []DirMoveResult
	if azureOld != "" && isDefaultConfigDir(azureOld, azureDirPrefix, oldName) {
		results = append(results, moveDir("azure", expandTilde(azureOld), expandTilde(azureNew)))
	}
	if azdOld != "" && isDefaultConfigDir(azdOld, azdDirPrefix, oldName) {
		results = append(results, moveDir("azd", expandTilde(azdOld), expandTilde(azdNew)))
	}
	results = append(results, moveDir("gh", stateSubdir("gh", oldName), stateSubdir("gh", newName)))
	results = append(results, moveDir("kube", stateSubdir("kube", oldName), stateSubdir("kube", newName)))
	return results
}

func moveDir(label, from, to string) DirMoveResult {
	res := DirMoveResult{Label: label, From: from, To: to}
	if strings.TrimSpace(from) == "" || strings.TrimSpace(to) == "" || pathEqual(from, to) {
		res.Status = DirUnchanged
		return res
	}
	if _, err := os.Stat(from); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			res.Status = DirAbsent
			return res
		}
		res.Status, res.Err = DirFailed, err
		return res
	}
	if _, err := os.Stat(to); err == nil {
		res.Status = DirTargetExists
		return res
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		res.Status, res.Err = DirFailed, err
		return res
	}
	if err := os.Rename(from, to); err != nil {
		res.Status, res.Err = DirFailed, err
		return res
	}
	res.Status = DirMoved
	return res
}

// RenameProfile renames a profile end to end: it validates both names, refuses
// to clobber an existing target, rewrites the on-disk TOML filename and the
// `name` field, rewrites default-pattern azure/azd config dirs, syncs an
// auto-default label (preserving custom labels), optionally moves the
// name-derived state dirs (best-effort), and removes the old file.
//
// moveDirs=false repoints the config-dir values without touching the
// filesystem; the providers recreate fresh dirs on the next Apply.
func RenameProfile(oldName, newName string, moveDirs bool) (RenameResult, error) {
	if err := ValidateName(oldName); err != nil {
		return RenameResult{}, err
	}
	if err := ValidateName(newName); err != nil {
		return RenameResult{}, err
	}
	res := RenameResult{OldName: oldName, NewName: newName}
	if oldName == newName {
		return res, nil
	}

	oldPath, err := ProfilePath(oldName)
	if err != nil {
		return res, err
	}
	newPath, err := ProfilePath(newName)
	if err != nil {
		return res, err
	}
	res.OldPath, res.NewPath = oldPath, newPath

	if _, err := os.Stat(newPath); err == nil {
		return res, fmt.Errorf("profile %q already exists at %s", newName, newPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return res, err
	}

	p, err := Load(oldPath)
	if err != nil {
		return res, err
	}

	var azureOld, azureNew, azdOld, azdNew string
	if p.Azure != nil {
		azureOld = p.Azure.ConfigDir
		if rewritten, ok := rewriteDefaultConfigDir(azureOld, azureDirPrefix, oldName, newName); ok {
			p.Azure.ConfigDir = rewritten
		}
		azureNew = p.Azure.ConfigDir
	}
	if p.Azd != nil {
		azdOld = p.Azd.ConfigDir
		if rewritten, ok := rewriteDefaultConfigDir(azdOld, azdDirPrefix, oldName, newName); ok {
			p.Azd.ConfigDir = rewritten
		}
		azdNew = p.Azd.ConfigDir
	}

	// Track an auto-default label to the new name; leave custom labels alone.
	if p.Label == oldName || p.Label == ApplyColorEmojiPrefix(oldName, p.Color) {
		p.Label = ApplyColorEmojiPrefix(newName, p.Color)
	}

	p.Name = newName
	if err := p.Save(newPath); err != nil {
		return res, err
	}

	if moveDirs {
		res.DirMoves = MoveRenamedProfileDirs(oldName, newName, azureOld, azureNew, azdOld, azdNew)
	}

	if err := os.Remove(oldPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return res, fmt.Errorf("remove old profile %s: %w", oldPath, err)
	}
	return res, nil
}

// IsDefaultAzureConfigDir reports whether path is the default home-rooted
// ~/.azure-<name> directory (tilde or absolute form). UIs use this to decide
// whether a config-dir input is still "linked" to the profile name.
func IsDefaultAzureConfigDir(path, name string) bool {
	return isDefaultConfigDir(path, azureDirPrefix, name)
}

// IsDefaultAzdConfigDir reports whether path is the default home-rooted
// ~/.azd-<name> directory (tilde or absolute form).
func IsDefaultAzdConfigDir(path, name string) bool {
	return isDefaultConfigDir(path, azdDirPrefix, name)
}
