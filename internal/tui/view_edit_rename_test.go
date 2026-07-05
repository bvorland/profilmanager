package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bvorland/profilmanager/internal/core"
	"github.com/bvorland/profilmanager/internal/state"
)

const editRenameUUID = "11111111-2222-3333-4444-555555555555"

func TestEditNameIsEditable(t *testing.T) {
	em := newEditModel(nil)
	if !em.fieldFocusable(fName) {
		t.Fatal("Name field should be editable in the edit view")
	}
}

func TestEditNameChangeSyncsDefaultConfigDirs(t *testing.T) {
	isolateWizardTestHome(t)
	app := newApp(true)
	em := app.edit
	em.load(&core.Profile{
		Schema: core.SchemaVersion,
		Name:   "Contoso.Old",
		Color:  "Cyan",
		Azure:  &core.AzureProfile{ConfigDir: core.DefaultAzureConfigDir("Contoso.Old"), TenantID: editRenameUUID},
		Azd:    &core.AzdProfile{ConfigDir: core.DefaultAzdConfigDir("Contoso.Old")},
	})
	if !em.azureLinked || !em.azdLinked {
		t.Fatalf("expected azure/azd linked after load, got azure=%v azd=%v", em.azureLinked, em.azdLinked)
	}

	em.focus = int(fName)
	em.inputs[fName].SetValue("Contoso.New")
	em.syncNameDerivedInputs()

	if got, want := em.inputs[fAzureConfigDir].Value(), core.DefaultAzureConfigDir("Contoso.New"); got != want {
		t.Fatalf("azure config dir input = %q, want %q", got, want)
	}
	if got, want := em.inputs[fAzdConfigDir].Value(), core.DefaultAzdConfigDir("Contoso.New"); got != want {
		t.Fatalf("azd config dir input = %q, want %q", got, want)
	}
}

func TestEditNameChangeKeepsCustomConfigDir(t *testing.T) {
	isolateWizardTestHome(t)
	custom := filepath.Join(t.TempDir(), "custom-azure")
	app := newApp(true)
	em := app.edit
	em.load(&core.Profile{
		Schema: core.SchemaVersion,
		Name:   "Contoso.Old",
		Color:  "Cyan",
		Azure:  &core.AzureProfile{ConfigDir: custom, TenantID: editRenameUUID},
	})
	if em.azureLinked {
		t.Fatal("custom azure dir should not be linked")
	}

	em.focus = int(fName)
	em.inputs[fName].SetValue("Contoso.New")
	em.syncNameDerivedInputs()

	if em.inputs[fAzureConfigDir].Value() != custom {
		t.Fatalf("custom azure dir should be unchanged, got %q", em.inputs[fAzureConfigDir].Value())
	}
}

func TestEditSaveRenamesProfileFileAndMarkers(t *testing.T) {
	isolateWizardTestHome(t)
	t.Setenv("PM_SESSION_ID", "edit-rename-session")

	orig := &core.Profile{
		Schema: core.SchemaVersion,
		Name:   "Contoso.Old",
		Label:  core.ApplyColorEmojiPrefix("Contoso.Old", "Cyan"),
		Color:  "Cyan",
		Azure:  &core.AzureProfile{ConfigDir: core.DefaultAzureConfigDir("Contoso.Old"), TenantID: editRenameUUID},
	}
	oldPath, err := core.ProfilePath("Contoso.Old")
	if err != nil {
		t.Fatalf("ProfilePath: %v", err)
	}
	if err := orig.Save(oldPath); err != nil {
		t.Fatalf("save orig: %v", err)
	}
	if err := state.SetActiveProfile("Contoso.Old"); err != nil {
		t.Fatalf("SetActiveProfile: %v", err)
	}

	app := newApp(true)
	em := app.edit
	em.load(orig)
	em.focus = int(fName)
	em.inputs[fName].SetValue("Contoso.New")
	em.syncNameDerivedInputs()

	if err := em.save(); err != nil {
		t.Fatalf("save rename: %v", err)
	}

	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old profile file should be gone, stat err=%v", err)
	}
	newPath, _ := core.ProfilePath("Contoso.New")
	loaded, err := core.Load(newPath)
	if err != nil {
		t.Fatalf("load renamed: %v", err)
	}
	if loaded.Name != "Contoso.New" {
		t.Fatalf("renamed name = %q, want Contoso.New", loaded.Name)
	}
	if want := core.DefaultAzureConfigDir("Contoso.New"); loaded.Azure.ConfigDir != want {
		t.Fatalf("azure config dir = %q, want %q", loaded.Azure.ConfigDir, want)
	}
	if active, _, _ := state.GetActiveProfile(); active != "Contoso.New" {
		t.Fatalf("active marker = %q, want Contoso.New", active)
	}
	if em.origName != "Contoso.New" {
		t.Fatalf("editor origName not updated: %q", em.origName)
	}
}

func TestEditSaveRenameRefusesExistingTarget(t *testing.T) {
	isolateWizardTestHome(t)
	t.Setenv("PM_SESSION_ID", "edit-rename-conflict")

	for _, name := range []string{"Contoso.Old", "Contoso.Taken"} {
		p := &core.Profile{Schema: core.SchemaVersion, Name: name, Color: "Cyan"}
		path, err := core.ProfilePath(name)
		if err != nil {
			t.Fatalf("ProfilePath(%q): %v", name, err)
		}
		if err := p.Save(path); err != nil {
			t.Fatalf("save %q: %v", name, err)
		}
	}

	app := newApp(true)
	em := app.edit
	em.load(&core.Profile{Schema: core.SchemaVersion, Name: "Contoso.Old", Color: "Cyan"})
	em.focus = int(fName)
	em.inputs[fName].SetValue("Contoso.Taken")

	if err := em.save(); err == nil {
		t.Fatal("expected error renaming onto an existing profile")
	}
	oldPath, _ := core.ProfilePath("Contoso.Old")
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("original profile should survive a refused rename: %v", err)
	}
}
