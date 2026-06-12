package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/bvorland/profilmanager/internal/core"
	"github.com/bvorland/profilmanager/internal/state"
)

func runDashboard(cmd *cobra.Command, _ []string) error {
	active := strings.TrimSpace(os.Getenv("PM_ACTIVE_PROFILE"))
	if active == "" {
		active = "(none — host config)"
	}

	recent, loadErrs, err := core.ListProfilesByModTime(5)
	if err != nil {
		return emitError(cmd, err)
	}
	all, _, err := core.ListProfiles()
	if err != nil {
		return emitError(cmd, err)
	}

	sessionID, sessionSource := state.SessionID()
	out := cmd.OutOrStdout()
	fmt.Fprintln(out, styleBold.Render("profilmanager"))
	fmt.Fprintln(out)
	fmt.Fprintln(out, styleBold.Render("Active profile:"), styleOK.Render(active))
	if agentContext, agentVar := core.InAgentContext(); agentContext && strings.TrimSpace(os.Getenv("PM_ACTIVE_PROFILE")) == "" {
		fmt.Fprintln(out)
		fmt.Fprintln(out, styleWarn.Render(fmt.Sprintf("⚠️  Inside an AI agent (%s set) without an active profile.", agentVar)))
		fmt.Fprintln(out, styleWarn.Render("    Tools (including copilot) will see host config."))
		fmt.Fprintln(out, styleWarn.Render("    Run:  pm env apply <name> | Invoke-Expression"))
		fmt.Fprintln(out)
	}
	fmt.Fprintf(out, "%s %d\n", styleBold.Render("Total profiles:"), len(all))
	fmt.Fprintf(out, "%s %s %s\n", styleBold.Render("Session id:"), sessionID, styleDim.Render("("+sessionSource+")"))
	fmt.Fprintln(out)

	fmt.Fprintln(out, styleBold.Render("Recent profiles:"))
	if len(recent) == 0 {
		fmt.Fprintln(out, "  "+styleDim.Render("none yet — run `pm profile new` or `pm profile add <name>`"))
	} else {
		for _, item := range recent {
			p := item.Profile
			fmt.Fprintf(out, "  %s %s\n", profileLabelStyle(p.Color).Render(p.DisplayLabel()), styleDim.Render(p.Name))
		}
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, styleBold.Render("Common commands:"))
	for _, line := range []string{
		"pm tui",
		"pm profile new",
		"pm env apply",
		"pm exec",
		"pm doctor",
	} {
		fmt.Fprintln(out, "  "+line)
	}

	for _, le := range loadErrs {
		fmt.Fprintln(cmd.ErrOrStderr(), styleWarn.Render("warn:"), le)
	}
	return nil
}

func profileLabelStyle(color string) lipgloss.Style {
	if !colorsOn {
		return lipgloss.NewStyle().Bold(true)
	}
	return lipgloss.NewStyle().Bold(true).Foreground(profileColor(color))
}

func profileColor(name string) lipgloss.Color {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "black":
		return lipgloss.Color("0")
	case "darkblue":
		return lipgloss.Color("4")
	case "darkgreen":
		return lipgloss.Color("2")
	case "darkcyan":
		return lipgloss.Color("6")
	case "darkred":
		return lipgloss.Color("1")
	case "darkmagenta":
		return lipgloss.Color("5")
	case "darkyellow":
		return lipgloss.Color("3")
	case "gray", "grey":
		return lipgloss.Color("7")
	case "darkgray", "darkgrey":
		return lipgloss.Color("8")
	case "blue":
		return lipgloss.Color("12")
	case "green":
		return lipgloss.Color("10")
	case "cyan":
		return lipgloss.Color("14")
	case "red":
		return lipgloss.Color("9")
	case "magenta":
		return lipgloss.Color("13")
	case "yellow":
		return lipgloss.Color("11")
	case "white":
		return lipgloss.Color("15")
	default:
		return lipgloss.Color("7")
	}
}
