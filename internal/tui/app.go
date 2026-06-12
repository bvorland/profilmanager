package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/bvorland/profilmanager/internal/core"
	"github.com/bvorland/profilmanager/internal/state"
)

// view identifies the currently rendered top-level view. Sub-modals
// (confirm dialog, help overlay) are layered on top of the active view
// and live in App, not as their own view value.
type view int

const (
	viewList view = iota
	viewEdit
	viewDoctor
	viewWizard
)

// toastKind controls the toast border/foreground color.
type toastKind int

const (
	toastInfo toastKind = iota
	toastOK
	toastWarn
	toastError
)

// toast is a short-lived status message rendered just above the status
// bar. It auto-clears after toastTTL.
type toast struct {
	kind     toastKind
	text     string
	expireAt time.Time
}

const toastTTL = 3 * time.Second

// toastExpireMsg is dispatched after toastTTL to clear the toast.
type toastExpireMsg struct{ at time.Time }

// switchViewMsg requests a view transition. Carries an optional payload
// (e.g. the profile to edit).
type switchViewMsg struct {
	to      view
	profile *core.Profile // nil → blank (create) for edit view
}

// reloadProfilesMsg asks the app to re-read profiles from disk.
type reloadProfilesMsg struct{}

// profilesLoadedMsg carries the result of a reload.
type profilesLoadedMsg struct {
	profiles []*core.Profile
	errors   []error
}

// App is the top-level Bubble Tea model. It owns the active view, the
// status bar, transient toast, and the help overlay.
type App struct {
	styles  *Styles
	keys    keyMap
	view    view
	width   int
	height  int
	noColor bool

	// Sub-models for each view.
	list   *listModel
	edit   *editModel
	doctor *doctorModel
	wizard *wizardModel

	// Confirm modal — when non-nil, intercepts input.
	confirm *confirmModel

	// Overlays / toast.
	helpOpen bool
	toast    *toast

	// Cached active profile name + source (refreshed on view enter).
	activeName    string
	sessionSource string
}

// newApp constructs the root model with all sub-views ready.
func newApp(noColor bool) *App {
	styles := newStyles(noColor)
	keys := defaultKeys()
	a := &App{
		styles:  styles,
		keys:    keys,
		view:    viewList,
		noColor: noColor,
	}
	a.list = newListModel(a)
	a.edit = newEditModel(a)
	a.doctor = newDoctorModel(a)
	a.wizard = newWizardModel(a)
	return a
}

// Init is the Bubble Tea bootstrap — kick off the first profile load
// and pull initial session/active-profile state.
func (a *App) Init() tea.Cmd {
	a.refreshSession()
	return tea.Batch(loadProfilesCmd(), tea.WindowSize())
}

// loadProfilesCmd reads profiles asynchronously so a slow disk doesn't
// block the first paint.
func loadProfilesCmd() tea.Cmd {
	return func() tea.Msg {
		profs, errs, err := loadProfiles()
		if err != nil {
			return profilesLoadedMsg{errors: []error{err}}
		}
		return profilesLoadedMsg{profiles: profs, errors: errs}
	}
}

func (a *App) refreshSession() {
	name, source, _ := state.GetActiveProfile()
	a.activeName = name
	a.sessionSource = source
}

// Update routes events: modal first, overlay second, then the active
// sub-view. Global keys (q, ?) take precedence over sub-view bindings
// only when no modal/edit text input is consuming input.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = m.Width
		a.height = m.Height
	case toastExpireMsg:
		if a.toast != nil && !m.at.Before(a.toast.expireAt) {
			a.toast = nil
		}
		return a, nil
	case profilesLoadedMsg:
		a.list.setProfiles(m.profiles)
		if len(m.errors) > 0 {
			a.setToast(toastWarn, fmt.Sprintf("loaded with %d error(s) — see pm doctor", len(m.errors)))
		}
		return a, nil
	case reloadProfilesMsg:
		return a, loadProfilesCmd()
	case switchViewMsg:
		a.view = m.to
		if m.to == viewEdit {
			a.edit.load(m.profile)
		}
		if m.to == viewDoctor {
			a.doctor.refresh()
		}
		if m.to == viewWizard {
			a.wizard = newWizardModel(a)
		}
		a.refreshSession()
		return a, nil
	}

	// Confirm modal swallows everything until dismissed.
	if a.confirm != nil {
		cm, cmd := a.confirm.Update(msg)
		a.confirm = cm
		return a, cmd
	}

	// Global keystrokes that don't conflict with text input.
	if k, ok := msg.(tea.KeyMsg); ok {
		// Edit/wizard views own their text inputs — let them consume keys first
		// (except quit on ctrl+c, which is non-negotiable).
		if a.view == viewEdit {
			if k.String() == "ctrl+c" {
				return a, tea.Quit
			}
			em, cmd := a.edit.Update(k)
			a.edit = em
			return a, cmd
		}
		if a.view == viewWizard {
			wm, cmd := a.wizard.Update(k)
			a.wizard = wm
			return a, cmd
		}
		switch {
		case a.helpOpen && (k.String() == "?" || k.String() == "esc" || k.String() == "q"):
			a.helpOpen = false
			return a, nil
		case key_match(a.keys.Help, k):
			a.helpOpen = !a.helpOpen
			return a, nil
		case key_match(a.keys.Quit, k):
			return a, tea.Quit
		case key_match(a.keys.Doctor, k):
			return a, func() tea.Msg { return switchViewMsg{to: viewDoctor} }
		}
	}

	// Route to active view.
	switch a.view {
	case viewList:
		lm, cmd := a.list.Update(msg)
		a.list = lm
		return a, cmd
	case viewDoctor:
		dm, cmd := a.doctor.Update(msg)
		a.doctor = dm
		return a, cmd
	case viewWizard:
		wm, cmd := a.wizard.Update(msg)
		a.wizard = wm
		return a, cmd
	}
	return a, nil
}

// View renders the full screen: title, content, toast, status bar.
// Overlays (help, confirm) replace the content area when active.
func (a *App) View() string {
	if a.width == 0 {
		// Pre-first-WindowSizeMsg — render a minimal placeholder so the
		// initial paint isn't an empty screen.
		return "loading…\n"
	}
	body := a.renderBody()
	status := a.renderStatusBar()
	toastLine := a.renderToast()

	contentHeight := a.height - lipgloss.Height(status) - 1
	if toastLine != "" {
		contentHeight -= lipgloss.Height(toastLine)
	}
	if contentHeight < 1 {
		contentHeight = 1
	}
	// Pad body to fill the available height so the status bar pins to
	// the bottom regardless of content length.
	body = padToHeight(body, contentHeight)

	parts := []string{body}
	if toastLine != "" {
		parts = append(parts, toastLine)
	}
	parts = append(parts, status)
	return strings.Join(parts, "\n")
}

func (a *App) renderBody() string {
	if a.confirm != nil {
		return a.confirm.View()
	}
	if a.helpOpen {
		return a.renderHelpOverlay()
	}
	switch a.view {
	case viewEdit:
		return a.edit.View()
	case viewDoctor:
		return a.doctor.View()
	case viewWizard:
		return a.wizard.View()
	default:
		return a.list.View()
	}
}

func (a *App) renderStatusBar() string {
	activeChunk := a.styles.Faint.Render("(no active profile)")
	if a.activeName != "" {
		profCol := a.styles.StatusValue
		// Try to color the active profile name with its own color.
		if p := a.list.byName(a.activeName); p != nil && p.Color != "" {
			profCol = lipgloss.NewStyle().Foreground(profileColor(p.Color)).Bold(true)
		}
		activeChunk = a.styles.StatusKey.Render("active: ") + profCol.Render(a.activeName)
	}
	sessionChunk := a.styles.StatusKey.Render("session: ") + a.styles.StatusValue.Render(a.sessionSource)
	viewChunk := a.styles.StatusKey.Render("view: ") + a.styles.StatusValue.Render(viewName(a.view))
	hint := a.styles.Faint.Render("? help · q quit")

	left := activeChunk + a.styles.Faint.Render("  •  ") + sessionChunk + a.styles.Faint.Render("  •  ") + viewChunk
	gap := a.width - lipgloss.Width(left) - lipgloss.Width(hint) - 2
	if gap < 1 {
		gap = 1
	}
	line := left + strings.Repeat(" ", gap) + hint
	return a.styles.StatusBar.Width(a.width).Render(line)
}

func (a *App) renderToast() string {
	if a.toast == nil {
		return ""
	}
	var s lipgloss.Style
	switch a.toast.kind {
	case toastOK:
		s = a.styles.ToastOK
	case toastWarn:
		s = a.styles.ToastWarn
	case toastError:
		s = a.styles.ToastError
	default:
		s = a.styles.Toast
	}
	return s.Render(a.toast.text)
}

func (a *App) renderHelpOverlay() string {
	keys := a.keys
	var b strings.Builder
	b.WriteString(a.styles.Title.Render("Help"))
	b.WriteString("\n\n")
	groups := keys.FullHelp()
	for _, g := range groups {
		for _, k := range g {
			h := k.Help()
			b.WriteString(a.styles.StatusKey.Render(padRight(h.Key, 12)))
			b.WriteString("  ")
			b.WriteString(a.styles.Subtle.Render(h.Desc))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	b.WriteString(a.styles.Faint.Render("Press ? or esc to close."))
	return a.styles.Content.Render(b.String())
}

// setToast displays a transient status message and schedules its
// expiry. Multiple toasts overwrite each other (most recent wins).
func (a *App) setToast(kind toastKind, text string) tea.Cmd {
	a.toast = &toast{kind: kind, text: text, expireAt: time.Now().Add(toastTTL)}
	expireAt := a.toast.expireAt
	return tea.Tick(toastTTL, func(t time.Time) tea.Msg {
		return toastExpireMsg{at: expireAt}
	})
}

// padToHeight ensures s spans exactly h visible lines by appending
// empty rows (or truncating if too tall).
func padToHeight(s string, h int) string {
	lines := strings.Split(s, "\n")
	if len(lines) >= h {
		return strings.Join(lines[:h], "\n")
	}
	pad := make([]string, h-len(lines))
	return strings.Join(append(lines, pad...), "\n")
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

func viewName(v view) string {
	switch v {
	case viewEdit:
		return "edit"
	case viewDoctor:
		return "doctor"
	case viewWizard:
		return "wizard"
	default:
		return "list"
	}
}

// key_match is a tiny indirection so we can swap implementations under
// test without dragging the bubbles/key package import into every file.
func key_match(b interface{ Keys() []string }, k tea.KeyMsg) bool {
	s := k.String()
	for _, kk := range b.Keys() {
		if kk == s {
			return true
		}
	}
	return false
}
