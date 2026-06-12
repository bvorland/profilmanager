package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bvorland/profilmanager/internal/cli/themes"
)

// pmStatuslineSuffixes are the (case-insensitive) command suffixes we
// recognize as pm-installed. We use suffix matching so users who move
// pm.exe still upgrade cleanly; the install-time exact-match against
// os.Executable() handles non-standard binary names like the test binary.
var pmStatuslineSuffixes = []string{
	"\\pm.exe statusline",
	"/pm.exe statusline",
	"/pm statusline",
	"pm.exe statusline",
	"pm statusline",
}

func newPromptInstallStatuslineCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install-statusline",
		Short: "Install pm as Copilot CLI's statusLine command",
		Long: `Wire ` + "`pm statusline`" + ` into Copilot CLI's ~/.copilot/settings.json
so a profile-aware status line appears below the Copilot CLI input box.

Writes the embedded oh-my-posh theme to the per-OS data dir (Windows:
%LOCALAPPDATA%\profilmanager\statusline.omp.json) and patches
settings.json with a ` + "`statusLine`" + ` block pointing at the current pm
binary. The original settings.json is backed up to .bak once.`,
		Args: cobra.NoArgs,
		RunE: runPromptInstallStatusline,
	}
	cmd.Flags().Bool("dry-run", false, "print the patched settings.json instead of writing")
	cmd.Flags().Bool("force", false, "overwrite an existing foreign statusLine.command, and re-write the theme even if it exists")
	return cmd
}

func newPromptUninstallStatuslineCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "uninstall-statusline",
		Short: "Remove pm's statusLine entry from Copilot CLI settings",
		Args:  cobra.NoArgs,
		RunE:  runPromptUninstallStatusline,
	}
	cmd.Flags().Bool("remove-theme", false, "also delete the on-disk statusline.omp.json theme")
	return cmd
}

func runPromptInstallStatusline(cmd *cobra.Command, args []string) error {
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	force, _ := cmd.Flags().GetBool("force")

	pmExe, err := os.Executable()
	if err != nil {
		return emitError(cmd, fmt.Errorf("resolve pm binary path: %w", err))
	}
	if abs, err := filepath.Abs(pmExe); err == nil {
		pmExe = abs
	}
	if _, err := os.Stat(pmExe); err != nil {
		return emitError(cmd, fmt.Errorf("pm binary not found at %s: %w", pmExe, err))
	}

	themePath, err := writeEmbeddedStatuslineTheme(force)
	if err != nil {
		return emitError(cmd, err)
	}

	settingsPath, err := copilotSettingsPath()
	if err != nil {
		return emitError(cmd, err)
	}
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return emitError(cmd, fmt.Errorf("create copilot settings dir: %w", err))
	}

	settings, raw, err := readCopilotSettings(settingsPath)
	if err != nil {
		return emitError(cmd, err)
	}

	if existing, isPM := extractStatuslineCommand(settings); existing != "" && !isPM && !force {
		return emitError(cmd, errInvalidUsage("existing statusLine.command points to %q; use --force to overwrite", existing))
	}

	settings["statusLine"] = map[string]any{
		"type":    "command",
		"command": statuslineCommandString(pmExe),
		"padding": json.Number("0"),
	}

	out, err := encodeCopilotSettings(settings)
	if err != nil {
		return emitError(cmd, err)
	}

	if dryRun {
		if _, werr := cmd.OutOrStdout().Write(out); werr != nil {
			return emitError(cmd, werr)
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "# would write %s\n", settingsPath)
		return nil
	}

	makeBackup := len(raw) > 0
	if err := writePatchedTheme(settingsPath, out, makeBackup); err != nil {
		return emitError(cmd, err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "%s Statusline installed. Restart your Copilot CLI session to see it.\n", styleOK.Render("✓"))
	fmt.Fprintf(cmd.OutOrStdout(), "  settings : %s\n", settingsPath)
	fmt.Fprintf(cmd.OutOrStdout(), "  theme    : %s\n", themePath)
	fmt.Fprintf(cmd.OutOrStdout(), "  pm.exe   : %s\n", pmExe)
	fmt.Fprintln(cmd.OutOrStdout(), styleDim.Render("  hint: if you later edit the theme file, run `oh-my-posh cache clear` for omp to pick up changes."))
	return nil
}

func runPromptUninstallStatusline(cmd *cobra.Command, args []string) error {
	removeTheme, _ := cmd.Flags().GetBool("remove-theme")

	settingsPath, err := copilotSettingsPath()
	if err != nil {
		return emitError(cmd, err)
	}
	settings, _, err := readCopilotSettings(settingsPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintln(cmd.OutOrStdout(), "(no pm statusLine found)")
			return nil
		}
		return emitError(cmd, err)
	}

	current, isPM := extractStatuslineCommand(settings)
	if current == "" {
		fmt.Fprintln(cmd.OutOrStdout(), "(no pm statusLine found)")
		return maybeRemoveStatuslineTheme(cmd, removeTheme)
	}
	if !isPM {
		fmt.Fprintf(cmd.OutOrStdout(), "(statusLine.command points to %q — not removing)\n", current)
		return maybeRemoveStatuslineTheme(cmd, removeTheme)
	}

	bak := settingsPath + ".bak"
	if restored, err := os.ReadFile(bak); err == nil {
		if err := writePatchedTheme(settingsPath, restored, false); err != nil {
			return emitError(cmd, err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s Restored %s from .bak\n", styleOK.Render("✓"), settingsPath)
		return maybeRemoveStatuslineTheme(cmd, removeTheme)
	}

	delete(settings, "statusLine")
	out, err := encodeCopilotSettings(settings)
	if err != nil {
		return emitError(cmd, err)
	}
	if err := writePatchedTheme(settingsPath, out, false); err != nil {
		return emitError(cmd, err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s Removed pm statusLine from %s\n", styleOK.Render("✓"), settingsPath)
	return maybeRemoveStatuslineTheme(cmd, removeTheme)
}

func maybeRemoveStatuslineTheme(cmd *cobra.Command, remove bool) error {
	if !remove {
		return nil
	}
	path, err := statuslineThemePath()
	if err != nil {
		return nil
	}
	if err := os.Remove(path); err == nil {
		fmt.Fprintf(cmd.OutOrStdout(), "  removed theme: %s\n", path)
	}
	return nil
}

// writeEmbeddedStatuslineTheme writes the embedded theme to disk if missing
// or if force is set. Returns the resolved path either way.
func writeEmbeddedStatuslineTheme(force bool) (string, error) {
	path, err := statuslineThemePath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("create theme dir: %w", err)
	}
	if !force {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	if err := os.WriteFile(path, themes.StatuslineOMP, 0o644); err != nil {
		return "", fmt.Errorf("write theme: %w", err)
	}
	return path, nil
}

// copilotSettingsPath returns the Copilot CLI settings.json path. Copilot
// CLI hardcodes ~/.copilot/settings.json on all platforms, so we don't
// branch by GOOS.
func copilotSettingsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".copilot", "settings.json"), nil
}

// readCopilotSettings loads settings.json into a map, preserving any keys
// we don't know about. Missing file returns ({}, nil, nil); other errors
// (parse failure, permission) bubble up. The raw bytes are returned so
// callers can detect "file existed and had content" for backup decisions.
func readCopilotSettings(path string) (map[string]any, []byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil, nil
		}
		return nil, nil, fmt.Errorf("read %s: %w", path, err)
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return map[string]any{}, data, nil
	}
	var root map[string]any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&root); err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if root == nil {
		root = map[string]any{}
	}
	return root, data, nil
}

func encodeCopilotSettings(root map[string]any) ([]byte, error) {
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(root); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// extractStatuslineCommand returns the current statusLine.command string
// and a flag indicating whether it appears to be pm-managed. Returns
// ("", false) when statusLine is absent OR when the command is empty —
// both treated as "nothing to refuse / nothing to remove".
func extractStatuslineCommand(settings map[string]any) (cmd string, isPM bool) {
	raw, ok := settings["statusLine"]
	if !ok {
		return "", false
	}
	block, ok := raw.(map[string]any)
	if !ok {
		return "", false
	}
	c, _ := block["command"].(string)
	if strings.TrimSpace(c) == "" {
		return "", false
	}
	return c, isPMStatuslineCommand(c)
}

// isPMStatuslineCommand returns true if cmd looks like a pm-installed
// statusline command. We recognize three forms:
//   - any command whose suffix matches pm's binary name + " statusline"
//     (handles renamed-drive installs and the Windows/POSIX split)
//   - an exact match against `statuslineCommandString(os.Executable())`,
//     so test binaries and renamed pm binaries also round-trip cleanly
func isPMStatuslineCommand(cmd string) bool {
	trimmed := strings.TrimSpace(cmd)
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)
	for _, suffix := range pmStatuslineSuffixes {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	if exe, err := os.Executable(); err == nil {
		if strings.EqualFold(trimmed, statuslineCommandString(exe)) {
			return true
		}
	}
	return false
}

// statuslineCommandString builds the value used for settings.json
// `statusLine.command`. On Windows, Copilot CLI runs the command via
// `cmd.exe /c <command>`, so an absolute path with backslashes embedded
// in a JSON string works as a single token without further quoting.
func statuslineCommandString(pmExe string) string {
	if runtime.GOOS == "windows" {
		return pmExe + " statusline"
	}
	// On POSIX shells, quote the path to survive spaces in directory names.
	if strings.ContainsAny(pmExe, " \t") {
		return "\"" + pmExe + "\" statusline"
	}
	return pmExe + " statusline"
}
