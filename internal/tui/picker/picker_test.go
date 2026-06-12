package picker

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/bvorland/profilmanager/internal/core"
)

var updateGolden = flag.Bool("update", false, "rewrite golden snapshot files")

func TestInitialRender(t *testing.T) {
	withPlainRendering(t)

	m := newModel(fixtureProfiles())
	m.width = 100
	m.height = 24

	assertGolden(t, "picker_initial.golden", m.View())
}

func TestFilterRender(t *testing.T) {
	withPlainRendering(t)

	m := newModel(fixtureProfiles())
	m.width = 100
	m.height = 24
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})

	assertGolden(t, "picker_filter_co.golden", m.View())
}

func TestEmptyRender(t *testing.T) {
	withPlainRendering(t)

	m := newModel(nil)
	m.width = 100
	m.height = 24

	assertGolden(t, "picker_empty.golden", m.View())
}

func TestNoMatchEnterDoesNotPanic(t *testing.T) {
	withPlainRendering(t)

	m := newModel(fixtureProfiles())
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})

	assertNotPanics(t, func() {
		_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	})
	if m.selectedName != "" {
		t.Fatalf("expected no selection for no-match filter, got %q", m.selectedName)
	}
}

func TestTextinputPasteKeybinding(t *testing.T) {
	m := newModel(nil)

	for _, key := range m.filter.KeyMap.Paste.Keys() {
		if key == "ctrl+v" {
			return
		}
	}
	t.Fatalf("paste keybinding keys = %v, want ctrl+v", m.filter.KeyMap.Paste.Keys())
}

func fixtureProfiles() []*core.Profile {
	return []*core.Profile{
		{Schema: core.SchemaVersion, Name: "Contoso.MainDev", Label: "🔵 Contoso Main Dev", Color: "Cyan"},
		{Schema: core.SchemaVersion, Name: "Contoso.North-Prod", Label: "🟣 Contoso North Prod", Color: "Magenta"},
		{Schema: core.SchemaVersion, Name: "Fabrikam.Personal", Label: "🟢 Fabrikam Personal", Color: "Green"},
	}
}

func withPlainRendering(t *testing.T) {
	t.Helper()
	lipgloss.SetColorProfile(termenv.Ascii)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.TrueColor) })
}

func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v (run -update to create)", path, err)
	}
	if got != string(want) {
		t.Errorf("snapshot %s mismatch\n--- want ---\n%s\n--- got ---\n%s", path, string(want), got)
	}
}

func assertNotPanics(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("unexpected panic: %v", r)
		}
	}()
	fn()
}
