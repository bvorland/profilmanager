package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/bvorland/profilmanager/internal/core"
)

func TestProfileRename_MovesFile(t *testing.T) {
	testEnv(t)
	if _, _, err := runCLI(t, "profile", "add", "Contoso.Old", "--label", "Old", "--color", "cyan"); err != nil {
		t.Fatalf("add: %v", err)
	}

	stdout, stderr, err := runCLI(t, "profile", "rename", "Contoso.Old", "Contoso.New")
	if err != nil {
		t.Fatalf("rename: %v (stderr=%s)", err, stderr)
	}
	if !strings.Contains(stdout, "renamed") {
		t.Fatalf("expected renamed confirmation, got: %s", stdout)
	}

	oldPath, _ := core.ProfilePath("Contoso.Old")
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old profile file should be gone, stat err=%v", err)
	}
	newPath, _ := core.ProfilePath("Contoso.New")
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("new profile file missing: %v", err)
	}
	p, err := core.Load(newPath)
	if err != nil {
		t.Fatalf("load renamed: %v", err)
	}
	if p.Name != "Contoso.New" {
		t.Fatalf("renamed profile name = %q, want Contoso.New", p.Name)
	}
}

func TestProfileRename_TargetExistsFails(t *testing.T) {
	testEnv(t)
	if _, _, err := runCLI(t, "profile", "add", "Contoso.A", "--color", "cyan"); err != nil {
		t.Fatalf("add A: %v", err)
	}
	if _, _, err := runCLI(t, "profile", "add", "Contoso.B", "--color", "green"); err != nil {
		t.Fatalf("add B: %v", err)
	}
	if _, _, err := runCLI(t, "profile", "rename", "Contoso.A", "Contoso.B"); err == nil {
		t.Fatal("expected error renaming onto an existing profile")
	}
	// Source must survive a refused rename.
	pathA, _ := core.ProfilePath("Contoso.A")
	if _, err := os.Stat(pathA); err != nil {
		t.Fatalf("Contoso.A should still exist after refused rename: %v", err)
	}
}

func TestProfileRename_ResolvesFuzzyOldName(t *testing.T) {
	testEnv(t)
	if _, _, err := runCLI(t, "profile", "add", "Contoso.Dev", "--color", "cyan"); err != nil {
		t.Fatalf("add: %v", err)
	}
	// Case-insensitive match on the old name should resolve.
	if _, stderr, err := runCLI(t, "profile", "rename", "contoso.dev", "Contoso.Prod"); err != nil {
		t.Fatalf("fuzzy rename: %v (stderr=%s)", err, stderr)
	}
	newPath, _ := core.ProfilePath("Contoso.Prod")
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("renamed profile missing: %v", err)
	}
}

func TestProfileRename_RejectsBadNewName(t *testing.T) {
	testEnv(t)
	if _, _, err := runCLI(t, "profile", "add", "Contoso.Ok", "--color", "cyan"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, _, err := runCLI(t, "profile", "rename", "Contoso.Ok", "bad name"); err == nil {
		t.Fatal("expected error for invalid new name")
	}
}
