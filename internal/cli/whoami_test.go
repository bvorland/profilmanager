package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/bvorland/profilmanager/internal/providers"
)

func TestWhoamiActiveProfileSectionSet(t *testing.T) {
	testEnv(t)
	t.Setenv("PM_ACTIVE_PROFILE", "Contoso.Prod-Pilot")

	var out bytes.Buffer
	if err := renderWhoami(&out, []providers.Status{{Provider: "az", Error: "az not installed"}}, nil); err != nil {
		t.Fatalf("renderWhoami: %v", err)
	}
	got := out.String()
	wantPrefix := "── active profile: Contoso.Prod-Pilot ──\n  (profile applied via pm env apply or pm exec)\n\n── az ──"
	if !strings.HasPrefix(got, wantPrefix) {
		t.Fatalf("whoami active profile prefix mismatch\nwant prefix:\n%s\n--- got:\n%s", wantPrefix, got)
	}
}

func TestWhoamiActiveProfileSectionUnset(t *testing.T) {
	testEnv(t)
	t.Setenv("PM_ACTIVE_PROFILE", "")

	var out bytes.Buffer
	if err := renderWhoami(&out, []providers.Status{{Provider: "az", Error: "az not installed"}}, nil); err != nil {
		t.Fatalf("renderWhoami: %v", err)
	}
	got := out.String()
	wantPrefix := "── active profile: (none — host config) ──\n  (no pm profile applied to this shell; tools see host config)\n\n── az ──"
	if !strings.HasPrefix(got, wantPrefix) {
		t.Fatalf("whoami active profile prefix mismatch\nwant prefix:\n%s\n--- got:\n%s", wantPrefix, got)
	}
}

func TestWhoamiJSONIncludesActiveProfile(t *testing.T) {
	testEnv(t)
	t.Setenv("PM_ACTIVE_PROFILE", "Contoso.Prod-Pilot")

	stdout, _, err := runCLI(t, "whoami", "--json")
	if err != nil {
		t.Fatalf("whoami --json: %v", err)
	}

	var report whoamiReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("parse JSON: %v\n%s", err, stdout)
	}
	if report.ActiveProfile != "Contoso.Prod-Pilot" {
		t.Fatalf("active_profile = %q", report.ActiveProfile)
	}
}
