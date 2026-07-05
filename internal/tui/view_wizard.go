package tui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	toml "github.com/pelletier/go-toml/v2"

	"github.com/bvorland/profilmanager/internal/core"
	"github.com/bvorland/profilmanager/internal/wizard"
)

type wizardModel struct {
	app       *App
	state     *wizard.State
	steps     []wizard.Step
	step      int
	input     textinput.Model
	statusErr string
	width     int
	height    int
	saving    bool

	rawInputs        map[string]string
	touched          bool
	suggestions      []string
	suggestionCursor int
	choosingTemplate bool
}

var presetOptions = []string{
	wizard.PresetAzureOnly,
	wizard.PresetAzureAzd,
	wizard.PresetFullDevOps,
}

func newWizardModel(a *App) *wizardModel {
	ti := textinput.New()
	ti.Prompt = "> "
	ti.CharLimit = 256
	ti.Width = 56
	ti.Focus()

	wm := &wizardModel{
		app:       a,
		state:     &wizard.State{EnvVars: map[string]string{}},
		steps:     wizard.Steps(),
		input:     ti,
		rawInputs: map[string]string{},
	}
	wm.resetInput()
	return wm
}

func (wm *wizardModel) Update(msg tea.Msg) (*wizardModel, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		wm.width = m.Width
		wm.height = m.Height
		return wm, nil
	case tea.KeyMsg:
		return wm.updateKey(m)
	default:
		return wm, nil
	}
}

func (wm *wizardModel) updateKey(k tea.KeyMsg) (*wizardModel, tea.Cmd) {
	if wm.choosingTemplate {
		return wm.updateTemplatePicker(k)
	}
	if wm.saving {
		switch k.String() {
		case "esc", "ctrl+c":
			return wm, func() tea.Msg { return switchViewMsg{to: viewList} }
		case "s", "S":
			return wm, wm.save()
		case "shift+tab":
			wm.back()
		case "enter":
			return wm, nil
		}
		return wm, nil
	}

	if wm.currentStep() == nil {
		return wm, nil
	}

	switch k.String() {
	case "esc", "ctrl+c":
		return wm, func() tea.Msg { return switchViewMsg{to: viewList} }
	case "enter", "tab":
		return wm, wm.advance()
	case "shift+tab":
		wm.back()
		return wm, nil
	case "left":
		if wm.currentStepID() == "preset" {
			wm.cyclePreset(-1)
			return wm, nil
		}
	case "right":
		if wm.currentStepID() == "preset" {
			wm.cyclePreset(1)
			return wm, nil
		}
	}

	if wm.currentStepID() == "preset" {
		return wm, nil
	}
	var cmd tea.Cmd
	wm.input, cmd = wm.input.Update(k)
	return wm, cmd
}

func (wm *wizardModel) updateTemplatePicker(k tea.KeyMsg) (*wizardModel, tea.Cmd) {
	switch k.String() {
	case "esc":
		wm.choosingTemplate = false
		wm.suggestions = nil
		wm.suggestionCursor = 0
		wm.gotoNextStep()
	case "up", "k":
		if wm.suggestionCursor > 0 {
			wm.suggestionCursor--
		}
	case "down", "j":
		if wm.suggestionCursor < len(wm.suggestions)-1 {
			wm.suggestionCursor++
		}
	case "enter":
		if wm.suggestionCursor < 0 || wm.suggestionCursor >= len(wm.suggestions) {
			return wm, nil
		}
		name := wm.state.Name
		st, err := wizard.LoadTemplate(wm.suggestions[wm.suggestionCursor], name)
		if err != nil {
			wm.statusErr = err.Error()
			return wm, nil
		}
		wm.state = st
		if wm.state.EnvVars == nil {
			wm.state.EnvVars = map[string]string{}
		}
		wm.rawInputsFromState()
		wm.choosingTemplate = false
		wm.suggestions = nil
		wm.suggestionCursor = 0
		wm.gotoNextStep()
	}
	return wm, nil
}

func (wm *wizardModel) advance() tea.Cmd {
	step := wm.currentStep()
	if step == nil {
		return nil
	}
	input := wm.currentInput()
	if err := step.Validate(input); err != nil {
		wm.statusErr = err.Error()
		return nil
	}
	oldName := ""
	if step.ID() == "name" {
		oldName = strings.TrimSpace(wm.state.Name)
	}
	step.Apply(wm.state, input)
	if step.ID() == "name" {
		wm.syncNameDerivedFields(oldName, wm.state.Name)
	}
	wm.rawInputs[step.ID()] = input
	wm.touched = true
	wm.statusErr = ""

	if step.ID() == "name" {
		wm.suggestions = core.SuggestTemplates(wm.state.Name)
		if len(wm.suggestions) > 0 {
			wm.choosingTemplate = true
			wm.suggestionCursor = 0
			return nil
		}
	}

	wm.gotoNextStep()
	return nil
}

func (wm *wizardModel) syncNameDerivedFields(oldName, newName string) {
	oldName = strings.TrimSpace(oldName)
	newName = strings.TrimSpace(newName)
	if oldName == "" || newName == "" || oldName == newName {
		return
	}

	wm.state.AzureConfigDir = strings.ReplaceAll(wm.state.AzureConfigDir, oldName, newName)
	wm.state.AzdConfigDir = strings.ReplaceAll(wm.state.AzdConfigDir, oldName, newName)

	for _, key := range []string{"azure_config_dir", "azd_config_dir"} {
		if raw, ok := wm.rawInputs[key]; ok {
			wm.rawInputs[key] = strings.ReplaceAll(raw, oldName, newName)
		}
	}

	// Keep the auto-default label in sync with a renamed profile, while
	// preserving custom labels.
	if strings.TrimSpace(wm.state.Label) == oldName {
		wm.state.Label = newName
		if _, ok := wm.rawInputs["label"]; ok {
			wm.rawInputs["label"] = newName
		}
	}
}

func (wm *wizardModel) gotoNextStep() {
	for i := wm.step + 1; i < len(wm.steps); i++ {
		if wm.shouldSkip(i) {
			continue
		}
		wm.step = i
		wm.saving = false
		wm.resetInput()
		return
	}
	wm.step = len(wm.steps)
	wm.saving = true
	wm.input.Blur()
}

func (wm *wizardModel) back() {
	if wm.saving {
		wm.saving = false
	}
	start := wm.step - 1
	if wm.step >= len(wm.steps) {
		start = len(wm.steps) - 1
	}
	for i := start; i >= 0; i-- {
		if wm.shouldSkip(i) {
			continue
		}
		wm.step = i
		wm.statusErr = ""
		wm.resetInput()
		return
	}
	wm.step = 0
	wm.saving = false
	wm.resetInput()
}

func (wm *wizardModel) shouldSkip(i int) bool {
	if i < 0 || i >= len(wm.steps) {
		return false
	}
	step := wm.steps[i]
	if step.ID() == "label" {
		return false
	}
	return step.Skippable(wm.state)
}

func (wm *wizardModel) resetInput() {
	step := wm.currentStep()
	if step == nil {
		return
	}
	for i := range wm.steps {
		if i != wm.step {
			wm.input.Blur()
		}
	}
	val := wm.stateValue(step.ID())
	if raw, ok := wm.rawInputs[step.ID()]; ok {
		val = raw
	}
	if val == "" {
		val = step.Default(wm.state)
	}
	wm.input.SetValue(val)
	wm.input.Focus()
}

func (wm *wizardModel) currentStep() wizard.Step {
	if wm.step < 0 || wm.step >= len(wm.steps) {
		return nil
	}
	return wm.steps[wm.step]
}

func (wm *wizardModel) currentStepID() string {
	if step := wm.currentStep(); step != nil {
		return step.ID()
	}
	return ""
}

func (wm *wizardModel) currentInput() string {
	if wm.currentStepID() == "preset" {
		v := wm.input.Value()
		if v == "" {
			return wizard.PresetAzureAzd
		}
		return v
	}
	return wm.input.Value()
}

func (wm *wizardModel) cyclePreset(delta int) {
	cur := wm.currentInput()
	idx := 0
	for i, opt := range presetOptions {
		if opt == cur {
			idx = i
			break
		}
	}
	idx = (idx + delta + len(presetOptions)) % len(presetOptions)
	wm.input.SetValue(presetOptions[idx])
}

func (wm *wizardModel) save() tea.Cmd {
	p, err := wizard.Build(wm.state)
	if err != nil {
		wm.statusErr = err.Error()
		return nil
	}
	path, err := core.ProfilePath(p.Name)
	if err != nil {
		wm.statusErr = err.Error()
		return nil
	}
	if _, err := os.Stat(path); err == nil {
		wm.statusErr = fmt.Sprintf("profile %q already exists", p.Name)
		return nil
	} else if !os.IsNotExist(err) {
		wm.statusErr = err.Error()
		return nil
	}
	if err := p.Save(path); err != nil {
		wm.statusErr = err.Error()
		return nil
	}
	if profs, _, err := loadProfiles(); err == nil {
		wm.app.list.setProfiles(profs)
		wm.selectListProfile(p.Name)
	}
	wm.app.view = viewList
	return wm.app.setToast(toastOK, fmt.Sprintf("✓ Created %s", p.Name))
}

func (wm *wizardModel) selectListProfile(name string) {
	if wm.app == nil || wm.app.list == nil {
		return
	}
	wm.app.list.filter.SetValue("")
	wm.app.list.filterOn = false
	for i, p := range wm.app.list.visible() {
		if p.Name == name {
			wm.app.list.cursor = i
			return
		}
	}
	wm.app.list.cursor = 0
}

func (wm *wizardModel) rawInputsFromState() {
	wm.rawInputs = map[string]string{
		"name":             wm.state.Name,
		"label":            wm.state.Label,
		"color":            wm.state.Color,
		"preset":           wm.state.Preset,
		"tenant":           wm.state.Tenant,
		"subscription":     wm.state.Subscription,
		"azure_config_dir": wm.state.AzureConfigDir,
		"azd_config_dir":   wm.state.AzdConfigDir,
		"gh_account":       wm.state.GhAccount,
		"gh_host":          wm.state.GhHost,
		"kube_context":     wm.state.KubeContext,
		"kube_namespace":   wm.state.KubeNamespace,
		"git_author":       wm.state.GitAuthor,
		"git_email":        wm.state.GitEmail,
	}
}

func (wm *wizardModel) View() string {
	s := wm.app.styles
	var b strings.Builder
	b.WriteString(wm.topBar())
	b.WriteString("\n\n")
	if wm.saving {
		b.WriteString(wm.previewView())
		return s.Content.Render(b.String())
	}
	step := wm.currentStep()
	if step == nil {
		b.WriteString(s.ToastError.Render("wizard has no current step"))
		return s.Content.Render(b.String())
	}

	b.WriteString(s.Faint.Render(step.Help()))
	b.WriteString("\n")
	if def := step.Default(wm.state); def != "" && step.ID() != "preset" {
		b.WriteString(s.Faint.Italic(true).Render("Default: " + def))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	if step.ID() == "preset" {
		b.WriteString(wm.renderPreset())
	} else {
		b.WriteString(wm.input.View())
	}
	if step.ID() == "color" {
		b.WriteString("  ")
		b.WriteString(ProfileStyle(wm.input.Value()).Render(" ████ "))
	}
	b.WriteString("\n")

	if wm.choosingTemplate {
		b.WriteString("\n")
		b.WriteString(wm.renderTemplatePicker())
	}
	if wm.statusErr != "" {
		b.WriteString("\n")
		b.WriteString(s.ToastError.Render(wm.statusErr))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(s.Faint.Render("enter: next  shift-tab: back  esc: cancel"))
	return s.Content.Render(b.String())
}

func (wm *wizardModel) topBar() string {
	total := len(wm.steps) + 1
	n := wm.step + 1
	title := "Preview"
	id := "preview"
	if step := wm.currentStep(); step != nil && !wm.saving {
		title = step.Title()
		id = step.ID()
	}
	if wm.saving {
		n = total
	}
	text := fmt.Sprintf("Step %d/%d: %s", n, total, title)
	return lipgloss.NewStyle().Bold(true).Foreground(wm.groupColor(id)).Render(text)
}

func (wm *wizardModel) groupColor(id string) lipgloss.Color {
	switch {
	case strings.HasPrefix(id, "azure") || id == "tenant" || id == "subscription" || strings.HasPrefix(id, "azd"):
		return lipgloss.Color("14")
	case strings.HasPrefix(id, "gh"):
		return lipgloss.Color("13")
	case strings.HasPrefix(id, "kube"):
		return lipgloss.Color("10")
	case strings.HasPrefix(id, "git"):
		return lipgloss.Color("11")
	default:
		return lipgloss.Color("12")
	}
}

func (wm *wizardModel) renderPreset() string {
	cur := wm.currentInput()
	parts := make([]string, 0, len(presetOptions))
	for _, opt := range presetOptions {
		if opt == cur {
			parts = append(parts, wm.app.styles.FieldFocus.Render("(● "+opt+")"))
			continue
		}
		parts = append(parts, wm.app.styles.Faint.Render("(○ "+opt+")"))
	}
	return strings.Join(parts, "  ") + "\n" + wm.app.styles.Faint.Render("←/→ cycle")
}

func (wm *wizardModel) renderTemplatePicker() string {
	s := wm.app.styles
	var b strings.Builder
	b.WriteString(s.Subtle.Render("Use existing profile as template?"))
	b.WriteString("\n")
	b.WriteString(s.Faint.Render("↑↓ to choose, enter to use, esc to skip"))
	b.WriteString("\n")
	for i, name := range wm.suggestions {
		marker := "  "
		if i == wm.suggestionCursor {
			marker = lipgloss.NewStyle().Background(defaultPalette.SelectionBG).Render("▌ ")
		}
		b.WriteString(marker)
		b.WriteString(s.StatusValue.Render(name))
		b.WriteString("\n")
	}
	return b.String()
}

func (wm *wizardModel) previewView() string {
	s := wm.app.styles
	body, err := wm.previewTOML()
	if err != nil {
		body = s.ToastError.Render(err.Error())
	}
	box := s.Modal.Render(body)
	footer := s.Faint.Render("[s] save  [esc] cancel")
	if wm.statusErr != "" {
		return box + "\n\n" + s.ToastError.Render(wm.statusErr) + "\n\n" + footer
	}
	return box + "\n\n" + footer
}

func (wm *wizardModel) previewTOML() (string, error) {
	p, err := wizard.Build(wm.state)
	if err != nil {
		return "", err
	}
	data, err := toml.Marshal(p)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(data), "\n"), nil
}

func (wm *wizardModel) stateValue(id string) string {
	switch id {
	case "name":
		return wm.state.Name
	case "label":
		return wm.state.Label
	case "color":
		return wm.state.Color
	case "preset":
		return wm.state.Preset
	case "tenant":
		return wm.state.Tenant
	case "subscription":
		return wm.state.Subscription
	case "azure_config_dir":
		return wm.state.AzureConfigDir
	case "azd_config_dir":
		return wm.state.AzdConfigDir
	case "gh_account":
		return wm.state.GhAccount
	case "gh_host":
		return wm.state.GhHost
	case "kube_context":
		return wm.state.KubeContext
	case "kube_namespace":
		return wm.state.KubeNamespace
	case "git_author":
		return wm.state.GitAuthor
	case "git_email":
		return wm.state.GitEmail
	default:
		return ""
	}
}
