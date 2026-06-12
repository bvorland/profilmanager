package tui

import (
	"os"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// TestNoColorFromEnv verifies that NO_COLOR (any value, even empty)
// triggers plain rendering — per https://no-color.org.
func TestNoColorFromEnv(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	if !noColorFromEnv() {
		t.Errorf("expected NO_COLOR='' to be treated as set")
	}
	t.Setenv("NO_COLOR", "1")
	if !noColorFromEnv() {
		t.Errorf("expected NO_COLOR='1' to be treated as set")
	}
	os.Unsetenv("NO_COLOR")
	if noColorFromEnv() {
		t.Errorf("expected unset NO_COLOR to be off")
	}
}

// TestDoctorViewMentionsSession confirms the doctor view surfaces the
// session id and source produced by internal/state.
func TestDoctorViewMentionsSession(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.TrueColor) })

	t.Setenv("PM_SESSION_ID", "test-session-123")
	app := newApp(true)
	app.width = 80
	app.height = 24
	app.doctor.refresh()
	out := app.doctor.View()
	if !contains(out, "test-session-123") {
		t.Errorf("doctor view missing session id; got:\n%s", out)
	}
	if !contains(out, "pm-session") {
		t.Errorf("doctor view missing session source 'pm-session'; got:\n%s", out)
	}
	if !contains(out, "Provider checks: pending") {
		t.Errorf("doctor view missing provider-pending placeholder; got:\n%s", out)
	}
}

// TestProfileColorFallback verifies unknown color names don't crash and
// fall back to the subtle palette color.
func TestProfileColorFallback(t *testing.T) {
	if got := profileColor("nonsense"); got != defaultPalette.Subtle {
		t.Errorf("expected fallback to subtle, got %v", got)
	}
	if got := profileColor("Cyan"); got != lipgloss.Color("14") {
		t.Errorf("Cyan should map to ANSI 14, got %v", got)
	}
	if got := profileColor("DarkYellow"); got != lipgloss.Color("3") {
		t.Errorf("DarkYellow should map to ANSI 3, got %v", got)
	}
}
