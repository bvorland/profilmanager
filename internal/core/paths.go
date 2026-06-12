package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const appDirName = "profilmanager"

// ProfilesDir returns the per-OS directory where profile TOML files live.
// It is created (with parents) on first call.
//
//	Windows: %APPDATA%\profilmanager\profiles
//	macOS:   ~/Library/Application Support/profilmanager/profiles
//	Linux:   ${XDG_CONFIG_HOME:-~/.config}/profilmanager/profiles
func ProfilesDir() (string, error) {
	base, err := configBase()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, appDirName, "profiles")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create profiles dir: %w", err)
	}
	return dir, nil
}

// StateDir returns the per-OS directory for session/cache state.
// It is created (with parents) on first call.
//
//	Windows: %LOCALAPPDATA%\profilmanager
//	macOS:   ~/Library/Caches/profilmanager
//	Linux:   ${XDG_STATE_HOME:-~/.local/state}/profilmanager
func StateDir() (string, error) {
	base, err := stateBase()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, appDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create state dir: %w", err)
	}
	return dir, nil
}

// ProfilePath returns the absolute .toml path for the named profile under
// ProfilesDir. The name is validated to prevent path traversal or weird
// filesystem characters (see ValidateName).
func ProfilePath(name string) (string, error) {
	if err := ValidateName(name); err != nil {
		return "", err
	}
	dir, err := ProfilesDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name+".toml"), nil
}

func configBase() (string, error) {
	switch runtime.GOOS {
	case "windows":
		if v := os.Getenv("APPDATA"); v != "" {
			return v, nil
		}
		return "", errors.New("APPDATA is not set")
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support"), nil
	default:
		if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
			return v, nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".config"), nil
	}
}

func stateBase() (string, error) {
	switch runtime.GOOS {
	case "windows":
		if v := os.Getenv("LOCALAPPDATA"); v != "" {
			return v, nil
		}
		return "", errors.New("LOCALAPPDATA is not set")
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Caches"), nil
	default:
		if v := os.Getenv("XDG_STATE_HOME"); v != "" {
			return v, nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".local", "state"), nil
	}
}
