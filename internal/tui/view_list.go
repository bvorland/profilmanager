package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/bvorland/profilmanager/internal/core"
	"github.com/bvorland/profilmanager/internal/state"
)

// listModel powers the default landing view: numbered profile rows,
// vim navigation, jump-pick, incremental filter, and per-row actions.
type listModel struct {
	app *App

	profiles []*core.Profile
	filter   textinput.Model
	filterOn bool
	cursor   int // index into the *filtered* slice
}

func newListModel(a *App) *listModel {
	ti := textinput.New()
	ti.Placeholder = "filter…"
	ti.CharLimit = 64
	ti.Prompt = "/ "
	return &listModel{app: a, filter: ti}
}

// setProfiles replaces the loaded set and clamps the cursor.
func (m *listModel) setProfiles(profs []*core.Profile) {
	m.profiles = profs
	if m.cursor >= len(m.visible()) {
		m.cursor = 0
	}
}

// byName returns the loaded profile with the given Name, or nil.
func (m *listModel) byName(name string) *core.Profile {
	for _, p := range m.profiles {
		if p.Name == name {
			return p
		}
	}
	return nil
}

// visible applies the current filter (case-insensitive substring match
// on Name + Label) to the loaded profiles.
func (m *listModel) visible() []*core.Profile {
	q := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	if q == "" {
		return m.profiles
	}
	out := make([]*core.Profile, 0, len(m.profiles))
	for _, p := range m.profiles {
		if strings.Contains(strings.ToLower(p.Name), q) ||
			strings.Contains(strings.ToLower(p.Label), q) {
			out = append(out, p)
		}
	}
	return out
}

func (m *listModel) Update(msg tea.Msg) (*listModel, tea.Cmd) {
	k, isKey := msg.(tea.KeyMsg)
	if !isKey {
		return m, nil
	}

	// While the filter text input owns focus, almost every key goes to
	// it; only esc/enter exit filter mode.
	if m.filterOn {
		switch k.String() {
		case "esc":
			m.filter.SetValue("")
			m.filter.Blur()
			m.filterOn = false
			m.cursor = 0
			return m, nil
		case "enter":
			m.filter.Blur()
			m.filterOn = false
			m.cursor = 0
			return m, nil
		}
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(msg)
		m.cursor = 0
		return m, cmd
	}

	keys := m.app.keys
	vis := m.visible()

	switch {
	case key_match(keys.Filter, k):
		m.filterOn = true
		m.filter.Focus()
		return m, textinput.Blink
	case key_match(keys.Up, k):
		if m.cursor > 0 {
			m.cursor--
		}
	case key_match(keys.Down, k):
		if m.cursor < len(vis)-1 {
			m.cursor++
		}
	case key_match(keys.Top, k):
		m.cursor = 0
	case key_match(keys.Bot, k):
		if n := len(vis) - 1; n > 0 {
			m.cursor = n
		} else {
			m.cursor = 0
		}
	case key_match(keys.Pick1, k):
		// Numeric jump-pick: 1..9 maps to indices 0..8.
		n := int(k.String()[0] - '1')
		if n >= 0 && n < len(vis) {
			m.cursor = n
		}
	case key_match(keys.Refresh, k):
		return m, func() tea.Msg { return reloadProfilesMsg{} }
	case key_match(keys.Enter, k):
		return m, m.activateSelected()
	case key_match(keys.Edit, k):
		if p := m.selected(); p != nil {
			pp := *p
			return m, func() tea.Msg { return switchViewMsg{to: viewEdit, profile: &pp} }
		}
	case key_match(keys.New, k):
		return m, func() tea.Msg { return switchViewMsg{to: viewWizard} }
	case key_match(keys.Delete, k):
		if p := m.selected(); p != nil {
			m.app.confirm = newDeleteConfirm(m.app, p.Name)
		}
	}
	return m, nil
}

func (m *listModel) selected() *core.Profile {
	vis := m.visible()
	if m.cursor < 0 || m.cursor >= len(vis) {
		return nil
	}
	return vis[m.cursor]
}

// activateSelected writes the active-profile file. Per the architecture
// memo, this is metadata only — provider env application lands in the
// next phase. We surface a toast so the operator isn't misled.
func (m *listModel) activateSelected() tea.Cmd {
	p := m.selected()
	if p == nil {
		return nil
	}
	if err := state.SetActiveProfile(p.Name); err != nil {
		return m.app.setToast(toastError, fmt.Sprintf("activate failed: %v", err))
	}
	_ = state.SetLastProfile(p.Name)
	m.app.refreshSession()
	return m.app.setToast(toastOK,
		fmt.Sprintf("✓ Active profile set to %s  (provider env application will be wired in next phase)", p.Name))
}

func (m *listModel) View() string {
	s := m.app.styles
	var b strings.Builder
	b.WriteString(s.Title.Render("Profiles"))
	b.WriteString("\n\n")

	if m.filterOn || m.filter.Value() != "" {
		b.WriteString(s.Faint.Render(m.filter.View()))
		b.WriteString("\n\n")
	}

	vis := m.visible()
	if len(vis) == 0 {
		b.WriteString(m.renderEmptyState())
		return s.Content.Render(b.String())
	}

	for i, p := range vis {
		b.WriteString(m.renderRow(i, p))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(s.Faint.Render("n: new · j/k move · enter set-active · e edit · d delete · / filter · ? help"))
	return s.Content.Render(b.String())
}

func (m *listModel) renderRow(i int, p *core.Profile) string {
	s := m.app.styles
	selected := i == m.cursor
	active := p.Name == m.app.activeName

	// Numeric prefix [1]..[9]; rows beyond 9 still get a number for
	// visual consistency.
	idx := s.ListIndex.Render(fmt.Sprintf("[%d]", i+1))

	// Profile label colored to its declared color (whole label is
	// styled, matching the sample's Write-Host -ForegroundColor model).
	labelStyle := profileStyle(p.Color)
	label := labelStyle.Render(p.DisplayLabel())

	marker := ""
	if active {
		marker = " " + s.ListActive.Render("◄")
	}

	row := fmt.Sprintf("%s %s%s", idx, label, marker)

	if selected {
		// Reverse-video selection bar via background; lipgloss falls back
		// gracefully on legacy terminals.
		return lipgloss.NewStyle().
			Background(defaultPalette.SelectionBG).
			Render("▌ ") + row
	}
	return "  " + row
}

func (m *listModel) renderEmptyState() string {
	s := m.app.styles
	var b strings.Builder
	b.WriteString(s.EmptyState.Render("No profiles yet."))
	b.WriteString("\n")
	b.WriteString(s.Subtle.Render("Tips:"))
	b.WriteString("\n  ")
	b.WriteString(s.Subtle.Render("• press "))
	b.WriteString(s.StatusValue.Render("n"))
	b.WriteString(s.Subtle.Render(" to create your first profile in this UI"))
	b.WriteString("\n  ")
	b.WriteString(s.Subtle.Render("• or run "))
	b.WriteString(s.StatusValue.Render("pm import-mj"))
	b.WriteString(s.Subtle.Render(" to import from your existing PowerShell mj setup"))
	return b.String()
}
