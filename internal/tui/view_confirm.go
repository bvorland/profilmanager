package tui

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bvorland/profilmanager/internal/core"
	"github.com/bvorland/profilmanager/internal/state"
)

// confirmModel is the generic Y/N modal. Set app.confirm to display it;
// it dismisses itself on either choice and returns a tea.Cmd for the
// confirmed action.
type confirmModel struct {
	app     *App
	title   string
	prompt  string
	confirm func() tea.Cmd // executed on Y
}

func (c *confirmModel) Update(msg tea.Msg) (*confirmModel, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return c, nil
	}
	switch k.String() {
	case "y", "Y", "enter":
		c.app.confirm = nil
		if c.confirm != nil {
			return nil, c.confirm()
		}
		return nil, nil
	case "n", "N", "esc":
		c.app.confirm = nil
		return nil, nil
	}
	return c, nil
}

func (c *confirmModel) View() string {
	s := c.app.styles
	body := s.Title.Render(c.title) + "\n\n" +
		s.Subtle.Render(c.prompt) + "\n\n" +
		s.Faint.Render("[y] yes   [n] no")
	return s.Modal.Render(body)
}

func newDeleteConfirm(a *App, name string) *confirmModel {
	return &confirmModel{
		app:    a,
		title:  "Delete profile?",
		prompt: fmt.Sprintf("This will permanently remove %s.toml from disk.", name),
		confirm: func() tea.Cmd {
			path, err := core.ProfilePath(name)
			if err != nil {
				return a.setToast(toastError, err.Error())
			}
			if err := os.Remove(path); err != nil {
				return a.setToast(toastError, fmt.Sprintf("delete failed: %v", err))
			}
			// If we just deleted the active profile, clear the marker
			// so we don't dangle.
			if active, _, _ := state.GetActiveProfile(); active == name {
				_ = state.ClearActiveProfile()
				a.refreshSession()
			}
			return tea.Batch(
				a.setToast(toastOK, fmt.Sprintf("✓ Deleted %s", name)),
				func() tea.Msg { return reloadProfilesMsg{} },
			)
		},
	}
}

func newDiscardConfirm(a *App) *confirmModel {
	return &confirmModel{
		app:    a,
		title:  "Discard unsaved changes?",
		prompt: "Your edits will be lost.",
		confirm: func() tea.Cmd {
			return func() tea.Msg { return switchViewMsg{to: viewList} }
		},
	}
}
