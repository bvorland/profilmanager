package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

// jsonFlag is the canonical name of the --json flag every machine-output
// verb attaches to itself. Centralized so help text stays consistent.
const jsonFlag = "json"

// addJSONFlag attaches --json (bool) to cmd. Each verb that emits
// machine-readable output should call this in its constructor.
func addJSONFlag(cmd *cobra.Command) {
	cmd.Flags().Bool(jsonFlag, false, "emit machine-readable JSON on stdout (stable schema; see decisions)")
}

// jsonRequested returns true if --json is set on cmd or any parent.
func jsonRequested(cmd *cobra.Command) bool {
	v, _ := cmd.Flags().GetBool(jsonFlag)
	return v
}

// writeJSON encodes v as pretty-printed JSON with a trailing newline. The
// pretty form is the default because pm is operator-facing; agents that
// want compact JSON can pipe through `jq -c`.
func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// jsonErrorEnvelope is the stable shape printed to stderr in --json mode
// when a command fails. Keeping a fixed shape lets agents branch on
// `code` rather than parse free-form English.
type jsonErrorEnvelope struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

// emitError prints err on cmd.ErrOrStderr in the appropriate format. In
// --json mode it writes a single-line envelope; otherwise it writes a
// `pm <verb>: <msg>` line in red (when colors are enabled). Always
// returns err so callers can `return emitError(cmd, err)` from RunE.
func emitError(cmd *cobra.Command, err error) error {
	if err == nil {
		return nil
	}
	w := cmd.ErrOrStderr()
	if jsonRequested(cmd) {
		_ = writeJSON(w, jsonErrorEnvelope{
			Error: err.Error(),
			Code:  codeStringFor(err),
		})
		return err
	}
	fmt.Fprintln(w, styleError.Render("pm "+cmdPath(cmd)+":")+" "+err.Error())
	return err
}

// codeStringFor maps an ExitErrorT to a stable JSON code string.
func codeStringFor(err error) string {
	switch CodeFor(err) {
	case ExitOK:
		return "ok"
	case ExitUsage:
		return "invalid_usage"
	case ExitUnwiredStub:
		return "not_implemented"
	default:
		return "error"
	}
}

// cmdPath returns the verb chain without the root binary name, suitable
// for prefixing error messages ("profile add", "session init").
func cmdPath(cmd *cobra.Command) string {
	parts := strings.Fields(cmd.CommandPath())
	if len(parts) <= 1 {
		return cmd.Use
	}
	return strings.Join(parts[1:], " ")
}

// ---------- color / styles ----------
//
// We only colorize when stdout is a TTY AND NO_COLOR is unset. Otherwise
// lipgloss renders bare text — which is what we want when output is
// piped, redirected, or consumed by an agent.

var (
	colorsOn = colorsEnabled()

	styleOK    = newColorStyle(lipgloss.Color("10")) // bright green
	styleWarn  = newColorStyle(lipgloss.Color("11")) // bright yellow
	styleError = newColorStyle(lipgloss.Color("9"))  // bright red
	styleDim   = newColorStyle(lipgloss.Color("8"))  // dark grey
	styleBold  = lipgloss.NewStyle().Bold(true)
)

func colorsEnabled() bool {
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

func newColorStyle(c lipgloss.Color) lipgloss.Style {
	if !colorsOn {
		return lipgloss.NewStyle()
	}
	return lipgloss.NewStyle().Bold(true).Foreground(c)
}

// renderBool returns "yes" / "no" with optional green/dim coloring. Used
// by the `profile list` table.
func renderBool(b bool) string {
	if b {
		return styleOK.Render("yes")
	}
	return styleDim.Render("no")
}
