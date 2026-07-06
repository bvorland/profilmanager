package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/bvorland/profilmanager/internal/core"
)

// requirePowerShell skips the test if neither pwsh nor (on Windows)
// powershell is on PATH. Integration tests that drive mj-export.ps1
// need this — pure-Go unit tests don't.
func requirePowerShell(t *testing.T) {
	t.Helper()
	if _, err := findPowerShell(); err != nil {
		t.Skipf("SKIP: %v", err)
	}
}

// isolateHome reroutes $HOME / %USERPROFILE% / config-base env vars into
// tmp so core.ProfilesDir() lands in a sandbox.
func isolateHome(t *testing.T, tmp string) {
	t.Helper()
	t.Setenv("HOME", tmp)
	switch runtime.GOOS {
	case "windows":
		t.Setenv("USERPROFILE", tmp)
		t.Setenv("APPDATA", filepath.Join(tmp, "appdata"))
		t.Setenv("LOCALAPPDATA", filepath.Join(tmp, "localappdata"))
	default:
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "xdgcfg"))
		t.Setenv("XDG_STATE_HOME", filepath.Join(tmp, "xdgstate"))
	}
}

// TestEmbeddedScriptMatchesCanonical fails the build if scripts/mj-export.ps1
// and the embedded copy under internal/cli drift. They're the same script
// and must stay byte-identical.
func TestEmbeddedScriptMatchesCanonical(t *testing.T) {
	t.Parallel()
	canon, err := os.ReadFile(filepath.Join("..", "..", "scripts", "mj-export.ps1"))
	if err != nil {
		t.Fatalf("read canonical script: %v", err)
	}
	if !bytes.Equal(canon, mjExportScript) {
		t.Fatalf("scripts/mj-export.ps1 and internal/cli/mj-export.ps1 drift; copy the canonical over.")
	}
}

func TestReadMJEnvFile_SkipsAndClassifies(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "p.env")
	body := strings.Join([]string{
		"# header comment",
		"",
		"AZURE_PROFILE_NAME=Acme.Dev",
		"AZURE_CONFIG_DIR=C:\\Users\\x\\.azure-Acme.Dev",
		"AZD_CONFIG_DIR=C:\\Users\\x\\.azd-Acme.Dev",
		"FOO=bar",
		"  SPACED_KEY  =  has-leading-spaces-preserved",
		"API_KEY=op://Vault/Item/password",
		"EMPTY=",
		"  # indented comment",
		"NO_EQUALS_LINE",
		"TRAILING=value-with-trailing-tabs\t\t",
	}, "\r\n")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, err := readMJEnvFile(path)
	if err != nil {
		t.Fatalf("readMJEnvFile: %v", err)
	}

	want := map[string]core.EnvEntry{
		"FOO":        {Key: "FOO", Value: "bar"},
		"SPACED_KEY": {Key: "SPACED_KEY", Value: "  has-leading-spaces-preserved"},
		"API_KEY":    {Key: "API_KEY", Ref: "op://Vault/Item/password"},
		"TRAILING":   {Key: "TRAILING", Value: "value-with-trailing-tabs"},
	}
	got := map[string]core.EnvEntry{}
	for _, e := range entries {
		got[e.Key] = e
	}
	if len(got) != len(want) {
		t.Fatalf("entry count: got %d want %d (%+v)", len(got), len(want), entries)
	}
	for k, w := range want {
		g, ok := got[k]
		if !ok {
			t.Errorf("missing key %q", k)
			continue
		}
		if g.Value != w.Value || g.Ref != w.Ref {
			t.Errorf("entry %q mismatch: got %+v want %+v", k, g, w)
		}
	}
	// Confirm the hoisted keys really were dropped, even though they
	// were present in the file.
	for _, banned := range []string{"AZURE_PROFILE_NAME", "AZURE_CONFIG_DIR", "AZD_CONFIG_DIR"} {
		if _, present := got[banned]; present {
			t.Errorf("hoisted key %q leaked into env entries", banned)
		}
	}
}

func TestReadMJEnvFile_MissingFileIsNotAnError(t *testing.T) {
	t.Parallel()
	entries, err := readMJEnvFile(filepath.Join(t.TempDir(), "absent.env"))
	if err != nil {
		t.Fatalf("missing file should be nil-nil, got err=%v", err)
	}
	if entries != nil {
		t.Fatalf("entries: got %+v want nil", entries)
	}
}

// writeFixtureEnvFiles populates a profiles dir matching the three names
// in testdata/profile.ps1. One has secrets, one has plain values, one has
// no .env at all.
func writeFixtureEnvFiles(t *testing.T, profilesDir string) {
	t.Helper()
	if err := os.MkdirAll(profilesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeEnv := func(name, body string) {
		if err := os.WriteFile(filepath.Join(profilesDir, name+".env"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeEnv("Acme.Dev", strings.Join([]string{
		"AZURE_PROFILE_NAME=Acme.Dev",
		"AZURE_CONFIG_DIR=ignored",
		"AZD_CONFIG_DIR=ignored",
		"OPENAI_API_KEY=op://Personal/openai/api-key",
		"FEATURE_FLAG=enabled",
	}, "\n"))
	writeEnv("Acme.Prod", strings.Join([]string{
		"AZURE_PROFILE_NAME=Acme.Prod",
		"DB_PASSWORD=op://Work/prod-db/password",
	}, "\n"))
	// Sandbox-1 deliberately has no .env file.
}

func TestImportMJ_DryRunWritesNothing(t *testing.T) {
	requirePowerShell(t)
	tmp := t.TempDir()
	isolateHome(t, tmp)

	profilesEnvDir := filepath.Join(tmp, "PSProfiles")
	writeFixtureEnvFiles(t, profilesEnvDir)

	var stdout, stderr bytes.Buffer
	err := runImportMJ(&stdout, &stderr, importMjOpts{
		FromPowerShell: filepath.Join("testdata", "profile.ps1"),
		ProfilesDir:    profilesEnvDir,
		DryRun:         true,
		JSON:           true,
	})
	if err != nil {
		t.Fatalf("runImportMJ: %v\nstderr=%s", err, stderr.String())
	}

	var summary importSummary
	if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil {
		t.Fatalf("parse summary JSON: %v\nstdout=%s", err, stdout.String())
	}
	wantNames := []string{"Acme.Dev", "Acme.Prod", "Sandbox-1"}
	if len(summary.Imported) != len(wantNames) {
		t.Fatalf("imported count: got %d want %d (%+v)", len(summary.Imported), len(wantNames), summary)
	}
	for _, n := range wantNames {
		if !containsString(summary.Imported, n) {
			t.Errorf("missing %q in imported (%v)", n, summary.Imported)
		}
	}
	if len(summary.Errors) != 0 {
		t.Errorf("unexpected errors: %+v", summary.Errors)
	}

	pd, err := core.ProfilesDir()
	if err != nil {
		t.Fatalf("ProfilesDir: %v", err)
	}
	entries, _ := os.ReadDir(pd)
	if len(entries) != 0 {
		t.Errorf("dry-run wrote files into %s: %v", pd, entries)
	}
}

func TestImportMJ_RunTwiceIsIdempotent(t *testing.T) {
	requirePowerShell(t)
	tmp := t.TempDir()
	isolateHome(t, tmp)

	profilesEnvDir := filepath.Join(tmp, "PSProfiles")
	writeFixtureEnvFiles(t, profilesEnvDir)

	opts := importMjOpts{
		FromPowerShell: filepath.Join("testdata", "profile.ps1"),
		ProfilesDir:    profilesEnvDir,
		JSON:           true,
	}

	// Pass 1: writes everything.
	var out1, errBuf1 bytes.Buffer
	if err := runImportMJ(&out1, &errBuf1, opts); err != nil {
		t.Fatalf("pass 1: %v\nstderr=%s", err, errBuf1.String())
	}
	var s1 importSummary
	if err := json.Unmarshal(out1.Bytes(), &s1); err != nil {
		t.Fatalf("pass 1 parse: %v", err)
	}
	if len(s1.Imported) != 3 || len(s1.Skipped) != 0 {
		t.Fatalf("pass 1 unexpected: %+v", s1)
	}

	pd, _ := core.ProfilesDir()
	for _, n := range []string{"Acme.Dev", "Acme.Prod", "Sandbox-1"} {
		f := filepath.Join(pd, n+".toml")
		if _, err := os.Stat(f); err != nil {
			t.Errorf("expected %s after pass 1: %v", f, err)
		}
	}

	// Snapshot Acme.Dev contents so we can confirm pass 2 doesn't touch them.
	preBytes, err := os.ReadFile(filepath.Join(pd, "Acme.Dev.toml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	// Pass 2: every profile already exists, so all skipped.
	var out2, errBuf2 bytes.Buffer
	if err := runImportMJ(&out2, &errBuf2, opts); err != nil {
		t.Fatalf("pass 2: %v\nstderr=%s", err, errBuf2.String())
	}
	var s2 importSummary
	if err := json.Unmarshal(out2.Bytes(), &s2); err != nil {
		t.Fatalf("pass 2 parse: %v", err)
	}
	if len(s2.Skipped) != 3 || len(s2.Imported) != 0 {
		t.Errorf("pass 2 should skip all: %+v", s2)
	}
	postBytes, _ := os.ReadFile(filepath.Join(pd, "Acme.Dev.toml"))
	if !bytes.Equal(preBytes, postBytes) {
		t.Errorf("pass 2 modified Acme.Dev.toml despite no --force")
	}
}

func TestImportMJ_ForceOverwrites(t *testing.T) {
	requirePowerShell(t)
	tmp := t.TempDir()
	isolateHome(t, tmp)

	profilesEnvDir := filepath.Join(tmp, "PSProfiles")
	writeFixtureEnvFiles(t, profilesEnvDir)
	opts := importMjOpts{
		FromPowerShell: filepath.Join("testdata", "profile.ps1"),
		ProfilesDir:    profilesEnvDir,
		JSON:           true,
	}

	// Pass 1
	var out1, errBuf1 bytes.Buffer
	if err := runImportMJ(&out1, &errBuf1, opts); err != nil {
		t.Fatalf("pass 1: %v\nstderr=%s", err, errBuf1.String())
	}

	// Tamper with Acme.Dev.toml.
	pd, _ := core.ProfilesDir()
	devPath := filepath.Join(pd, "Acme.Dev.toml")
	tampered := []byte("schema = \"1\"\nname = \"Acme.Dev\"\nlabel = \"TAMPERED\"\n")
	if err := os.WriteFile(devPath, tampered, 0o644); err != nil {
		t.Fatal(err)
	}

	// Pass 2 with --force overwrites.
	opts.Force = true
	var out2, errBuf2 bytes.Buffer
	if err := runImportMJ(&out2, &errBuf2, opts); err != nil {
		t.Fatalf("pass 2: %v\nstderr=%s", err, errBuf2.String())
	}
	var s2 importSummary
	if err := json.Unmarshal(out2.Bytes(), &s2); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(s2.Imported) != 3 || len(s2.Skipped) != 0 {
		t.Errorf("force should re-import all 3: %+v", s2)
	}

	after, err := os.ReadFile(devPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(after, tampered) {
		t.Error("--force did not overwrite tampered file")
	}
	if !strings.Contains(string(after), "Acme Dev") {
		t.Errorf("rewritten Acme.Dev.toml missing original label: %s", string(after))
	}
}

// TestImportMJ_ProfileContentsRoundTrip confirms the produced TOML loads
// back through core.Load with all expected fields populated — in
// particular that op:// refs land in Ref and not Value, and that
// AZURE_CONFIG_DIR is hoisted into the typed Azure block.
func TestImportMJ_ProfileContentsRoundTrip(t *testing.T) {
	requirePowerShell(t)
	tmp := t.TempDir()
	isolateHome(t, tmp)

	profilesEnvDir := filepath.Join(tmp, "PSProfiles")
	writeFixtureEnvFiles(t, profilesEnvDir)
	opts := importMjOpts{
		FromPowerShell: filepath.Join("testdata", "profile.ps1"),
		ProfilesDir:    profilesEnvDir,
		JSON:           true,
	}
	var out, errBuf bytes.Buffer
	if err := runImportMJ(&out, &errBuf, opts); err != nil {
		t.Fatalf("import: %v\nstderr=%s", err, errBuf.String())
	}

	pd, _ := core.ProfilesDir()
	dev, err := core.Load(filepath.Join(pd, "Acme.Dev.toml"))
	if err != nil {
		t.Fatalf("load Acme.Dev: %v", err)
	}
	if dev.Label != "🟢 Acme Dev" || dev.Color != "Green" {
		t.Errorf("label/color: %+v", dev)
	}
	if dev.Azure == nil || !strings.Contains(dev.Azure.ConfigDir, ".azure-Acme.Dev") {
		t.Errorf("azure: %+v", dev.Azure)
	}
	if dev.Azd == nil || !strings.Contains(dev.Azd.ConfigDir, ".azd-Acme.Dev") {
		t.Errorf("azd: %+v", dev.Azd)
	}
	// Two entries: one ref, one value. AZURE_* must not appear.
	envByKey := map[string]core.EnvEntry{}
	for _, e := range dev.Env {
		envByKey[e.Key] = e
	}
	if e, ok := envByKey["OPENAI_API_KEY"]; !ok || e.Ref != "op://Personal/openai/api-key" || e.Value != "" {
		t.Errorf("OPENAI_API_KEY: got %+v", e)
	}
	if e, ok := envByKey["FEATURE_FLAG"]; !ok || e.Value != "enabled" || e.Ref != "" {
		t.Errorf("FEATURE_FLAG: got %+v", e)
	}
	for _, banned := range []string{"AZURE_PROFILE_NAME", "AZURE_CONFIG_DIR", "AZD_CONFIG_DIR"} {
		if _, present := envByKey[banned]; present {
			t.Errorf("hoisted key %q leaked into TOML env entries", banned)
		}
	}

	// Sandbox-1 had no .env file — still imported, just no env entries.
	sb, err := core.Load(filepath.Join(pd, "Sandbox-1.toml"))
	if err != nil {
		t.Fatalf("load Sandbox-1: %v", err)
	}
	if len(sb.Env) != 0 {
		t.Errorf("Sandbox-1 unexpectedly has env entries: %+v", sb.Env)
	}
}

func TestFindPowerShellWhenAbsent(t *testing.T) {
	// Empty PATH on this process forces both lookups to fail; the function
	// must report something useful instead of blowing up.
	t.Setenv("PATH", "")
	if runtime.GOOS == "windows" {
		t.Setenv("PATHEXT", "")
	}
	_, err := findPowerShell()
	if err == nil {
		t.Fatal("expected error on empty PATH; got nil")
	}
	if !strings.Contains(err.Error(), "powershell") {
		t.Errorf("error should mention powershell, got: %v", err)
	}
	// Also confirm the error type still satisfies errors.Is for the
	// generic case — not strictly required, just defensive.
	var _ = errors.Is(err, os.ErrNotExist) // no assertion; just exercise
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
