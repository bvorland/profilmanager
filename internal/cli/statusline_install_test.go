package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readSettingsForTest decodes the on-disk Copilot settings file.
func readSettingsForTest(t *testing.T) (string, map[string]any) {
	t.Helper()
	path, err := copilotSettingsPath()
	if err != nil {
		t.Fatalf("copilotSettingsPath: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read settings.json %s: %v", path, err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("parse settings.json: %v\n%s", err, data)
	}
	return path, m
}

func assertStatusLineInstalled(t *testing.T, settings map[string]any) {
	t.Helper()
	block, ok := settings["statusLine"].(map[string]any)
	if !ok {
		t.Fatalf("statusLine missing or not object: %+v", settings)
	}
	if block["type"] != "command" {
		t.Errorf("statusLine.type = %v, want command", block["type"])
	}
	cmd, _ := block["command"].(string)
	if cmd == "" {
		t.Fatalf("statusLine.command empty")
	}
	if !isPMStatuslineCommand(cmd) {
		t.Errorf("statusLine.command not recognized as pm: %q", cmd)
	}
}

func TestInstallStatusline_FromScratch(t *testing.T) {
	testEnv(t)
	stdout, _, err := runCLI(t, "prompt", "install-statusline")
	if err != nil {
		t.Fatalf("install: err=%v stdout=%s", err, stdout)
	}
	if !strings.Contains(stdout, "Statusline installed") {
		t.Errorf("install stdout missing confirmation: %s", stdout)
	}
	_, settings := readSettingsForTest(t)
	assertStatusLineInstalled(t, settings)
	// Theme must also exist on disk.
	themePath, err := statuslineThemePath()
	if err != nil {
		t.Fatalf("statuslineThemePath: %v", err)
	}
	if _, err := os.Stat(themePath); err != nil {
		t.Fatalf("theme not written: %v", err)
	}
}

func TestInstallStatusline_PreservesExistingKeys(t *testing.T) {
	testEnv(t)
	settingsPath, err := copilotSettingsPath()
	if err != nil {
		t.Fatalf("copilotSettingsPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	preserve := map[string]any{
		"theme":   "dark",
		"trusted": []any{"github.com/bvorland"},
		"nested":  map[string]any{"k": "v"},
	}
	data, _ := json.MarshalIndent(preserve, "", "  ")
	if err := os.WriteFile(settingsPath, data, 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if _, _, err := runCLI(t, "prompt", "install-statusline"); err != nil {
		t.Fatalf("install: %v", err)
	}
	_, settings := readSettingsForTest(t)
	if settings["theme"] != "dark" {
		t.Errorf("theme not preserved: %+v", settings["theme"])
	}
	if _, ok := settings["trusted"]; !ok {
		t.Errorf("trusted key dropped")
	}
	if _, ok := settings["nested"]; !ok {
		t.Errorf("nested key dropped")
	}
	assertStatusLineInstalled(t, settings)
}

func TestInstallStatusline_RefusesForeignCommand(t *testing.T) {
	testEnv(t)
	settingsPath, err := copilotSettingsPath()
	if err != nil {
		t.Fatalf("copilotSettingsPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	foreign := map[string]any{
		"statusLine": map[string]any{
			"type":    "command",
			"command": "/usr/local/bin/some-other-tool",
			"padding": 0,
		},
	}
	data, _ := json.MarshalIndent(foreign, "", "  ")
	if err := os.WriteFile(settingsPath, data, 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	_, stderr, err := runCLI(t, "prompt", "install-statusline")
	if err == nil {
		t.Fatalf("expected error refusing foreign command")
	}
	if CodeFor(err) != ExitUsage {
		t.Errorf("expected ExitUsage, got %d (err=%v)", CodeFor(err), err)
	}
	if !strings.Contains(stderr+err.Error(), "--force") {
		t.Errorf("expected --force hint, got stderr=%q err=%v", stderr, err)
	}
}

func TestInstallStatusline_ForceOverwritesForeign(t *testing.T) {
	testEnv(t)
	settingsPath, _ := copilotSettingsPath()
	_ = os.MkdirAll(filepath.Dir(settingsPath), 0o755)
	foreign := map[string]any{
		"statusLine": map[string]any{
			"type":    "command",
			"command": "/usr/local/bin/some-other-tool",
			"padding": 0,
		},
	}
	data, _ := json.MarshalIndent(foreign, "", "  ")
	_ = os.WriteFile(settingsPath, data, 0o644)

	if _, _, err := runCLI(t, "prompt", "install-statusline", "--force"); err != nil {
		t.Fatalf("install --force: %v", err)
	}
	_, settings := readSettingsForTest(t)
	assertStatusLineInstalled(t, settings)
}

func TestInstallStatusline_Idempotent(t *testing.T) {
	testEnv(t)
	if _, _, err := runCLI(t, "prompt", "install-statusline"); err != nil {
		t.Fatalf("install 1: %v", err)
	}
	settingsPath, _ := copilotSettingsPath()
	bak := settingsPath + ".bak"
	// First install starts from no file → no .bak should exist.
	if _, err := os.Stat(bak); err == nil {
		t.Errorf("unexpected .bak after first install from empty: %s", bak)
	}
	if _, _, err := runCLI(t, "prompt", "install-statusline"); err != nil {
		t.Fatalf("install 2: %v", err)
	}
	// Second install MUST create a .bak (file existed) and only once.
	bakInfoBefore, err := os.Stat(bak)
	if err != nil {
		t.Fatalf("expected .bak after second install: %v", err)
	}
	if _, _, err := runCLI(t, "prompt", "install-statusline"); err != nil {
		t.Fatalf("install 3: %v", err)
	}
	bakInfoAfter, err := os.Stat(bak)
	if err != nil {
		t.Fatalf("missing .bak after third install: %v", err)
	}
	if bakInfoAfter.ModTime() != bakInfoBefore.ModTime() {
		t.Errorf(".bak modtime changed across re-installs (want stable)")
	}
	_, settings := readSettingsForTest(t)
	assertStatusLineInstalled(t, settings)
}

func TestInstallStatusline_DryRun(t *testing.T) {
	testEnv(t)
	stdout, _, err := runCLI(t, "prompt", "install-statusline", "--dry-run")
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if !strings.Contains(stdout, "\"statusLine\"") {
		t.Errorf("dry-run stdout missing statusLine: %s", stdout)
	}
	settingsPath, _ := copilotSettingsPath()
	if _, err := os.Stat(settingsPath); err == nil {
		t.Errorf("dry-run unexpectedly wrote settings.json")
	}
}

func TestUninstallStatusline_RestoresFromBak(t *testing.T) {
	testEnv(t)
	settingsPath, _ := copilotSettingsPath()
	_ = os.MkdirAll(filepath.Dir(settingsPath), 0o755)
	seed := map[string]any{"theme": "original"}
	data, _ := json.MarshalIndent(seed, "", "  ")
	_ = os.WriteFile(settingsPath, data, 0o644)

	if _, _, err := runCLI(t, "prompt", "install-statusline"); err != nil {
		t.Fatalf("install: %v", err)
	}
	if _, _, err := runCLI(t, "prompt", "uninstall-statusline"); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	_, settings := readSettingsForTest(t)
	if _, ok := settings["statusLine"]; ok {
		t.Errorf("statusLine still present after uninstall+restore: %+v", settings)
	}
	if settings["theme"] != "original" {
		t.Errorf("theme not restored: %+v", settings["theme"])
	}
}

func TestUninstallStatusline_DeletesKeyWhenNoBak(t *testing.T) {
	testEnv(t)
	settingsPath, _ := copilotSettingsPath()
	_ = os.MkdirAll(filepath.Dir(settingsPath), 0o755)
	pmExe, _ := os.Executable()
	preset := map[string]any{
		"theme": "kept",
		"statusLine": map[string]any{
			"type":    "command",
			"command": pmExe + " statusline",
		},
	}
	data, _ := json.MarshalIndent(preset, "", "  ")
	_ = os.WriteFile(settingsPath, data, 0o644)
	// No .bak exists.

	if _, _, err := runCLI(t, "prompt", "uninstall-statusline"); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	_, settings := readSettingsForTest(t)
	if _, ok := settings["statusLine"]; ok {
		t.Errorf("statusLine still present: %+v", settings)
	}
	if settings["theme"] != "kept" {
		t.Errorf("other keys clobbered: %+v", settings)
	}
}

func TestUninstallStatusline_Noop(t *testing.T) {
	testEnv(t)
	// No settings.json at all.
	stdout, _, err := runCLI(t, "prompt", "uninstall-statusline")
	if err != nil {
		t.Fatalf("uninstall noop (missing file): %v", err)
	}
	if !strings.Contains(stdout, "no pm statusLine") {
		t.Errorf("noop stdout missing message: %s", stdout)
	}

	// settings.json with non-pm command → leaves it intact.
	settingsPath, _ := copilotSettingsPath()
	_ = os.MkdirAll(filepath.Dir(settingsPath), 0o755)
	foreign := map[string]any{
		"statusLine": map[string]any{"type": "command", "command": "/foo/bar"},
	}
	data, _ := json.MarshalIndent(foreign, "", "  ")
	_ = os.WriteFile(settingsPath, data, 0o644)

	if _, _, err := runCLI(t, "prompt", "uninstall-statusline"); err != nil {
		t.Fatalf("uninstall foreign: %v", err)
	}
	_, settings := readSettingsForTest(t)
	block, ok := settings["statusLine"].(map[string]any)
	if !ok || block["command"] != "/foo/bar" {
		t.Errorf("foreign statusLine modified: %+v", settings)
	}
}

func TestUninstallStatusline_RemoveTheme(t *testing.T) {
	testEnv(t)
	if _, _, err := runCLI(t, "prompt", "install-statusline"); err != nil {
		t.Fatalf("install: %v", err)
	}
	themePath, _ := statuslineThemePath()
	if _, err := os.Stat(themePath); err != nil {
		t.Fatalf("theme expected: %v", err)
	}
	if _, _, err := runCLI(t, "prompt", "uninstall-statusline", "--remove-theme"); err != nil {
		t.Fatalf("uninstall --remove-theme: %v", err)
	}
	if _, err := os.Stat(themePath); !os.IsNotExist(err) {
		t.Errorf("theme not removed: err=%v", err)
	}
}
