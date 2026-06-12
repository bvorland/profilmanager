package tui

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/bvorland/profilmanager/internal/core"
	"github.com/bvorland/profilmanager/internal/wizard"
)

// updateGolden lets us refresh the snapshot files when intentional
// rendering changes happen: go test ./internal/tui/ -update
var updateGolden = flag.Bool("update", false, "rewrite golden snapshot files")

// TestRenderProfileList renders the list view with three fixture
// profiles in plain (no-color) mode and compares against a golden file.
// Plain mode keeps the snapshot diff-friendly across terminal profiles.
func TestRenderProfileList(t *testing.T) {
	// Force plain ASCII rendering so the snapshot is portable.
	lipgloss.SetColorProfile(termenv.Ascii)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.TrueColor) })

	app := newApp(true)
	app.width = 100
	app.height = 24
	app.activeName = "Contoso.Dev"
	app.sessionSource = "copilot-session"

	profiles := []*core.Profile{
		{Schema: core.SchemaVersion, Name: "Contoso.Dev", Label: "🔵 Contoso Dev", Color: "Cyan"},
		{Schema: core.SchemaVersion, Name: "Fabrikam.Work", Label: "🔴 Fabrikam Work", Color: "Red"},
		{Schema: core.SchemaVersion, Name: "Fabrikam.Personal", Label: "🟢 Fabrikam Personal", Color: "Green"},
	}
	app.list.setProfiles(profiles)
	app.list.cursor = 0

	got := app.list.View()

	goldenPath := filepath.Join("testdata", "list_three_profiles.golden")
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v (re-run with -update to create)", goldenPath, err)
	}
	if got != string(want) {
		t.Errorf("list view render mismatch (-want +got):\n--- want ---\n%s\n--- got ---\n%s", string(want), got)
	}
}

// TestRenderProfileListEmpty verifies the empty-state hint renders.
func TestRenderProfileListEmpty(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.TrueColor) })

	app := newApp(true)
	app.width = 80
	app.height = 24
	app.list.setProfiles(nil)

	out := app.list.View()
	if !contains(out, "No profiles yet.") {
		t.Errorf("empty state missing greeting; got:\n%s", out)
	}
	if !contains(out, "pm import-mj") {
		t.Errorf("empty state missing import-mj tip; got:\n%s", out)
	}
}

func TestListNewSwitchesToWizard(t *testing.T) {
	app := newApp(true)
	app.view = viewList
	app.list.setProfiles([]*core.Profile{{Schema: core.SchemaVersion, Name: "Existing.Dev"}})

	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	runAppCmd(t, app, cmd)

	if app.view != viewWizard {
		t.Fatalf("pressing n should switch to wizard, got %s", viewName(app.view))
	}
	if app.wizard == nil || app.wizard.state == nil || app.wizard.state.Name != "" {
		t.Fatalf("wizard should start fresh, got %#v", app.wizard)
	}
}

func TestListWizardSaveReturnsRefreshesAndSelectsNewProfile(t *testing.T) {
	isolateWizardTestHome(t)

	old := &core.Profile{Schema: core.SchemaVersion, Name: "Existing.Dev", Label: "Existing Dev", Color: "Cyan"}
	if err := old.Save(mustProfilePathForListTest(t, old.Name)); err != nil {
		t.Fatal(err)
	}

	app := newApp(true)
	app.view = viewList
	app.list.setProfiles([]*core.Profile{old})

	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	runAppCmd(t, app, cmd)
	app.wizard.state = &wizard.State{
		Name:           "Contoso.NewProj",
		Label:          "Contoso NewProj",
		Color:          "Blue",
		Preset:         wizard.PresetAzureOnly,
		Tenant:         "11111111-2222-3333-4444-555555555555",
		AzureConfigDir: filepath.Join(t.TempDir(), ".azure-Contoso.NewProj"),
		EnvVars:        map[string]string{},
	}

	cmd = app.wizard.save()
	if cmd != nil {
		_ = cmd()
	}

	if app.view != viewList {
		t.Fatalf("save should return to list, got %s", viewName(app.view))
	}
	if app.list.byName("Contoso.NewProj") == nil {
		t.Fatalf("new profile missing from refreshed list: %#v", app.list.profiles)
	}
	if got := app.list.selected(); got == nil || got.Name != "Contoso.NewProj" {
		t.Fatalf("new profile should be selected, got %#v", got)
	}
}

func TestListWizardCancelReturnsWithoutChangingList(t *testing.T) {
	app := newApp(true)
	app.view = viewList
	profiles := []*core.Profile{
		{Schema: core.SchemaVersion, Name: "Alpha.Dev"},
		{Schema: core.SchemaVersion, Name: "Beta.Dev"},
	}
	app.list.setProfiles(profiles)
	app.list.cursor = 1

	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	runAppCmd(t, app, cmd)
	_, cmd = app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	runAppCmd(t, app, cmd)

	if app.view != viewList {
		t.Fatalf("cancel should return to list, got %s", viewName(app.view))
	}
	if len(app.list.profiles) != 2 || app.list.cursor != 1 || app.list.selected().Name != "Beta.Dev" {
		t.Fatalf("list changed after cancel: cursor=%d profiles=%#v", app.list.cursor, app.list.profiles)
	}
}

func TestListFooterMentionsNewFirst(t *testing.T) {
	app := newApp(true)
	app.list.setProfiles([]*core.Profile{{Schema: core.SchemaVersion, Name: "Existing.Dev"}})

	out := app.list.View()
	if !strings.Contains(out, "n: new") {
		t.Fatalf("footer missing n: new hint:\n%s", out)
	}
	if strings.Index(out, "n: new") > strings.Index(out, "j/k move") {
		t.Fatalf("n: new should be first actionable hint:\n%s", out)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func runAppCmd(t *testing.T, app *App, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	msg := cmd()
	if msg == nil {
		return
	}
	_, _ = app.Update(msg)
}

func mustProfilePathForListTest(t *testing.T, name string) string {
	t.Helper()
	path, err := core.ProfilePath(name)
	if err != nil {
		t.Fatal(err)
	}
	return path
}
