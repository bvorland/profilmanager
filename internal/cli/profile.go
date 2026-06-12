package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	toml "github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"

	"github.com/bvorland/profilmanager/internal/core"
)

// profileMeta is the JSON shape returned by `pm profile list --json`. It
// is deliberately metadata-only (no env, no refs, no secrets) so listing
// profiles is always safe to paste into a bug report.
type profileMeta struct {
	Name     string `json:"name"`
	Label    string `json:"label,omitempty"`
	Color    string `json:"color,omitempty"`
	Path     string `json:"path"`
	HasAzure bool   `json:"has_azure"`
	HasAzd   bool   `json:"has_azd"`
	HasGh    bool   `json:"has_gh"`
	HasKube  bool   `json:"has_kube"`
	HasGit   bool   `json:"has_git"`
	EnvCount int    `json:"env_count"`
}

type profileListJSON struct {
	Profiles []profileMeta `json:"profiles"`
}

func metaFor(p *core.Profile, path string) profileMeta {
	return profileMeta{
		Name:     p.Name,
		Label:    p.Label,
		Color:    p.Color,
		Path:     path,
		HasAzure: p.Azure != nil,
		HasAzd:   p.Azd != nil,
		HasGh:    p.GitHub != nil,
		HasKube:  p.Kube != nil,
		HasGit:   p.Git != nil,
		EnvCount: len(p.Env),
	}
}

// listAllProfiles adapts core.ListProfiles to the older CLI table shape.
// Bad files are skipped with their errors collected.
func listAllProfiles() ([]*core.Profile, []string, []error, error) {
	items, loadErrs, err := core.ListProfiles()
	if err != nil {
		return nil, nil, nil, err
	}
	profiles := make([]*core.Profile, 0, len(items))
	paths := make([]string, 0, len(items))
	for _, item := range items {
		profiles = append(profiles, item.Profile)
		paths = append(paths, item.Path)
	}
	return profiles, paths, loadErrs, nil
}

// ---------- pm profile ----------

func newProfileCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Manage profile files (list, show, add, new, set-color, rm)",
		Long: `Operate on profile TOML files stored under the per-OS profiles dir.

Profile editing beyond the basics (env vars, provider blocks, secret refs)
belongs to the TUI; use ` + "`pm`" + ` to launch it.`,
		Args: cobra.NoArgs,
	}
	cmd.AddCommand(newProfileListCmd())
	cmd.AddCommand(newProfileShowCmd())
	cmd.AddCommand(newProfileAddCmd())
	cmd.AddCommand(newProfileNewCmd())
	cmd.AddCommand(newProfileRmCmd())
	cmd.AddCommand(newProfileSetColorCmd())
	return cmd
}

// ---------- pm profile list ----------

func newProfileListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all profiles",
		Long: `List profiles from the per-OS profiles directory.

Default output is a human-readable table. With --json, emits a stable
metadata-only array (NOT the full profile contents — secret refs and env
vars are deliberately omitted; use ` + "`pm profile show --json <name>`" + `
for one profile's full body).`,
		Args: cobra.NoArgs,
		RunE: runProfileList,
	}
	addJSONFlag(cmd)
	return cmd
}

func runProfileList(cmd *cobra.Command, _ []string) error {
	profiles, paths, loadErrs, err := listAllProfiles()
	if err != nil {
		return emitError(cmd, err)
	}

	if jsonRequested(cmd) {
		out := profileListJSON{Profiles: make([]profileMeta, 0, len(profiles))}
		for i, p := range profiles {
			out.Profiles = append(out.Profiles, metaFor(p, paths[i]))
		}
		return writeJSON(cmd.OutOrStdout(), out)
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, styleBold.Render("NAME\tLABEL\tCOLOR\tAZURE\tAZD\tGH\tKUBE\tGIT\tENV"))
	if len(profiles) == 0 {
		_ = w.Flush()
		fmt.Fprintln(cmd.OutOrStdout(), styleDim.Render("no profiles yet — `pm profile add <name>` to create one"))
		return nil
	}
	for i, p := range profiles {
		_ = paths[i]
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%d\n",
			p.Name, p.Label, p.Color,
			renderBool(p.Azure != nil),
			renderBool(p.Azd != nil),
			renderBool(p.GitHub != nil),
			renderBool(p.Kube != nil),
			renderBool(p.Git != nil),
			len(p.Env),
		)
	}
	_ = w.Flush()

	for _, le := range loadErrs {
		fmt.Fprintln(cmd.ErrOrStderr(), styleWarn.Render("warn:"), le)
	}
	return nil
}

// ---------- pm profile show ----------

func newProfileShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <name>",
		Short: "Print one profile",
		Long: `Print one profile.

Default output is a human-readable view. With --json the full TOML body
is round-tripped to JSON. With --redacted, sub IDs and tenant IDs are
masked and every secret ref becomes the literal string "<ref>" — safe
to paste into a bug report.`,
		Args:              cobra.MaximumNArgs(1),
		RunE:              runProfileShow,
		ValidArgsFunction: profileNameCompletions,
	}
	addJSONFlag(cmd)
	cmd.Flags().Bool("redacted", false, "mask subscription/tenant IDs and replace secret refs with <ref>")
	return cmd
}

func runProfileShow(cmd *cobra.Command, args []string) error {
	name, err := resolveProfileArg(cmd, args)
	if err != nil {
		return emitProfileArgError(cmd, err)
	}
	path, err := core.ProfilePath(name)
	if err != nil {
		return emitError(cmd, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return emitError(cmd, errInvalidUsage("profile %q not found at %s", name, path))
		}
		return emitError(cmd, err)
	}
	// Round-trip TOML -> generic map so we don't lose anything pm doesn't
	// know about and so the JSON shape mirrors the on-disk shape.
	var m map[string]any
	if err := toml.Unmarshal(data, &m); err != nil {
		return emitError(cmd, fmt.Errorf("parse %s: %w", path, err))
	}

	redacted, _ := cmd.Flags().GetBool("redacted")
	if redacted {
		redactProfileMap(m)
	}

	if jsonRequested(cmd) {
		return writeJSON(cmd.OutOrStdout(), m)
	}

	// Human form: re-marshal to TOML for a clean canonical view.
	out, err := toml.Marshal(m)
	if err != nil {
		return emitError(cmd, fmt.Errorf("re-encode profile: %w", err))
	}
	fmt.Fprintln(cmd.OutOrStdout(), styleBold.Render("# "+name)+"  ("+path+")")
	fmt.Fprint(cmd.OutOrStdout(), string(out))
	return nil
}

// redactProfileMap masks subscription/tenant IDs in place and replaces
// every env entry's `ref` with the placeholder "<ref>". Mutates m.
func redactProfileMap(m map[string]any) {
	for _, section := range []string{"azure", "azd"} {
		s, ok := m[section].(map[string]any)
		if !ok {
			continue
		}
		for _, key := range []string{"subscription", "tenant"} {
			if v, ok := s[key].(string); ok && v != "" {
				s[key] = maskID(v)
			}
		}
	}
	if git, ok := m["git"].(map[string]any); ok {
		if v, ok := git["user_email"].(string); ok && v != "" {
			git["user_email"] = maskEmail(v)
		}
	}
	if envs, ok := m["env"].([]any); ok {
		for _, e := range envs {
			em, ok := e.(map[string]any)
			if !ok {
				continue
			}
			if _, ok := em["ref"].(string); ok {
				em["ref"] = "<ref>"
			}
		}
	}
}

func maskID(v string) string {
	// Show first 4 chars of structured IDs (GUIDs are 36 chars) so
	// operators can still tell two profiles apart at a glance.
	if len(v) <= 8 {
		return strings.Repeat("*", len(v))
	}
	return v[:4] + strings.Repeat("*", len(v)-4)
}

func maskEmail(v string) string {
	at := strings.Index(v, "@")
	if at <= 0 {
		return strings.Repeat("*", len(v))
	}
	return strings.Repeat("*", at) + v[at:]
}

// ---------- pm profile add ----------

func newProfileAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Create a new profile (basics only)",
		Long: `Create a new profile with just the basics: name, optional label, optional color.

Provider blocks (azure/azd/gh/kube/git) and env entries are NOT set here —
that is the TUI's job. After create, prints the on-disk path and a hint
for the next step.

Fails if a profile with the same name already exists.`,
		Args: cobra.ExactArgs(1),
		RunE: runProfileAdd,
	}
	cmd.Flags().String("label", "", "display label (free-form; defaults to <name>)")
	cmd.Flags().String("color", "", "PowerShell-style color name (e.g. cyan, magenta)")
	return cmd
}

func runProfileAdd(cmd *cobra.Command, args []string) error {
	name := args[0]
	if err := core.ValidateName(name); err != nil {
		return emitError(cmd, errInvalidUsage("%v", err))
	}
	label, _ := cmd.Flags().GetString("label")
	color, _ := cmd.Flags().GetString("color")

	path, err := core.ProfilePath(name)
	if err != nil {
		return emitError(cmd, err)
	}
	if _, err := os.Stat(path); err == nil {
		return emitError(cmd, errInvalidUsage("profile %q already exists at %s", name, path))
	} else if !errors.Is(err, os.ErrNotExist) {
		return emitError(cmd, err)
	}

	p := &core.Profile{
		Schema: core.SchemaVersion,
		Name:   name,
		Label:  label,
		Color:  color,
	}
	p.Label = core.ApplyColorEmojiPrefix(p.Label, p.Color)
	if err := p.Save(path); err != nil {
		return emitError(cmd, err)
	}

	fmt.Fprintln(cmd.OutOrStdout(), styleOK.Render("created"), path)
	fmt.Fprintln(cmd.OutOrStdout(), styleDim.Render(fmt.Sprintf("  set color:  pm profile set-color %s <color>", name)))
	fmt.Fprintln(cmd.OutOrStdout(), styleDim.Render(fmt.Sprintf("  show:       pm profile show %s", name)))
	return nil
}

// ---------- pm profile rm ----------

func newProfileSetColorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set-color <name> <color>",
		Short: "Change a profile's color (and update its label emoji to match)",
		Long: `Change the color field of an existing profile.

Color must be one of the supported PowerShell-style names (Cyan, Blue, Green,
Yellow, Red, Magenta, White, Gray, Black, plus the Dark* variants — or "" to
clear). If the profile's label starts with a known color emoji prefix, that
prefix is replaced to match the new color so the dashboard, picker, and prompt
segment all stay in sync.

The on-disk profile is the source of truth — once saved, the prompt segment
will reflect the new color on the next ` + "`pm env apply`" + `.`,
		Args:              cobra.ExactArgs(2),
		RunE:              runProfileSetColor,
		ValidArgsFunction: profileNameCompletions,
	}
	return cmd
}

func runProfileSetColor(cmd *cobra.Command, args []string) error {
	rawName, color := args[0], args[1]
	name, err := core.ResolveProfileName(rawName)
	if err != nil {
		return emitError(cmd, err)
	}
	if color != "" && core.ColorEmoji(color) == "" {
		return emitError(cmd, errInvalidUsage("unknown color %q (try: Cyan, Blue, Green, Yellow, Red, Magenta, White, Gray, Black, or any Dark* variant)", color))
	}
	path, err := core.ProfilePath(name)
	if err != nil {
		return emitError(cmd, err)
	}
	p, err := core.Load(path)
	if err != nil {
		return emitError(cmd, err)
	}
	oldColor := p.Color
	if oldColor == color {
		fmt.Fprintln(cmd.OutOrStdout(), styleDim.Render(fmt.Sprintf("color already %q — no change", color)))
		return nil
	}
	p.Color = color
	p.Label = core.ReplaceColorEmojiPrefix(p.Label, color)
	if err := p.Save(path); err != nil {
		return emitError(cmd, err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s color: %s → %s   label: %s\n",
		styleOK.Render("✓ updated"),
		styleDim.Render(colorOrNone(oldColor)),
		styleDim.Render(colorOrNone(color)),
		styleDim.Render(colorOrNone(p.Label)),
	)
	if active := os.Getenv("PM_ACTIVE_PROFILE"); active == name {
		fmt.Fprintln(cmd.OutOrStdout(), styleDim.Render("  re-apply to refresh the prompt segment:  pm env apply "+name+" | Invoke-Expression"))
	}
	return nil
}

func colorOrNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

// ---------- pm profile rm ----------

func newProfileRmCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rm <name>",
		Short: "Delete a profile",
		Long: `Delete a profile.

Without --force, asks for y/N confirmation on the terminal. With --force,
deletion is idempotent (no error if the profile is already absent).
Non-interactive callers (no TTY on stdin) MUST pass --force.`,
		Args:              cobra.MaximumNArgs(1),
		RunE:              runProfileRm,
		ValidArgsFunction: profileNameCompletions,
	}
	cmd.Flags().Bool("force", false, "skip confirmation; idempotent if already absent")
	return cmd
}

func runProfileRm(cmd *cobra.Command, args []string) error {
	force, _ := cmd.Flags().GetBool("force")
	name, err := resolveProfileArg(cmd, args)
	if err != nil {
		if force && len(args) == 1 {
			if _, resolveErr := core.ResolveProfileName(args[0]); errors.Is(resolveErr, core.ErrNotFound) {
				if err := core.ValidateName(args[0]); err != nil {
					return emitError(cmd, errInvalidUsage("%v", err))
				}
				path, pathErr := core.ProfilePath(args[0])
				if pathErr != nil {
					return emitError(cmd, pathErr)
				}
				if _, statErr := os.Stat(path); errors.Is(statErr, os.ErrNotExist) {
					return nil
				}
			}
		}
		return emitProfileArgError(cmd, err)
	}

	path, err := core.ProfilePath(name)
	if err != nil {
		return emitError(cmd, err)
	}
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if force {
				return nil // idempotent
			}
			return emitError(cmd, errInvalidUsage("profile %q not found at %s", name, path))
		}
		return emitError(cmd, err)
	}

	if !force {
		if !stdinIsTTY(cmd) {
			return emitError(cmd, errInvalidUsage("refusing to delete without --force (no TTY on stdin)"))
		}
		fmt.Fprintf(cmd.OutOrStdout(), "delete profile %q at %s? [y/N] ", name, path)
		reader := bufio.NewReader(cmd.InOrStdin())
		line, _ := reader.ReadString('\n')
		ans := strings.ToLower(strings.TrimSpace(line))
		if ans != "y" && ans != "yes" {
			fmt.Fprintln(cmd.OutOrStdout(), styleDim.Render("aborted"))
			return nil
		}
	}

	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return emitError(cmd, err)
	}
	fmt.Fprintln(cmd.OutOrStdout(), styleOK.Render("removed"), path)
	return nil
}

// stdinIsTTY reports whether stdin appears to be an interactive
// terminal. False under tests and `pm ... < input` redirection — both of
// which we want to require --force from.
func stdinIsTTY(cmd *cobra.Command) bool {
	f, ok := cmd.InOrStdin().(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// (unused but useful in tests; silences linters if someone removes the call)
var _ = json.Valid
