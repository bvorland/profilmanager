package core

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func setupProfilesDir(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("APPDATA", filepath.Join(tmp, "appdata"))
	t.Setenv("LOCALAPPDATA", filepath.Join(tmp, "localappdata"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "xdgcfg"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(tmp, "xdgstate"))
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	if runtime.GOOS == "windows" {
		return filepath.Join(tmp, "appdata", appDirName, "profiles")
	}
	return filepath.Join(tmp, "xdgcfg", appDirName, "profiles")
}

func writeProfile(t *testing.T, name string) {
	t.Helper()
	path, err := ProfilePath(name)
	if err != nil {
		t.Fatalf("ProfilePath(%q): %v", name, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "schema = \"1\"\nname = \"" + name + "\"\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveProfileNameExactCaseSensitive(t *testing.T) {
	setupProfilesDir(t)
	writeProfile(t, "Contoso.MainDev")

	got, err := ResolveProfileName("Contoso.MainDev")
	if err != nil {
		t.Fatalf("ResolveProfileName: %v", err)
	}
	if got != "Contoso.MainDev" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveProfileNameExactCaseInsensitive(t *testing.T) {
	setupProfilesDir(t)
	writeProfile(t, "Contoso.MainDev")

	got, err := ResolveProfileName("contoso.maindev")
	if err != nil {
		t.Fatalf("ResolveProfileName: %v", err)
	}
	if got != "Contoso.MainDev" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveProfileNameUniquePrefix(t *testing.T) {
	setupProfilesDir(t)
	writeProfile(t, "Contoso.MainDev")
	writeProfile(t, "Contoso.Dev")

	got, err := ResolveProfileName("contoso.m")
	if err != nil {
		t.Fatalf("ResolveProfileName: %v", err)
	}
	if got != "Contoso.MainDev" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveProfileNameAmbiguousPrefix(t *testing.T) {
	setupProfilesDir(t)
	writeProfile(t, "Contoso.MainDev")
	writeProfile(t, "Contoso.MainProd")

	_, err := ResolveProfileName("Contoso")
	if !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("got %v, want ErrAmbiguous", err)
	}
	if !strings.Contains(err.Error(), "Contoso.MainDev") || !strings.Contains(err.Error(), "Contoso.MainProd") {
		t.Fatalf("ambiguous error should list matches, got %v", err)
	}
}

func TestResolveProfileNameNotFound(t *testing.T) {
	setupProfilesDir(t)
	writeProfile(t, "Contoso.MainDev")

	_, err := ResolveProfileName("XYZ")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}

func TestResolveProfileNameEmptyDirNotFound(t *testing.T) {
	setupProfilesDir(t)

	_, err := ResolveProfileName("anything")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}
