package tui

import "github.com/charmbracelet/bubbles/key"

// keyMap is the discoverable, view-aware key binding table. bubbles/help
// renders this directly so the `?` overlay never drifts from reality.
type keyMap struct {
	// Navigation
	Up    key.Binding
	Down  key.Binding
	Top   key.Binding
	Bot   key.Binding
	Pick1 key.Binding // numeric jump-pick (1..9)

	// Actions on selected profile
	Enter   key.Binding
	Edit    key.Binding
	New     key.Binding
	Delete  key.Binding
	Refresh key.Binding

	// Search / filter
	Filter key.Binding

	// Modes
	Doctor key.Binding
	Help   key.Binding

	// Lifecycle
	Save   key.Binding
	Cancel key.Binding
	Back   key.Binding
	Quit   key.Binding
	Tab    key.Binding
	ShTab  key.Binding
}

// ShortHelp returns the row of bindings shown in the status bar / inline.
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Enter, k.Edit, k.New, k.Help, k.Quit}
}

// FullHelp returns the grid shown in the `?` overlay.
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Top, k.Bot, k.Pick1},
		{k.Enter, k.Edit, k.New, k.Delete, k.Refresh},
		{k.Filter, k.Doctor, k.Help},
		{k.Back, k.Quit},
	}
}

func defaultKeys() keyMap {
	return keyMap{
		Up:      key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("k/↑", "up")),
		Down:    key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j/↓", "down")),
		Top:     key.NewBinding(key.WithKeys("g", "home"), key.WithHelp("g", "top")),
		Bot:     key.NewBinding(key.WithKeys("G", "end"), key.WithHelp("G", "bottom")),
		Pick1:   key.NewBinding(key.WithKeys("1", "2", "3", "4", "5", "6", "7", "8", "9"), key.WithHelp("1-9", "jump-pick")),
		Enter:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "set active")),
		Edit:    key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit")),
		New:     key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new")),
		Delete:  key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
		Refresh: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		Filter:  key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		Doctor:  key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "doctor")),
		Help:    key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Save:    key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "save")),
		Cancel:  key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
		Back:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Quit:    key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		Tab:     key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next field")),
		ShTab:   key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("⇧tab", "prev field")),
	}
}
