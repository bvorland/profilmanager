package tui

import (
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/bvorland/profilmanager/internal/core"
)

// editField indexes into editModel.inputs.
type editField int

const (
	fName editField = iota
	fLabel
	fColor
	fAzureConfigDir
	fAzureSubscription
	fAzureTenant
	fAzdConfigDir
	fAzdSubscription
	fGHUser
	fKubeContext
	fKubeNamespace
	fGitUserName
	fGitUserEmail
	fGitSigningKey
	fieldCount
)

var editFieldLabels = [fieldCount]string{
	fName:              "Name",
	fLabel:             "Label",
	fColor:             "Color",
	fAzureConfigDir:    "Azure ConfigDir",
	fAzureSubscription: "Azure SubscriptionID",
	fAzureTenant:       "Azure TenantID",
	fAzdConfigDir:      "Azd ConfigDir",
	fAzdSubscription:   "Azd SubscriptionID",
	fGHUser:            "GitHub Account",
	fKubeContext:       "Kube Context",
	fKubeNamespace:     "Kube Namespace",
	fGitUserName:       "Git UserName",
	fGitUserEmail:      "Git UserEmail",
	fGitSigningKey:     "Git SigningKey",
}

// envRow is a single [[env]] entry in the editor's sub-list. Exactly
// one of Value or Ref is "active" depending on useRef.
type envRow struct {
	keyIn   textinput.Model
	valIn   textinput.Model
	refIn   textinput.Model
	useRef  bool
	focused bool
}

func newEnvRow(e core.EnvEntry) envRow {
	mk := func(placeholder, val string) textinput.Model {
		ti := textinput.New()
		ti.Placeholder = placeholder
		ti.CharLimit = 256
		ti.Width = 32
		ti.SetValue(val)
		return ti
	}
	return envRow{
		keyIn:  mk("KEY", e.Key),
		valIn:  mk("literal value", e.Value),
		refIn:  mk("op://Vault/Item/field", e.Ref),
		useRef: e.Ref != "" && e.Value == "",
	}
}

// editModel is the field-by-field profile editor.
type editModel struct {
	app *App

	// Are we editing an existing profile (Name read-only) or creating?
	creating bool
	origName string

	inputs       [fieldCount]textinput.Model
	focus        int // 0..fieldCount-1 → text input; fieldCount → env list focus
	envRows      []envRow
	envFocus     int  // -1 if focus is on the env header
	envSubFocus  int  // 0 = key, 1 = value/ref, 2 = toggle
	dirty        bool
	statusErr    string
	initialSaved string // snapshot of marshaled state for dirty tracking
}

func newEditModel(a *App) *editModel {
	em := &editModel{app: a}
	for i := range em.inputs {
		ti := textinput.New()
		ti.CharLimit = 256
		ti.Width = 48
		em.inputs[i] = ti
	}
	em.inputs[fName].Placeholder = "Acme.Dev (ASCII letters, digits, . _ -)"
	em.inputs[fLabel].Placeholder = "🔵 Acme Dev"
	em.inputs[fColor].Placeholder = "Cyan / Magenta / Yellow / ..."
	em.inputs[fAzureConfigDir].Placeholder = "~/.azure-Acme.Dev"
	em.inputs[fAzdConfigDir].Placeholder = "~/.azd-Acme.Dev"
	em.envFocus = -1
	return em
}

// load reset the editor for either an existing profile (p != nil) or a
// blank create form (p == nil).
func (em *editModel) load(p *core.Profile) {
	em.statusErr = ""
	em.focus = 0
	em.envFocus = -1
	em.envSubFocus = 0
	em.dirty = false
	em.envRows = em.envRows[:0]

	for i := range em.inputs {
		em.inputs[i].SetValue("")
		em.inputs[i].Blur()
	}

	if p == nil {
		em.creating = true
		em.origName = ""
		em.inputs[fName].Focus()
		em.snapshot()
		return
	}
	em.creating = false
	em.origName = p.Name
	em.inputs[fName].SetValue(p.Name)
	em.inputs[fLabel].SetValue(p.Label)
	em.inputs[fColor].SetValue(p.Color)
	if p.Azure != nil {
		em.inputs[fAzureConfigDir].SetValue(p.Azure.ConfigDir)
		em.inputs[fAzureSubscription].SetValue(p.Azure.SubscriptionID)
		em.inputs[fAzureTenant].SetValue(p.Azure.TenantID)
	}
	if p.Azd != nil {
		em.inputs[fAzdConfigDir].SetValue(p.Azd.ConfigDir)
		em.inputs[fAzdSubscription].SetValue(p.Azd.SubscriptionID)
	}
	if p.GitHub != nil {
		em.inputs[fGHUser].SetValue(p.GitHub.Account)
	}
	if p.Kube != nil {
		em.inputs[fKubeContext].SetValue(p.Kube.Context)
		em.inputs[fKubeNamespace].SetValue(p.Kube.Namespace)
	}
	if p.Git != nil {
		em.inputs[fGitUserName].SetValue(p.Git.UserName)
		em.inputs[fGitUserEmail].SetValue(p.Git.UserEmail)
		em.inputs[fGitSigningKey].SetValue(p.Git.SigningKey)
	}
	for _, e := range p.Env {
		em.envRows = append(em.envRows, newEnvRow(e))
	}
	em.inputs[fLabel].Focus()
	em.focus = int(fLabel)
	em.snapshot()
}

func (em *editModel) snapshot() {
	em.initialSaved = em.serialize()
}

// serialize is used purely for dirty tracking — not for persistence.
func (em *editModel) serialize() string {
	var b strings.Builder
	for i := range em.inputs {
		b.WriteString(em.inputs[i].Value())
		b.WriteByte(0)
	}
	for _, r := range em.envRows {
		b.WriteString(r.keyIn.Value())
		b.WriteByte(0)
		if r.useRef {
			b.WriteString("R:")
			b.WriteString(r.refIn.Value())
		} else {
			b.WriteString("V:")
			b.WriteString(r.valIn.Value())
		}
		b.WriteByte(0)
	}
	return b.String()
}

// fieldFocusable returns true if the field is editable in the current
// mode (Name is read-only when editing an existing profile).
func (em *editModel) fieldFocusable(f editField) bool {
	if f == fName && !em.creating {
		return false
	}
	return true
}

func (em *editModel) Update(k tea.KeyMsg) (*editModel, tea.Cmd) {
	keys := em.app.keys

	switch {
	case key_match(keys.Save, k):
		if err := em.save(); err != nil {
			em.statusErr = err.Error()
			return em, em.app.setToast(toastError, err.Error())
		}
		em.app.setToast(toastOK, fmt.Sprintf("✓ Saved %s", em.inputs[fName].Value()))
		cmd := tea.Batch(
			func() tea.Msg { return reloadProfilesMsg{} },
			func() tea.Msg { return switchViewMsg{to: viewList} },
		)
		return em, cmd
	case key_match(keys.Cancel, k):
		em.dirty = em.serialize() != em.initialSaved
		if em.dirty {
			em.app.confirm = newDiscardConfirm(em.app)
			return em, nil
		}
		return em, func() tea.Msg { return switchViewMsg{to: viewList} }
	case key_match(keys.Tab, k):
		em.focusNext()
		return em, nil
	case key_match(keys.ShTab, k):
		em.focusPrev()
		return em, nil
	}

	// Env row management — keys active only when focused on env area.
	if em.focus == int(fieldCount) {
		switch k.String() {
		case "a", "+":
			em.envRows = append(em.envRows, newEnvRow(core.EnvEntry{}))
			em.envFocus = len(em.envRows) - 1
			em.envSubFocus = 0
			return em, nil
		case "x", "-":
			if em.envFocus >= 0 && em.envFocus < len(em.envRows) {
				em.envRows = append(em.envRows[:em.envFocus], em.envRows[em.envFocus+1:]...)
				if em.envFocus >= len(em.envRows) {
					em.envFocus = len(em.envRows) - 1
				}
			}
			return em, nil
		case "t":
			if em.envFocus >= 0 && em.envFocus < len(em.envRows) {
				em.envRows[em.envFocus].useRef = !em.envRows[em.envFocus].useRef
			}
			return em, nil
		case "up":
			if em.envFocus > 0 {
				em.envFocus--
			} else if em.envFocus == 0 {
				em.envFocus = -1
				em.focus = int(fieldCount - 1)
				em.inputs[fieldCount-1].Focus()
			}
			return em, nil
		case "down":
			if em.envFocus < len(em.envRows)-1 {
				em.envFocus++
			}
			return em, nil
		}
		// Route to the focused sub-input on the focused env row.
		if em.envFocus >= 0 && em.envFocus < len(em.envRows) {
			row := &em.envRows[em.envFocus]
			var cmd tea.Cmd
			switch em.envSubFocus {
			case 0:
				row.keyIn.Focus()
				row.keyIn, cmd = row.keyIn.Update(k)
			default:
				if row.useRef {
					row.refIn.Focus()
					row.refIn, cmd = row.refIn.Update(k)
				} else {
					row.valIn.Focus()
					row.valIn, cmd = row.valIn.Update(k)
				}
			}
			return em, cmd
		}
	}

	// Plain field editing — route to focused text input.
	if em.focus >= 0 && em.focus < int(fieldCount) {
		var cmd tea.Cmd
		em.inputs[em.focus], cmd = em.inputs[em.focus].Update(k)
		return em, cmd
	}
	return em, nil
}

func (em *editModel) focusNext() {
	if em.focus >= 0 && em.focus < int(fieldCount) {
		em.inputs[em.focus].Blur()
	}
	for i := 1; i <= int(fieldCount); i++ {
		next := (em.focus + i) % (int(fieldCount) + 1)
		if next == int(fieldCount) {
			// env section focus
			em.focus = next
			em.envFocus = 0
			if len(em.envRows) == 0 {
				em.envFocus = -1
			}
			return
		}
		if em.fieldFocusable(editField(next)) {
			em.focus = next
			em.inputs[em.focus].Focus()
			return
		}
	}
}

func (em *editModel) focusPrev() {
	if em.focus >= 0 && em.focus < int(fieldCount) {
		em.inputs[em.focus].Blur()
	}
	for i := 1; i <= int(fieldCount); i++ {
		prev := (em.focus - i + int(fieldCount) + 1) % (int(fieldCount) + 1)
		if prev == int(fieldCount) {
			em.focus = prev
			em.envFocus = len(em.envRows) - 1
			return
		}
		if em.fieldFocusable(editField(prev)) {
			em.focus = prev
			em.inputs[em.focus].Focus()
			return
		}
	}
}

// save assembles a *core.Profile from inputs, validates it, and writes
// the TOML file.
func (em *editModel) save() error {
	name := strings.TrimSpace(em.inputs[fName].Value())
	if err := core.ValidateName(name); err != nil {
		return err
	}
	p := &core.Profile{
		Schema: core.SchemaVersion,
		Name:   name,
		Label:  strings.TrimSpace(em.inputs[fLabel].Value()),
		Color:  strings.TrimSpace(em.inputs[fColor].Value()),
	}
	if v := em.subProfileAzure(); v != nil {
		p.Azure = v
	}
	if v := em.subProfileAzd(); v != nil {
		p.Azd = v
	}
	if v := strings.TrimSpace(em.inputs[fGHUser].Value()); v != "" {
		p.GitHub = &core.GitHubProfile{Account: v}
	}
	if c, n := strings.TrimSpace(em.inputs[fKubeContext].Value()), strings.TrimSpace(em.inputs[fKubeNamespace].Value()); c != "" || n != "" {
		p.Kube = &core.KubeProfile{Context: c, Namespace: n}
	}
	if u, e, k := strings.TrimSpace(em.inputs[fGitUserName].Value()), strings.TrimSpace(em.inputs[fGitUserEmail].Value()), strings.TrimSpace(em.inputs[fGitSigningKey].Value()); u != "" || e != "" || k != "" {
		p.Git = &core.GitIdentity{UserName: u, UserEmail: e, SigningKey: k}
	}
	for i, r := range em.envRows {
		key := strings.TrimSpace(r.keyIn.Value())
		if key == "" {
			continue // skip empty rows silently
		}
		entry := core.EnvEntry{Key: key}
		if r.useRef {
			entry.Ref = strings.TrimSpace(r.refIn.Value())
		} else {
			entry.Value = r.valIn.Value()
		}
		if entry.Value == "" && entry.Ref == "" {
			return fmt.Errorf("env[%d] %q: value or ref must be set", i, key)
		}
		p.Env = append(p.Env, entry)
	}
	if !em.creating && name != em.origName {
		return errors.New("profile name cannot be changed in edit mode (delete and re-create)")
	}
	path, err := core.ProfilePath(name)
	if err != nil {
		return err
	}
	if err := p.Save(path); err != nil {
		return err
	}
	em.snapshot()
	return nil
}

func (em *editModel) subProfileAzure() *core.AzureProfile {
	dir := strings.TrimSpace(em.inputs[fAzureConfigDir].Value())
	sub := strings.TrimSpace(em.inputs[fAzureSubscription].Value())
	ten := strings.TrimSpace(em.inputs[fAzureTenant].Value())
	if dir == "" && sub == "" && ten == "" {
		return nil
	}
	return &core.AzureProfile{ConfigDir: dir, SubscriptionID: sub, TenantID: ten}
}

func (em *editModel) subProfileAzd() *core.AzdProfile {
	dir := strings.TrimSpace(em.inputs[fAzdConfigDir].Value())
	sub := strings.TrimSpace(em.inputs[fAzdSubscription].Value())
	if dir == "" && sub == "" {
		return nil
	}
	return &core.AzdProfile{ConfigDir: dir, SubscriptionID: sub}
}

func (em *editModel) View() string {
	s := em.app.styles
	var b strings.Builder
	title := "Edit profile"
	if em.creating {
		title = "New profile"
	}
	b.WriteString(s.Title.Render(title))
	b.WriteString("\n\n")

	for i := 0; i < int(fieldCount); i++ {
		f := editField(i)
		labelStyle := s.FieldLabel
		valueRender := em.inputs[i].View()
		if !em.fieldFocusable(f) {
			labelStyle = s.Faint
			valueRender = s.Faint.Render(em.inputs[i].Value() + "  (read-only)")
		}
		if em.focus == i {
			labelStyle = s.FieldFocus
		}
		row := fmt.Sprintf("%s  %s", labelStyle.Render(padRight(editFieldLabels[f]+":", 22)), valueRender)
		if f == fColor {
			swatch := profileStyle(em.inputs[i].Value()).Render(" ████ ")
			row += "  " + swatch
		}
		b.WriteString(row)
		b.WriteString("\n")
	}

	b.WriteString("\n")
	envHeaderStyle := s.FieldLabel
	if em.focus == int(fieldCount) {
		envHeaderStyle = s.FieldFocus
	}
	b.WriteString(envHeaderStyle.Render(fmt.Sprintf("Env entries (%d)", len(em.envRows))))
	if em.focus == int(fieldCount) {
		b.WriteString(s.Faint.Render("   a/+ add  ·  x/- remove  ·  t toggle value/ref  ·  ↑/↓ move"))
	}
	b.WriteString("\n")
	for i, r := range em.envRows {
		marker := "  "
		if em.focus == int(fieldCount) && em.envFocus == i {
			marker = lipgloss.NewStyle().Background(defaultPalette.SelectionBG).Render("▌ ")
		}
		kind := "value"
		val := r.valIn.Value()
		if r.useRef {
			kind = "ref  "
			val = r.refIn.Value()
		}
		b.WriteString(marker)
		b.WriteString(s.StatusValue.Render(padRight(r.keyIn.Value(), 22)))
		b.WriteString(s.Faint.Render(kind + " = "))
		b.WriteString(s.Subtle.Render(val))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	if em.statusErr != "" {
		b.WriteString(s.ToastError.Render(em.statusErr))
		b.WriteString("\n")
	}
	b.WriteString(s.Faint.Render("tab/⇧tab move · ctrl+s save · esc cancel"))
	return s.Content.Render(b.String())
}
