package tui

import (
	"path/filepath"
	"runtime"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestRenderWizardColorStep(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.TrueColor) })
	isolateWizardTestHome(t)

	app := newApp(true)
	app.width = 100
	app.height = 24
	app.wizard.state.Name = "Contoso.MainDev"
	app.wizard.step = wizardStepIndex(t, app.wizard, "color")
	app.wizard.input.SetValue("Cyan")
	app.wizard.input.Focus()

	assertTUIGolden(t, "wizard_color_step.golden", app.wizard.View())
}

func TestWizardEmptyEnterDoesNotPanic(t *testing.T) {
	app := newApp(true)
	assertNotPanic(t, func() {
		_, _ = app.wizard.Update(tea.KeyMsg{Type: tea.KeyEnter})
	})
	if app.wizard.step != 0 {
		t.Fatalf("empty name should stay on step 0, got %d", app.wizard.step)
	}
	if app.wizard.statusErr == "" {
		t.Fatal("empty name should surface validation error")
	}
}

func TestWizardShiftTabAtStartDoesNotUnderflow(t *testing.T) {
	app := newApp(true)
	assertNotPanic(t, func() {
		_, _ = app.wizard.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	})
	if app.wizard.step != 0 {
		t.Fatalf("shift-tab from step 0 should stay at 0, got %d", app.wizard.step)
	}
}

func TestWizardEnterOnFinalDoesNotOverflow(t *testing.T) {
	app := newApp(true)
	app.wizard.saving = true
	app.wizard.step = len(app.wizard.steps)
	assertNotPanic(t, func() {
		_, _ = app.wizard.Update(tea.KeyMsg{Type: tea.KeyEnter})
	})
	if !app.wizard.saving || app.wizard.step != len(app.wizard.steps) {
		t.Fatalf("enter on preview should stay on preview, saving=%v step=%d", app.wizard.saving, app.wizard.step)
	}
}

func wizardStepIndex(t *testing.T, wm *wizardModel, id string) int {
	t.Helper()
	for i, step := range wm.steps {
		if step.ID() == id {
			return i
		}
	}
	t.Fatalf("wizard step %q not found", id)
	return 0
}

func assertNotPanic(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("unexpected panic: %v", r)
		}
	}()
	fn()
}

func isolateWizardTestHome(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", tmp)
		t.Setenv("APPDATA", filepath.Join(tmp, "appdata"))
		t.Setenv("LOCALAPPDATA", filepath.Join(tmp, "localappdata"))
		return
	}
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "xdgcfg"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(tmp, "xdgstate"))
}
