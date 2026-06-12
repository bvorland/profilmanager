package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ProfileColor returns the lipgloss color for a PowerShell-style color
// name (matching the names used in sample/profile.ps1: Cyan, Magenta,
// Yellow, Red, DarkYellow, Green, White, Blue, DarkCyan, etc.). Unknown
// names fall back to the default subtle palette color so we never crash
// on bad input.
//
// We map deliberately to ANSI 16 (0-15) so termenv degrades to a sane
// approximation on legacy terminals and so NO_COLOR strips cleanly. The
// "Dark*" variants map to the dim ANSI slot; the unqualified names map
// to the bright slot — matching PowerShell's Write-Host -ForegroundColor
// rendering on Windows Terminal.
func ProfileColor(name string) lipgloss.Color {
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
		return defaultPalette.Subtle
	}
}

// ProfileStyle returns a bold lipgloss.Style colored for the given
// profile color name. Convenience wrapper used by the list view.
func ProfileStyle(name string) lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(ProfileColor(name))
}

// profileColor keeps existing internal call sites terse.
func profileColor(name string) lipgloss.Color {
	return ProfileColor(name)
}

// profileStyle keeps existing internal call sites terse.
func profileStyle(name string) lipgloss.Style {
	return ProfileStyle(name)
}
