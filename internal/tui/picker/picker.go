package picker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/bvorland/profilmanager/internal/core"
	"github.com/bvorland/profilmanager/internal/tui"
)

// PickProfile launches an interactive bubble tea picker over all profiles
// on disk. Returns the canonical profile name selected, or cancelled=true
// if the user pressed Esc / Ctrl-C / q. Returns an error only on
// unrecoverable bubble tea / IO failures.
//
// Designed to be called from CLI commands when a profile-name arg is
// omitted AND stdin is a TTY. The caller is responsible for the TTY
// check before invoking.
func PickProfile(ctx context.Context) (name string, cancelled bool, err error) {
	profiles, err := loadProfiles()
	if err != nil {
		return "", false, err
	}

	p := tea.NewProgram(newModel(profiles),
		tea.WithContext(ctx),
		tea.WithOutput(os.Stderr),
	)
	final, err := p.Run()
	if err != nil {
		return "", false, fmt.Errorf("profile picker: %w", err)
	}
	m, ok := final.(*model)
	if !ok {
		return "", false, errors.New("profile picker: unexpected final model")
	}
	if m.selectedName != "" {
		return m.selectedName, false, nil
	}
	return "", true, nil
}

type model struct {
	profiles []*core.Profile
	filter   textinput.Model
	filterOn bool
	cursor   int
	width    int
	height   int

	selectedName string
	cancelled    bool
}

func newModel(profiles []*core.Profile) *model {
	ti := textinput.New()
	ti.Placeholder = "filter…"
	ti.CharLimit = 64
	ti.Prompt = "/ "
	return &model{profiles: profiles, filter: ti}
}

func (m *model) Init() tea.Cmd {
	return tea.WindowSize()
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.KeyMsg:
		return m.updateKey(msg)
	default:
		if m.filterOn {
			var cmd tea.Cmd
			m.filter, cmd = m.filter.Update(msg)
			return m, cmd
		}
		return m, nil
	}
}

func (m *model) updateKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if len(m.profiles) == 0 {
		m.cancelled = true
		return m, tea.Quit
	}

	if k.String() == "ctrl+c" {
		m.cancelled = true
		return m, tea.Quit
	}

	if m.filterOn {
		switch k.String() {
		case "esc":
			m.filter.SetValue("")
			m.filter.Blur()
			m.filterOn = false
			m.cursor = 0
			return m, nil
		case "enter":
			return m.selectCurrent()
		}
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(k)
		m.cursor = 0
		return m, cmd
	}

	vis := m.visible()
	switch k.String() {
	case "esc", "q":
		m.cancelled = true
		return m, tea.Quit
	case "/":
		m.filterOn = true
		m.filter.Focus()
		return m, textinput.Blink
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(vis)-1 {
			m.cursor++
		}
	case "enter":
		return m.selectCurrent()
	}
	return m, nil
}

func (m *model) selectCurrent() (tea.Model, tea.Cmd) {
	p := m.selected()
	if p == nil {
		return m, nil
	}
	m.selectedName = p.Name
	return m, tea.Quit
}

func (m *model) selected() *core.Profile {
	vis := m.visible()
	if m.cursor < 0 || m.cursor >= len(vis) {
		return nil
	}
	return vis[m.cursor]
}

func (m *model) visible() []*core.Profile {
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

func (m *model) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Select a profile:"))
	b.WriteString("\n\n")

	if len(m.profiles) == 0 {
		b.WriteString(emptyStyle.Render("No profiles found. Run `pm profile new` to create one."))
		b.WriteString("\n\n")
		b.WriteString(helpStyle.Render("press any key to cancel"))
		return contentStyle.Render(b.String())
	}

	if m.filterOn || m.filter.Value() != "" {
		b.WriteString(helpStyle.Render(m.filter.View()))
		b.WriteString("\n\n")
	}

	vis := m.visible()
	if len(vis) == 0 {
		b.WriteString(emptyStyle.Render("No profiles match the current filter."))
		b.WriteString("\n")
	} else {
		for i, p := range vis {
			b.WriteString(m.renderRow(i, p))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("Ctrl+V paste · Esc cancel · Enter select"))
	return contentStyle.Render(b.String())
}

func (m *model) renderRow(i int, p *core.Profile) string {
	idx := indexStyle.Render(fmt.Sprintf("[%d]", i+1))
	label := tui.ProfileStyle(p.Color).Render(p.DisplayLabel())
	row := fmt.Sprintf("%s %s", idx, label)
	if i == m.cursor {
		return lipgloss.NewStyle().
			Background(lipgloss.Color("236")).
			Render("▌ ") + row
	}
	return "  " + row
}

func loadProfiles() ([]*core.Profile, error) {
	dir, err := core.ProfilesDir()
	if err != nil {
		return nil, fmt.Errorf("locate profiles dir: %w", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read profiles dir: %w", err)
	}
	profiles := make([]*core.Profile, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".toml") ||
			strings.HasPrefix(name, ".pm-") {
			continue
		}
		p, err := core.Load(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		profiles = append(profiles, p)
	}
	sort.Slice(profiles, func(i, j int) bool {
		return strings.ToLower(profiles[i].Name) < strings.ToLower(profiles[j].Name)
	})
	return profiles, nil
}

var (
	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	helpStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	indexStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	emptyStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Italic(true).Padding(1, 0)
	contentStyle = lipgloss.NewStyle().Padding(1, 1)
)

var _ tea.Model = (*model)(nil)
