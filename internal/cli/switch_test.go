package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/bvorland/profilmanager/internal/core"
	"github.com/bvorland/profilmanager/internal/state"
)

func TestSwitchSetsActiveAndLast(t *testing.T) {
	withTempDirs(t)
	writeProfile(t, &core.Profile{Name: "sw"})

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"switch", "sw"})
	if err := root.Execute(); err != nil {
		t.Fatalf("switch: %v -- %s", err, out.String())
	}
	active, _, err := state.GetActiveProfile()
	if err != nil {
		t.Fatalf("GetActiveProfile: %v", err)
	}
	if active != "sw" {
		t.Fatalf("active = %q, want sw", active)
	}
	last, err := state.GetLastProfile()
	if err != nil {
		t.Fatalf("GetLastProfile: %v", err)
	}
	if last != "sw" {
		t.Fatalf("last = %q, want sw", last)
	}
	if !strings.Contains(out.String(), "switch is metadata only") {
		t.Fatalf("expected honest activation hint, got:\n%s", out.String())
	}
}

func TestSwitchRefusesUnknownProfile(t *testing.T) {
	withTempDirs(t)

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"switch", "missing"})
	if err := root.Execute(); err == nil {
		t.Fatalf("expected error for unknown profile")
	}
}

func TestSwitchQuietSuppressesHint(t *testing.T) {
	withTempDirs(t)
	writeProfile(t, &core.Profile{Name: "q"})

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"switch", "q", "--quiet"})
	if err := root.Execute(); err != nil {
		t.Fatalf("switch: %v", err)
	}
	if strings.Contains(out.String(), "to apply the profile env") {
		t.Fatalf("--quiet should suppress hint, got:\n%s", out.String())
	}
}
