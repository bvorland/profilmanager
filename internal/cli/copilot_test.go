package cli

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/bvorland/profilmanager/internal/core"
)

func TestCopilotNoArgNonTTYRequiresProfile(t *testing.T) {
	withTempDirs(t)
	withNonTTYStdin(t)

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader(""))
	root.SetArgs([]string{"copilot"})

	if err := root.Execute(); err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(out.String(), "stdin is not a TTY") {
		t.Fatalf("expected non-TTY profile error, got:\n%s", out.String())
	}
}

func TestCopilotUnknownProfileUsesResolver(t *testing.T) {
	withTempDirs(t)
	writeProfile(t, &core.Profile{Name: "Contoso.Prod-Pilot"})

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"copilot", "NonexistentProfile"})

	if err := root.Execute(); err == nil {
		t.Fatal("expected error")
	}
	got := out.String()
	if !strings.Contains(got, "profile \"NonexistentProfile\" not found") && !strings.Contains(got, "Did you mean") {
		t.Fatalf("expected resolver profile-not-found error, got:\n%s", got)
	}
}

func TestCopilotHelpExplainsHostConfigTrap(t *testing.T) {
	withTempDirs(t)

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"copilot", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("help: %v", err)
	}
	got := strings.ToLower(out.String())
	for _, want := range []string{"copilot", "host config", "pm exec <name> -- copilot"} {
		if !strings.Contains(got, want) {
			t.Fatalf("help missing %q:\n%s", want, out.String())
		}
	}
}

func TestCopilotValidArgsMatchExecProfiles(t *testing.T) {
	withTempDirs(t)
	writeProfile(t, &core.Profile{Name: "alpha"})
	writeProfile(t, &core.Profile{Name: "bravo"})

	copilotNames, copilotDirective := profileNameCompletions(&cobra.Command{}, nil, "")
	execNames, execDirective := execProfileCompletions(&cobra.Command{}, nil, "")

	if copilotDirective != execDirective {
		t.Fatalf("directive mismatch: copilot=%v exec=%v", copilotDirective, execDirective)
	}
	if !reflect.DeepEqual(copilotNames, execNames) {
		t.Fatalf("completion mismatch: copilot=%v exec=%v", copilotNames, execNames)
	}
}
