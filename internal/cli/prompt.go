package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
)

type promptPatchResult struct {
	replaced bool
}

func newPromptCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prompt",
		Short: "Prompt integrations",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newPromptSegmentCmd())
	cmd.AddCommand(newPromptInstallCmd())
	cmd.AddCommand(newPromptUninstallCmd())
	cmd.AddCommand(newPromptInstallStatuslineCmd())
	cmd.AddCommand(newPromptUninstallStatuslineCmd())
	return cmd
}

func newPromptSegmentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "segment --omp",
		Short: "Print the oh-my-posh pm segment JSON",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireOMP(cmd); err != nil {
				return emitError(cmd, err)
			}
			return writeJSON(cmd.OutOrStdout(), buildPMSegment())
		},
	}
	cmd.Flags().Bool("omp", false, "emit an oh-my-posh segment (only supported prompt format)")
	return cmd
}

func newPromptInstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install --omp [--theme <path>]",
		Short: "Install the pm segment into an oh-my-posh theme",
		Args:  cobra.NoArgs,
		RunE:  runPromptInstall,
	}
	cmd.Flags().Bool("omp", false, "patch an oh-my-posh theme (only supported prompt format)")
	cmd.Flags().String("theme", "", "path to the oh-my-posh theme JSON")
	cmd.Flags().Bool("dry-run", false, "print the patched JSON instead of writing the theme")
	cmd.Flags().Bool("no-backup", false, "do not create a one-time .bak before patching")
	return cmd
}

func newPromptUninstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "uninstall --omp [--theme <path>]",
		Short: "Remove the pm segment from an oh-my-posh theme",
		Args:  cobra.NoArgs,
		RunE:  runPromptUninstall,
	}
	cmd.Flags().Bool("omp", false, "patch an oh-my-posh theme (only supported prompt format)")
	cmd.Flags().String("theme", "", "path to the oh-my-posh theme JSON")
	return cmd
}

func requireOMP(cmd *cobra.Command) error {
	omp, _ := cmd.Flags().GetBool("omp")
	if !omp {
		return errInvalidUsage("use --omp; other prompts not yet supported")
	}
	return nil
}

func runPromptInstall(cmd *cobra.Command, args []string) error {
	if err := requireOMP(cmd); err != nil {
		return emitError(cmd, err)
	}
	theme, _ := cmd.Flags().GetString("theme")
	themePath, err := resolveOMPThemePath(theme)
	if err != nil {
		return emitError(cmd, err)
	}

	orig, err := os.ReadFile(themePath)
	if err != nil {
		return emitError(cmd, err)
	}
	patched, result, err := patchThemeWithResult(orig)
	if err != nil {
		return emitError(cmd, err)
	}

	noBackup, _ := cmd.Flags().GetBool("no-backup")
	makeBackup := !noBackup
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	if dryRun {
		if _, err := cmd.OutOrStdout().Write(patched); err != nil {
			return emitError(cmd, err)
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "# would write %s (%s)\n", themePath, backupSummary(themePath, makeBackup))
		return nil
	}

	if err := writePatchedTheme(themePath, patched, makeBackup); err != nil {
		return emitError(cmd, err)
	}
	if result.replaced {
		fmt.Fprintf(cmd.OutOrStdout(), "✓ Patched %s (replaced existing pm segment)\n", themePath)
		return nil
	}
	if makeBackup {
		fmt.Fprintf(cmd.OutOrStdout(), "✓ Patched %s (backed up to %s.bak)\n", themePath, themePath)
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "✓ Patched %s (no backup)\n", themePath)
	return nil
}

func runPromptUninstall(cmd *cobra.Command, args []string) error {
	if err := requireOMP(cmd); err != nil {
		return emitError(cmd, err)
	}
	theme, _ := cmd.Flags().GetString("theme")
	themePath, err := resolveOMPThemePath(theme)
	if err != nil {
		return emitError(cmd, err)
	}

	orig, err := os.ReadFile(themePath)
	if err != nil {
		return emitError(cmd, err)
	}
	if !themeHasPMSegment(orig) {
		fmt.Fprintf(cmd.OutOrStdout(), "(no pm segment found in %s)\n", themePath)
		return nil
	}

	bak := themePath + ".bak"
	if restored, err := os.ReadFile(bak); err == nil {
		if err := writePatchedTheme(themePath, restored, false); err != nil {
			return emitError(cmd, err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "✓ Removed pm segment from %s\n", themePath)
		return nil
	}

	patched, removed, err := removePMSegment(orig)
	if err != nil {
		return emitError(cmd, err)
	}
	if !removed {
		fmt.Fprintf(cmd.OutOrStdout(), "(no pm segment found in %s)\n", themePath)
		return nil
	}
	if err := writePatchedTheme(themePath, patched, false); err != nil {
		return emitError(cmd, err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "✓ Removed pm segment from %s\n", themePath)
	return nil
}

func buildPMSegment() map[string]any {
	return map[string]any{
		"type":             "text",
		"style":            "powerline",
		"powerline_symbol": "\ue0b0",
		"foreground":       "#ffffff",
		"background":       "#0078d4",
		"background_templates": []string{
			"{{ if not .Env.PM_ACTIVE_PROFILE }}#c50f1f{{ end }}",
			"{{ if .Env.PM_ACTIVE_PROFILE_BG }}{{ .Env.PM_ACTIVE_PROFILE_BG }}{{ end }}",
		},
		"template": "{{ if .Env.PM_ACTIVE_PROFILE }} {{ .Env.PM_ACTIVE_PROFILE_EMOJI }} {{ .Env.PM_ACTIVE_PROFILE }} {{ else }} ⚠️ no profile {{ end }}",
		"properties": map[string]any{
			"pm_managed": "v0.8.1",
		},
	}
}

func patchTheme(themeJSON []byte) ([]byte, error) {
	out, _, err := patchThemeWithResult(themeJSON)
	return out, err
}

func patchThemeWithResult(themeJSON []byte) ([]byte, promptPatchResult, error) {
	root, err := decodeTheme(themeJSON)
	if err != nil {
		return nil, promptPatchResult{}, err
	}

	blocks, ok := root["blocks"].([]any)
	if !ok {
		return nil, promptPatchResult{}, errors.New("theme has no .blocks array")
	}

	leftIdx := -1
	for i, b := range blocks {
		bm, _ := b.(map[string]any)
		if bm["alignment"] == "left" && bm["type"] == "prompt" {
			leftIdx = i
			break
		}
	}
	if leftIdx == -1 {
		return nil, promptPatchResult{}, errors.New(`theme has no left-aligned prompt block; pass a theme with at least one {"alignment":"left","type":"prompt"} block`)
	}

	leftBlock, _ := blocks[leftIdx].(map[string]any)
	segs, _ := leftBlock["segments"].([]any)

	replaceAt := -1
	afterSession := -1
	for i, s := range segs {
		sm, _ := s.(map[string]any)
		if segmentIsPMManaged(sm) {
			replaceAt = i
			break
		}
		if sm["type"] == "session" && afterSession == -1 {
			afterSession = i
		}
	}

	pmSeg := buildPMSegment()
	result := promptPatchResult{replaced: replaceAt >= 0}
	if replaceAt >= 0 {
		segs[replaceAt] = pmSeg
	} else if afterSession >= 0 {
		segs = append(segs[:afterSession+1], append([]any{pmSeg}, segs[afterSession+1:]...)...)
	} else {
		segs = append([]any{pmSeg}, segs...)
	}
	leftBlock["segments"] = segs
	blocks[leftIdx] = leftBlock
	root["blocks"] = blocks

	out, err := encodeTheme(root)
	if err != nil {
		return nil, promptPatchResult{}, err
	}
	return out, result, nil
}

func removePMSegment(themeJSON []byte) ([]byte, bool, error) {
	root, err := decodeTheme(themeJSON)
	if err != nil {
		return nil, false, err
	}
	blocks, ok := root["blocks"].([]any)
	if !ok {
		return nil, false, errors.New("theme has no .blocks array")
	}

	removed := false
	for bi, b := range blocks {
		bm, _ := b.(map[string]any)
		segs, _ := bm["segments"].([]any)
		if len(segs) == 0 {
			continue
		}
		blockRemoved := false
		kept := make([]any, 0, len(segs))
		for _, s := range segs {
			sm, _ := s.(map[string]any)
			if segmentIsPMManaged(sm) {
				removed = true
				blockRemoved = true
				continue
			}
			kept = append(kept, s)
		}
		if blockRemoved {
			bm["segments"] = kept
			blocks[bi] = bm
		}
	}
	if !removed {
		return themeJSON, false, nil
	}
	root["blocks"] = blocks
	out, err := encodeTheme(root)
	return out, true, err
}

func themeHasPMSegment(themeJSON []byte) bool {
	root, err := decodeTheme(themeJSON)
	if err != nil {
		return false
	}
	blocks, _ := root["blocks"].([]any)
	for _, b := range blocks {
		bm, _ := b.(map[string]any)
		segs, _ := bm["segments"].([]any)
		for _, s := range segs {
			sm, _ := s.(map[string]any)
			if segmentIsPMManaged(sm) {
				return true
			}
		}
	}
	return false
}

func segmentIsPMManaged(seg map[string]any) bool {
	if seg == nil {
		return false
	}
	props, ok := seg["properties"].(map[string]any)
	if !ok {
		return false
	}
	_, managed := props["pm_managed"]
	return managed
}

func decodeTheme(themeJSON []byte) (map[string]any, error) {
	var root map[string]any
	dec := json.NewDecoder(bytes.NewReader(themeJSON))
	dec.UseNumber()
	if err := dec.Decode(&root); err != nil {
		return nil, err
	}
	return root, nil
}

func encodeTheme(root map[string]any) ([]byte, error) {
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(root); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func resolveOMPThemePath(explicit string) (string, error) {
	if strings.TrimSpace(explicit) != "" {
		if _, err := os.Stat(explicit); err != nil {
			return "", err
		}
		return explicit, nil
	}
	return detectOMPThemePath()
}

func detectOMPThemePath() (string, error) {
	userProfile := os.Getenv("USERPROFILE")
	if userProfile == "" {
		userProfile, _ = os.UserHomeDir()
	}
	return detectOMPThemePathFromEnv(userProfile, os.Getenv("OneDrive"), os.Getenv("OneDriveCommercial"), os.Getenv("POSH_THEME"))
}

func detectOMPThemePathFromEnv(userProfile, oneDrive, oneDriveCommercial, poshTheme string) (string, error) {
	return detectOMPThemePathFromCandidates(poshTheme, powerShellProfileCandidates(userProfile, oneDrive, oneDriveCommercial))
}

func detectOMPThemePathFromCandidates(poshTheme string, profilePaths []string) (string, error) {
	poshTheme = strings.TrimSpace(poshTheme)
	poshStatus := "$POSH_THEME (not set)"
	if poshTheme != "" {
		if _, err := os.Stat(poshTheme); err == nil {
			return poshTheme, nil
		}
		poshStatus = fmt.Sprintf("$POSH_THEME (%s not found)", poshTheme)
	}

	for _, profilePath := range profilePaths {
		content, err := os.ReadFile(profilePath)
		if err != nil {
			continue
		}
		if themePath := extractOMPConfigPath(string(content)); themePath != "" {
			if _, err := os.Stat(themePath); err == nil {
				return themePath, nil
			}
		}
	}

	return "", fmt.Errorf("no oh-my-posh theme found. Tried:\n  --theme flag (not set)\n  %s\n  Microsoft.PowerShell_profile.ps1 in: %s\n\nPass --theme <path> explicitly.", poshStatus, strings.Join(profilePaths, ", "))
}

func powerShellProfileCandidates(userProfile, oneDrive, oneDriveCommercial string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(base, rest string) {
		if strings.TrimSpace(base) == "" {
			return
		}
		p := filepath.Join(base, rest)
		if !seen[p] {
			out = append(out, p)
			seen[p] = true
		}
	}
	add(userProfile, filepath.Join("Documents", "PowerShell", "Microsoft.PowerShell_profile.ps1"))
	add(userProfile, filepath.Join("Documents", "WindowsPowerShell", "Microsoft.PowerShell_profile.ps1"))
	add(oneDrive, filepath.Join("Documents", "PowerShell", "Microsoft.PowerShell_profile.ps1"))
	add(oneDriveCommercial, filepath.Join("Documents", "PowerShell", "Microsoft.PowerShell_profile.ps1"))
	return out
}

var ompConfigRe = regexp.MustCompile(`(?i)oh-my-posh\s+(?:--init\s+)?--shell\s+pwsh\s+--config\s+("[^"]+"|'[^']+'|[^\s|]+)`)

func extractOMPConfigPath(profile string) string {
	m := ompConfigRe.FindStringSubmatch(profile)
	if len(m) < 2 {
		return ""
	}
	return strings.Trim(strings.TrimSpace(m[1]), `"'`)
}

func backupSummary(path string, makeBackup bool) string {
	if !makeBackup {
		return "no backup"
	}
	if _, err := os.Stat(path + ".bak"); os.IsNotExist(err) {
		return "created .bak"
	}
	return "backup already exists"
}

func writePatchedTheme(path string, content []byte, makeBackup bool) error {
	if makeBackup {
		bak := path + ".bak"
		if _, err := os.Stat(bak); os.IsNotExist(err) {
			orig, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if err := os.WriteFile(bak, orig, 0o644); err != nil {
				return err
			}
		}
	}
	tmp := path + ".new"
	if err := os.WriteFile(tmp, content, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
