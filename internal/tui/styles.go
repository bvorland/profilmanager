package tui

// All visual styling lives in this file. Other files in the package MUST
// reference these vars instead of declaring inline lipgloss styles — that
// way NO_COLOR / --no-color is honored by flipping a single switch.

import (
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// Palette holds every color the TUI uses. Centralizing here makes the
// NO_COLOR flip a one-liner and keeps the visual identity coherent.
type Palette struct {
	Title       lipgloss.Color
	Subtle      lipgloss.Color
	Faint       lipgloss.Color
	Border      lipgloss.Color
	Accent      lipgloss.Color
	Warn        lipgloss.Color
	Error       lipgloss.Color
	OK          lipgloss.Color
	StatusBG    lipgloss.Color
	SelectionBG lipgloss.Color
}

// Default palette tuned to read on both dark and light backgrounds. We
// stick to ANSI 16 so termenv can degrade gracefully on legacy terminals.
var defaultPalette = Palette{
	Title:       lipgloss.Color("12"), // bright blue
	Subtle:      lipgloss.Color("7"),  // light gray
	Faint:       lipgloss.Color("8"),  // dark gray
	Border:      lipgloss.Color("8"),
	Accent:      lipgloss.Color("14"), // bright cyan
	Warn:        lipgloss.Color("11"), // bright yellow
	Error:       lipgloss.Color("9"),  // bright red
	OK:          lipgloss.Color("10"), // bright green
	StatusBG:    lipgloss.Color("0"),
	SelectionBG: lipgloss.Color("236"),
}

// Styles bundles every named lipgloss style used by the views. Built once
// at program start by newStyles().
type Styles struct {
	Title       lipgloss.Style
	Subtle      lipgloss.Style
	Faint       lipgloss.Style
	Help        lipgloss.Style
	StatusBar   lipgloss.Style
	StatusKey   lipgloss.Style
	StatusValue lipgloss.Style
	Content     lipgloss.Style
	ListRow     lipgloss.Style
	ListRowSel  lipgloss.Style
	ListIndex   lipgloss.Style
	ListActive  lipgloss.Style
	Toast       lipgloss.Style
	ToastOK     lipgloss.Style
	ToastWarn   lipgloss.Style
	ToastError  lipgloss.Style
	Modal       lipgloss.Style
	FieldLabel  lipgloss.Style
	FieldFocus  lipgloss.Style
	EmptyState  lipgloss.Style
}

// newStyles builds the style bundle. When noColor is true every style is
// stripped of foreground/background and bold/faint attributes are kept
// (they're plain text, not color).
func newStyles(noColor bool) *Styles {
	if noColor {
		// Force lipgloss to render plain ASCII regardless of TTY profile.
		lipgloss.SetColorProfile(termenv.Ascii)
	}
	p := defaultPalette
	s := &Styles{
		Title:       lipgloss.NewStyle().Bold(true).Foreground(p.Title),
		Subtle:      lipgloss.NewStyle().Foreground(p.Subtle),
		Faint:       lipgloss.NewStyle().Foreground(p.Faint),
		Help:        lipgloss.NewStyle().Foreground(p.Faint),
		StatusBar:   lipgloss.NewStyle().Foreground(p.Subtle).BorderTop(true).BorderStyle(lipgloss.NormalBorder()).BorderForeground(p.Border).Padding(0, 1),
		StatusKey:   lipgloss.NewStyle().Foreground(p.Faint),
		StatusValue: lipgloss.NewStyle().Foreground(p.Subtle).Bold(true),
		Content:     lipgloss.NewStyle().Padding(1, 1),
		ListRow:     lipgloss.NewStyle(),
		ListRowSel:  lipgloss.NewStyle().Bold(true).Foreground(p.Accent),
		ListIndex:   lipgloss.NewStyle().Foreground(p.Faint),
		ListActive:  lipgloss.NewStyle().Bold(true).Foreground(p.OK),
		Toast:       lipgloss.NewStyle().Padding(0, 1).BorderStyle(lipgloss.RoundedBorder()).BorderForeground(p.Border),
		ToastOK:     lipgloss.NewStyle().Padding(0, 1).BorderStyle(lipgloss.RoundedBorder()).BorderForeground(p.OK).Foreground(p.OK),
		ToastWarn:   lipgloss.NewStyle().Padding(0, 1).BorderStyle(lipgloss.RoundedBorder()).BorderForeground(p.Warn).Foreground(p.Warn),
		ToastError:  lipgloss.NewStyle().Padding(0, 1).BorderStyle(lipgloss.RoundedBorder()).BorderForeground(p.Error).Foreground(p.Error),
		Modal:       lipgloss.NewStyle().Padding(1, 2).BorderStyle(lipgloss.RoundedBorder()).BorderForeground(p.Border),
		FieldLabel:  lipgloss.NewStyle().Foreground(p.Faint),
		FieldFocus:  lipgloss.NewStyle().Foreground(p.Accent).Bold(true),
		EmptyState:  lipgloss.NewStyle().Foreground(p.Faint).Italic(true).Padding(1, 0),
	}
	return s
}

// noColorFromEnv reports whether the NO_COLOR env var is set (any value
// triggers it, per https://no-color.org). Reads once at program start.
func noColorFromEnv() bool {
	_, ok := os.LookupEnv("NO_COLOR")
	return ok
}
