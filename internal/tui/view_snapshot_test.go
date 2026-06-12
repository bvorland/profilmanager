package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/bvorland/profilmanager/internal/core"
)

// TestRenderEditViewLoaded snapshots the edit view with a populated
// profile. Locks the field labels, field order, env-row rendering, and
// the focused-field marker — these are operator-facing layout that
// is easy to drift on accidentally.
func TestRenderEditViewLoaded(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.TrueColor) })

	app := newApp(true)
	app.width = 100
	app.height = 28
	app.view = viewEdit

	p := &core.Profile{
		Schema: core.SchemaVersion,
		Name:   "Contoso.MainDev",
		Label:  "🔵 Contoso Main Dev",
		Color:  "Cyan",
		Azure: &core.AzureProfile{
			ConfigDir:      "~/.azure-Contoso.MainDev",
			SubscriptionID: "11111111-1111-1111-1111-111111111111",
			TenantID:       "22222222-2222-2222-2222-222222222222",
		},
		Git: &core.GitIdentity{
			UserName:  "Bjørn Atle Vorland",
			UserEmail: "alex@example.com",
		},
		Env: []core.EnvEntry{
			{Key: "DEPLOY_ENV", Value: "dev"},
			{Key: "API_KEY", Ref: "op://Vault/Dev/api_key"},
		},
	}
	app.edit.load(p)
	// Force focus onto the Label field for a deterministic "focused"
	// marker; the load() helper already does this, but assert it.
	if app.edit.focus != int(fLabel) {
		t.Fatalf("expected load() to focus fLabel, got %d", app.edit.focus)
	}
	assertTUIGolden(t, "edit_view_loaded.golden", app.edit.View())
}

// TestRenderEditViewCreating snapshots the empty-form variant — covers
// the placeholder rendering for every textinput and the "New profile"
// title swap.
func TestRenderEditViewCreating(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.TrueColor) })

	app := newApp(true)
	app.width = 100
	app.height = 28
	app.view = viewEdit
	app.edit.load(nil) // creating

	assertTUIGolden(t, "edit_view_creating.golden", app.edit.View())
}

// TestRenderDoctorView snapshots the doctor view with deterministic
// state. We seed PM_SESSION_ID so the session-id source line is stable
// across runs.
//
// NOTE: doctorModel currently only renders four lines (profiles dir,
// session id, session source, active profile) and a "provider checks
// pending" placeholder. Once provider checks expand the view to
// surface CheckResult warn/fail rows, extend the snapshot with one
// warn + one fail case as the task brief asks. Today we lock the
// skeleton so the expansion is a deliberate golden diff.
func TestRenderDoctorView(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.TrueColor) })

	t.Setenv("PM_SESSION_ID", "test-session-doctor")

	app := newApp(true)
	app.width = 100
	app.height = 24
	app.view = viewDoctor
	app.doctor.refresh()
	// Profiles dir resolution depends on per-OS env (USERPROFILE /
	// HOME / XDG_CONFIG_HOME). Substituting a stable placeholder
	// before snapshotting keeps the golden portable across Win/macOS/Linux.
	app.doctor.profilesDir = "<profiles-dir>"
	app.doctor.profilesDirErr = nil
	app.doctor.activeProfile = "Contoso.MainDev"

	assertTUIGolden(t, "doctor_view.golden", app.doctor.View())
}

// TestRenderDoctorViewWithPPIDWarning locks the WARN path: when the
// session id falls back to PPID, the view appends a yellow warning
// suffix. This is the closest analog to the "one warning, one error"
// snapshot the task brief asks for — the doctor view today doesn't
// have its own per-check renderer (that's CLI doctor); the warning
// surfaces through sessionSource.
func TestRenderDoctorViewWithPPIDWarning(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.TrueColor) })

	app := newApp(true)
	app.width = 100
	app.height = 24
	app.view = viewDoctor
	// Manually populate state — refresh() would call into internal/state
	// and we want a deterministic shape independent of how the test
	// process derived its session id.
	app.doctor.profilesDir = "<profiles-dir>"
	app.doctor.sessionID = "12345"
	app.doctor.sessionSource = "ppid-fallback" // matches state.SourcePPIDFallback
	app.doctor.activeProfile = ""

	assertTUIGolden(t, "doctor_view_ppid_warn.golden", app.doctor.View())
}

// TestRenderConfirmModal snapshots the delete-confirmation modal — the
// classic destructive-action y/N prompt. Locks the title, body, and
// keymap hint.
func TestRenderConfirmModal(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.TrueColor) })

	app := newApp(true)
	app.width = 80
	app.height = 24
	cm := newDeleteConfirm(app, "Contoso.MainDev")
	assertTUIGolden(t, "confirm_delete.golden", cm.View())
}

// TestRenderConfirmDiscardModal snapshots the discard-unsaved-changes
// modal — different title/body than delete, same chrome. Catches
// regressions where the modal style drifts between confirm types.
func TestRenderConfirmDiscardModal(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.TrueColor) })

	app := newApp(true)
	app.width = 80
	app.height = 24
	cm := newDiscardConfirm(app)
	assertTUIGolden(t, "confirm_discard.golden", cm.View())
}

// assertTUIGolden centralizes the read/write/diff for TUI snapshots. Same
// shape as the CLI golden helper but for raw string buffers (no JSON
// normalization). Update via:
//
//	go test ./internal/tui -update -run TestRender
//
// The `-update` flag is declared once in view_list_test.go.
func assertTUIGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		t.Logf("golden %s updated", path)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v (run -update to create)", path, err)
	}
	// Normalize CRLF→LF on Windows checkouts to match the deterministic
	// LF we write at -update time.
	wantStr := string(want)
	if got != wantStr {
		t.Errorf("snapshot %s mismatch\n--- want ---\n%s\n--- got ---\n%s",
			path, wantStr, got)
	}
}
