package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bvorland/profilmanager/internal/core"
	"github.com/bvorland/profilmanager/internal/state"
)

// doctorModel is a skeleton diagnostics view. Provider-specific checks
// will be added when the providers work lands; for now we report the
// bits we can verify from internal/core + internal/state alone.
type doctorModel struct {
	app *App

	profilesDir    string
	profilesDirErr error
	sessionID      string
	sessionSource  string
	activeProfile  string
}

func newDoctorModel(a *App) *doctorModel {
	return &doctorModel{app: a}
}

// refresh re-reads everything from disk + env. Called on view entry.
func (d *doctorModel) refresh() {
	if dir, err := core.ProfilesDir(); err != nil {
		d.profilesDirErr = err
	} else {
		d.profilesDir = dir
		d.profilesDirErr = nil
	}
	d.sessionID, d.sessionSource = state.SessionID()
	if name, _, err := state.GetActiveProfile(); err == nil {
		d.activeProfile = name
	}
}

func (d *doctorModel) Update(msg tea.Msg) (*doctorModel, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "r":
			d.refresh()
		case "esc", "backspace":
			return d, func() tea.Msg { return switchViewMsg{to: viewList} }
		}
	}
	return d, nil
}

func (d *doctorModel) View() string {
	s := d.app.styles
	var b strings.Builder
	b.WriteString(s.Title.Render("Doctor"))
	b.WriteString("\n\n")

	b.WriteString(s.FieldLabel.Render(padRight("Profiles dir:", 22)))
	if d.profilesDirErr != nil {
		b.WriteString(s.ToastError.Render(d.profilesDirErr.Error()))
	} else {
		b.WriteString(s.StatusValue.Render(d.profilesDir))
		b.WriteString("   ")
		b.WriteString(s.OkLike("ok"))
	}
	b.WriteString("\n")

	b.WriteString(s.FieldLabel.Render(padRight("Session ID:", 22)))
	b.WriteString(s.StatusValue.Render(d.sessionID))
	b.WriteString("\n")

	b.WriteString(s.FieldLabel.Render(padRight("Session source:", 22)))
	b.WriteString(s.StatusValue.Render(d.sessionSource))
	if d.sessionSource == state.SourcePPIDFallback {
		b.WriteString("  ")
		b.WriteString(s.WarnLike("⚠ ppid fallback is fragile — set PM_SESSION_ID for stability"))
	}
	b.WriteString("\n")

	b.WriteString(s.FieldLabel.Render(padRight("Active profile:", 22)))
	if d.activeProfile == "" {
		b.WriteString(s.Faint.Render("(none)"))
	} else {
		b.WriteString(s.StatusValue.Render(d.activeProfile))
	}
	b.WriteString("\n\n")

	b.WriteString(s.Faint.Render("Provider checks: pending (next phase)"))
	b.WriteString("\n\n")
	b.WriteString(s.Faint.Render("r refresh · esc back"))
	return s.Content.Render(b.String())
}

// OkLike and WarnLike are convenience helpers attached to Styles so we
// don't have to expose the underlying lipgloss styles for one-off cases.
func (s *Styles) OkLike(text string) string {
	return s.ListActive.Render(fmt.Sprintf("✓ %s", text))
}

func (s *Styles) WarnLike(text string) string {
	return s.ToastWarn.Render(text)
}
