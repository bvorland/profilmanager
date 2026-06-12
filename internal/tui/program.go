package tui

import (
	"context"
	"errors"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

// RunOptions tweak how Run() boots. Most callers want the zero value
// (auto-detect NO_COLOR from env, run on stdin/stdout). The CLI layer
// uses ForceNoColor to honor a --no-color flag.
type RunOptions struct {
	ForceNoColor bool
	StartView    string // "doctor" to land on the doctor view; default is list
}

// ErrNotATerminal is returned by Run when stdout isn't a TTY. The CLI
// wrapper translates this into a friendly stderr message pointing the
// operator at `pm --help`.
var ErrNotATerminal = errors.New("pm tui requires a terminal; stdout is not a TTY")

// Run launches the TUI. Blocks until the user quits or ctx is cancelled.
//
// Honors NO_COLOR (env) and opts.ForceNoColor (CLI flag). Refuses to
// start when stdout isn't a TTY — Bubble Tea would otherwise paint
// escape sequences into piped output and corrupt downstream tools.
func Run(ctx context.Context, opts RunOptions) error {
	if !isTerminal(os.Stdout) {
		return ErrNotATerminal
	}
	noColor := opts.ForceNoColor || noColorFromEnv()

	app := newApp(noColor)
	if opts.StartView == "doctor" {
		app.view = viewDoctor
		app.doctor.refresh()
	}

	teaOpts := []tea.ProgramOption{
		tea.WithAltScreen(),
		tea.WithContext(ctx),
	}
	p := tea.NewProgram(app, teaOpts...)
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("tui: %w", err)
	}
	return nil
}

// isTerminal reports whether f looks like a real interactive terminal.
// Uses os.File.Stat to avoid pulling in the x/term dep — the
// ModeCharDevice bit is what every TTY check ultimately reduces to on
// both Windows and POSIX.
func isTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	st, err := f.Stat()
	if err != nil {
		return false
	}
	return (st.Mode() & os.ModeCharDevice) != 0
}

// Compile-time assertion that App implements tea.Model. Keeps refactors
// from silently breaking the contract.
var _ tea.Model = (*App)(nil)
