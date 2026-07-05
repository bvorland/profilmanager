package cli

import (
	"strings"
	"testing"

	"github.com/bvorland/profilmanager/internal/state"
)

func TestBareRootShowsDashboard(t *testing.T) {
	testEnv(t)
	t.Setenv("PM_ACTIVE_PROFILE", "alpha")
	_, _, err := runCLI(t, "profile", "add", "alpha", "--label", "Alpha Profile", "--color", "cyan")
	if err != nil {
		t.Fatalf("add alpha: %v", err)
	}

	stdout, _, err := runCLI(t)
	if err != nil {
		t.Fatalf("bare pm: %v", err)
	}
	for _, want := range []string{
		"profilmanager",
		"Active profile:",
		"alpha",
		"Total profiles: 1",
		"Recent profiles:",
		"Alpha Profile",
		"Common commands:",
		"pm tui",
		"pm doctor",
		"Session id:",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("dashboard missing %q:\n%s", want, stdout)
		}
	}
}

func TestRootHelpStillShowsCobraHelp(t *testing.T) {
	testEnv(t)
	stdout, _, err := runCLI(t, "--help")
	if err != nil {
		t.Fatalf("pm --help: %v", err)
	}
	if !strings.Contains(stdout, "Usage:") || !strings.Contains(stdout, "Available Commands:") {
		t.Fatalf("--help did not render cobra help:\n%s", stdout)
	}
	if strings.Contains(stdout, "Recent profiles:") {
		t.Fatalf("--help should not render dashboard:\n%s", stdout)
	}
}

func TestDashboardAgentContextWarning(t *testing.T) {
	t.Run("no agent context does not warn", func(t *testing.T) {
		testEnv(t)
		t.Setenv("PM_SESSION_ID", "")
		t.Setenv("COPILOT_SESSION_ID", "")
		t.Setenv("COPILOT_CLI_SESSION_ID", "")
		t.Setenv("CLAUDE_SESSION_ID", "")
		t.Setenv("AIDER_SESSION_ID", "")
		t.Setenv("PM_ACTIVE_PROFILE", "")

		stdout, _, err := runCLI(t)
		if err != nil {
			t.Fatalf("bare pm: %v", err)
		}
		if strings.Contains(stdout, "Inside an AI agent") {
			t.Fatalf("dashboard should not warn outside agent context:\n%s", stdout)
		}
	})

	t.Run("pm session without active profile warns", func(t *testing.T) {
		testEnv(t)
		t.Setenv("PM_SESSION_ID", "pm-session")
		t.Setenv("COPILOT_SESSION_ID", "")
		t.Setenv("COPILOT_CLI_SESSION_ID", "")
		t.Setenv("CLAUDE_SESSION_ID", "")
		t.Setenv("AIDER_SESSION_ID", "")
		t.Setenv("PM_ACTIVE_PROFILE", "")

		stdout, _, err := runCLI(t)
		if err != nil {
			t.Fatalf("bare pm: %v", err)
		}
		for _, want := range []string{
			"Inside an AI agent (PM_SESSION_ID set)",
			"pm env apply",
		} {
			if !strings.Contains(stdout, want) {
				t.Fatalf("dashboard missing %q:\n%s", want, stdout)
			}
		}
	})

	t.Run("copilot session var is reported", func(t *testing.T) {
		testEnv(t)
		t.Setenv("PM_SESSION_ID", "")
		t.Setenv("COPILOT_SESSION_ID", "copilot-session")
		t.Setenv("COPILOT_CLI_SESSION_ID", "")
		t.Setenv("CLAUDE_SESSION_ID", "")
		t.Setenv("AIDER_SESSION_ID", "")
		t.Setenv("PM_ACTIVE_PROFILE", "")

		stdout, _, err := runCLI(t)
		if err != nil {
			t.Fatalf("bare pm: %v", err)
		}
		if !strings.Contains(stdout, "COPILOT_SESSION_ID") {
			t.Fatalf("dashboard should mention COPILOT_SESSION_ID:\n%s", stdout)
		}
	})

	t.Run("active profile suppresses warning", func(t *testing.T) {
		testEnv(t)
		t.Setenv("PM_SESSION_ID", "pm-session")
		t.Setenv("COPILOT_SESSION_ID", "")
		t.Setenv("COPILOT_CLI_SESSION_ID", "")
		t.Setenv("CLAUDE_SESSION_ID", "")
		t.Setenv("AIDER_SESSION_ID", "")
		t.Setenv("PM_ACTIVE_PROFILE", "Contoso.Foo")

		stdout, _, err := runCLI(t)
		if err != nil {
			t.Fatalf("bare pm: %v", err)
		}
		if strings.Contains(stdout, "Inside an AI agent") {
			t.Fatalf("dashboard should not warn with active profile:\n%s", stdout)
		}
	})

	t.Run("session profile suppresses warning", func(t *testing.T) {
		testEnv(t)
		t.Setenv("PM_SESSION_ID", "pm-session")
		t.Setenv("COPILOT_SESSION_ID", "")
		t.Setenv("COPILOT_CLI_SESSION_ID", "")
		t.Setenv("CLAUDE_SESSION_ID", "")
		t.Setenv("AIDER_SESSION_ID", "")
		t.Setenv("PM_ACTIVE_PROFILE", "")

		if err := state.SetActiveProfile("Contoso.Foo"); err != nil {
			t.Fatalf("SetActiveProfile: %v", err)
		}

		stdout, _, err := runCLI(t)
		if err != nil {
			t.Fatalf("bare pm: %v", err)
		}
		if strings.Contains(stdout, "Inside an AI agent") {
			t.Fatalf("dashboard should not warn with session active profile:\n%s", stdout)
		}
		if !strings.Contains(stdout, "Contoso.Foo") {
			t.Fatalf("dashboard should show session active profile:\n%s", stdout)
		}
	})
}
