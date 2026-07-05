package core

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// renameIsolate points core config/state dirs and home at a fresh tmp dir.
func renameIsolate(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	switch runtime.GOOS {
	case "windows":
		t.Setenv("USERPROFILE", tmp)
		t.Setenv("APPDATA", filepath.Join(tmp, "appdata"))
		t.Setenv("LOCALAPPDATA", filepath.Join(tmp, "localappdata"))
	default:
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "xdgcfg"))
		t.Setenv("XDG_STATE_HOME", filepath.Join(tmp, "xdgstate"))
	}
	return tmp
}

func mkdirWithMarker(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "marker"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write marker in %s: %v", dir, err)
	}
}

func saveProfile(t *testing.T, p *Profile) string {
	t.Helper()
	path, err := ProfilePath(p.Name)
	if err != nil {
		t.Fatalf("ProfilePath(%q): %v", p.Name, err)
	}
	if err := p.Save(path); err != nil {
		t.Fatalf("save %q: %v", p.Name, err)
	}
	return path
}

func TestRenameProfile_RewritesDefaultDirsAndMovesThem(t *testing.T) {
	renameIsolate(t)
	old, newName := "Contoso.MainDev", "Contoso.NewDev"

	p := &Profile{
		Schema: SchemaVersion,
		Name:   old,
		Label:  ApplyColorEmojiPrefix(old, "Cyan"),
		Color:  "Cyan",
		Azure:  &AzureProfile{ConfigDir: DefaultAzureConfigDir(old), TenantID: "11111111-2222-3333-4444-555555555555"},
		Azd:    &AzdProfile{ConfigDir: DefaultAzdConfigDir(old)},
	}
	oldPath := saveProfile(t, p)

	mkdirWithMarker(t, DefaultAzureConfigDir(old))
	mkdirWithMarker(t, DefaultAzdConfigDir(old))
	mkdirWithMarker(t, stateSubdir("gh", old))
	mkdirWithMarker(t, stateSubdir("kube", old))

	res, err := RenameProfile(old, newName, true)
	if err != nil {
		t.Fatalf("RenameProfile: %v", err)
	}

	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old profile file should be gone, stat err=%v", err)
	}
	loaded, err := Load(res.NewPath)
	if err != nil {
		t.Fatalf("load renamed: %v", err)
	}
	if loaded.Name != newName {
		t.Fatalf("name = %q, want %q", loaded.Name, newName)
	}
	if loaded.Azure.ConfigDir != DefaultAzureConfigDir(newName) {
		t.Fatalf("azure config dir = %q, want %q", loaded.Azure.ConfigDir, DefaultAzureConfigDir(newName))
	}
	if loaded.Azd.ConfigDir != DefaultAzdConfigDir(newName) {
		t.Fatalf("azd config dir = %q, want %q", loaded.Azd.ConfigDir, DefaultAzdConfigDir(newName))
	}

	for label, dir := range map[string]string{
		"azure": DefaultAzureConfigDir(newName),
		"azd":   DefaultAzdConfigDir(newName),
		"gh":    stateSubdir("gh", newName),
		"kube":  stateSubdir("kube", newName),
	} {
		if _, err := os.Stat(filepath.Join(dir, "marker")); err != nil {
			t.Fatalf("%s dir not moved to %s: %v", label, dir, err)
		}
	}
	for label, dir := range map[string]string{
		"azure": DefaultAzureConfigDir(old),
		"gh":    stateSubdir("gh", old),
	} {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("old %s dir should be gone (%s), stat err=%v", label, dir, err)
		}
	}
}

func TestRenameProfile_PreservesCustomConfigDir(t *testing.T) {
	tmp := renameIsolate(t)
	old, newName := "Contoso.Custom", "Contoso.Custom2"
	custom := filepath.Join(tmp, "somewhere", "azure-home")

	p := &Profile{
		Schema: SchemaVersion,
		Name:   old,
		Color:  "Green",
		Azure:  &AzureProfile{ConfigDir: custom, TenantID: "11111111-2222-3333-4444-555555555555"},
	}
	saveProfile(t, p)

	res, err := RenameProfile(old, newName, true)
	if err != nil {
		t.Fatalf("RenameProfile: %v", err)
	}
	loaded, err := Load(res.NewPath)
	if err != nil {
		t.Fatalf("load renamed: %v", err)
	}
	if loaded.Azure.ConfigDir != custom {
		t.Fatalf("custom azure dir should be preserved: got %q want %q", loaded.Azure.ConfigDir, custom)
	}
	for _, m := range res.DirMoves {
		if m.Label == "azure" {
			t.Fatalf("custom azure dir should not be moved, got %+v", m)
		}
	}
}

func TestRenameProfile_PreservesCustomLabel(t *testing.T) {
	renameIsolate(t)
	old, newName := "Equinor.PSS-Pilot", "Equinor.AEP-Demo"
	custom := "⚪ Equinor PSS Pilot"

	saveProfile(t, &Profile{Schema: SchemaVersion, Name: old, Label: custom, Color: "White"})

	res, err := RenameProfile(old, newName, false)
	if err != nil {
		t.Fatalf("RenameProfile: %v", err)
	}
	loaded, err := Load(res.NewPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Label != custom {
		t.Fatalf("custom label should be preserved: got %q want %q", loaded.Label, custom)
	}
}

func TestRenameProfile_SyncsAutoDefaultLabel(t *testing.T) {
	renameIsolate(t)
	old, newName := "Contoso.Alpha", "Contoso.Beta"
	saveProfile(t, &Profile{Schema: SchemaVersion, Name: old, Label: ApplyColorEmojiPrefix(old, "Cyan"), Color: "Cyan"})

	res, err := RenameProfile(old, newName, false)
	if err != nil {
		t.Fatalf("RenameProfile: %v", err)
	}
	loaded, err := Load(res.NewPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if want := ApplyColorEmojiPrefix(newName, "Cyan"); loaded.Label != want {
		t.Fatalf("auto label should track rename: got %q want %q", loaded.Label, want)
	}
}

func TestRenameProfile_RefusesWhenTargetExists(t *testing.T) {
	renameIsolate(t)
	saveProfile(t, &Profile{Schema: SchemaVersion, Name: "Contoso.A", Color: "Cyan"})
	saveProfile(t, &Profile{Schema: SchemaVersion, Name: "Contoso.B", Color: "Green"})

	if _, err := RenameProfile("Contoso.A", "Contoso.B", true); err == nil {
		t.Fatal("expected error renaming onto an existing profile")
	}
	// Original must be untouched.
	if _, err := ProfilePath("Contoso.A"); err != nil {
		t.Fatalf("ProfilePath: %v", err)
	}
	pathA, _ := ProfilePath("Contoso.A")
	if _, err := os.Stat(pathA); err != nil {
		t.Fatalf("Contoso.A should still exist: %v", err)
	}
}

func TestRenameProfile_NoopOnEqual(t *testing.T) {
	renameIsolate(t)
	saveProfile(t, &Profile{Schema: SchemaVersion, Name: "Contoso.Same", Color: "Cyan"})
	res, err := RenameProfile("Contoso.Same", "Contoso.Same", true)
	if err != nil {
		t.Fatalf("no-op rename: %v", err)
	}
	if len(res.DirMoves) != 0 {
		t.Fatalf("no-op rename should not move dirs, got %+v", res.DirMoves)
	}
}

func TestMoveRenamedProfileDirs_SkipsAbsentAndExisting(t *testing.T) {
	renameIsolate(t)
	old, newName := "Grp.Old", "Grp.New"

	// gh source exists, kube target already exists (should be skipped).
	mkdirWithMarker(t, stateSubdir("gh", old))
	mkdirWithMarker(t, stateSubdir("kube", old))
	mkdirWithMarker(t, stateSubdir("kube", newName))

	results := MoveRenamedProfileDirs(old, newName, "", "", "", "")
	byLabel := map[string]DirMoveResult{}
	for _, r := range results {
		byLabel[r.Label] = r
	}
	if byLabel["gh"].Status != DirMoved {
		t.Fatalf("gh should move, got %s", byLabel["gh"].Status)
	}
	if byLabel["kube"].Status != DirTargetExists {
		t.Fatalf("kube should be skipped (target exists), got %s", byLabel["kube"].Status)
	}
}
