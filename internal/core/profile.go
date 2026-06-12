package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

// SchemaVersion is the only profile schema version this build understands.
// Bump in lockstep with migrations.
const SchemaVersion = "1"

// nameRe constrains profile names to ASCII letters, digits, dot, dash,
// underscore. This is both the on-disk filename and an identifier used by
// shells, env vars, and `pm exec`, so we keep it boring on purpose.
var nameRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

var profileColorEmoji = map[string]string{
	"Cyan":        "🔵",
	"DarkCyan":    "🔵",
	"Blue":        "🔷",
	"DarkBlue":    "🔷",
	"Green":       "🟢",
	"DarkGreen":   "🟢",
	"Yellow":      "🟡",
	"DarkYellow":  "🟠",
	"Red":         "🔴",
	"DarkRed":     "🔴",
	"Magenta":     "🟣",
	"DarkMagenta": "🟣",
	"White":       "⚪",
	"Gray":        "⚪",
	"DarkGray":    "⚫",
	"Black":       "⚫",
}

// profileColorHex maps PowerShell-style color names to hex codes used as
// background colors for the oh-my-posh prompt segment. Picked so each maps
// visually to its profileColorEmoji glyph while staying dark enough to
// contrast with white foreground text.
var profileColorHex = map[string]string{
	"Cyan":        "#0078d4",
	"DarkCyan":    "#005a9e",
	"Blue":        "#1f6feb",
	"DarkBlue":    "#0e639c",
	"Green":       "#1f883d",
	"DarkGreen":   "#116329",
	"Yellow":      "#a16207",
	"DarkYellow":  "#bf6900",
	"Red":         "#c50f1f",
	"DarkRed":     "#8b0000",
	"Magenta":     "#8250df",
	"DarkMagenta": "#6e40c9",
	"White":       "#6e7681",
	"Gray":        "#57606a",
	"DarkGray":    "#24292f",
	"Black":       "#1c1c1c",
}

// Profile is the v1 on-disk profile model. Sub-structs are pointers so the
// TOML round-trip preserves "not set" vs "set to zero value".
type Profile struct {
	Schema string         `toml:"schema"`
	Name   string         `toml:"name"`
	Label  string         `toml:"label,omitempty"`
	Color  string         `toml:"color,omitempty"`
	Azure  *AzureProfile  `toml:"azure,omitempty"`
	Azd    *AzdProfile    `toml:"azd,omitempty"`
	GitHub *GitHubProfile `toml:"gh,omitempty"`
	Kube   *KubeProfile   `toml:"kube,omitempty"`
	Git    *GitIdentity   `toml:"git,omitempty"`
	Env    []EnvEntry     `toml:"env,omitempty"`
}

// AzureProfile holds the subset of `az` config we manage. Extend this
// when implementing the az provider.
type AzureProfile struct {
	ConfigDir      string `toml:"config_dir,omitempty"`
	SubscriptionID string `toml:"subscription,omitempty"`
	TenantID       string `toml:"tenant,omitempty"`
}

// AzdProfile holds the subset of `azd` config we manage.
type AzdProfile struct {
	ConfigDir      string `toml:"config_dir,omitempty"`
	SubscriptionID string `toml:"subscription,omitempty"`
}

// GitHubProfile selects a `gh` account; Hosts pins it to one or more
// GitHub Enterprise hostnames (empty means just github.com).
type GitHubProfile struct {
	Account string   `toml:"user,omitempty"`
	Hosts   []string `toml:"hosts,omitempty"`
}

// KubeProfile pins kubectl context and optional default namespace.
type KubeProfile struct {
	Context   string `toml:"context,omitempty"`
	Namespace string `toml:"namespace,omitempty"`
}

// GitIdentity overrides git user.name/user.email/user.signingKey for
// processes spawned through `pm exec`.
type GitIdentity struct {
	UserName   string `toml:"user_name,omitempty"`
	UserEmail  string `toml:"user_email,omitempty"`
	SigningKey string `toml:"signing_key,omitempty"`
}

// EnvEntry is a single env var contributed by the profile. Exactly one of
// Value or Ref must be set: Value is a literal, Ref is a secret reference
// resolved at exec time by an internal/secrets.Resolver.
type EnvEntry struct {
	Key   string `toml:"key"`
	Value string `toml:"value,omitempty"`
	Ref   string `toml:"ref,omitempty"`
}

// ValidateName checks that a profile name is safe to use as a filename
// and as a shell identifier. Returns nil on success.
func ValidateName(name string) error {
	if name == "" {
		return errors.New("profile name is required")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("invalid profile name %q: reserved", name)
	}
	if !nameRe.MatchString(name) {
		return fmt.Errorf("invalid profile name %q: only ASCII letters, digits, '.', '-', '_' allowed", name)
	}
	return nil
}

// Validate enforces v1 schema invariants.
func (p *Profile) Validate() error {
	if p == nil {
		return errors.New("profile is nil")
	}
	if p.Schema != SchemaVersion {
		return fmt.Errorf("unsupported schema %q (want %q)", p.Schema, SchemaVersion)
	}
	if err := ValidateName(p.Name); err != nil {
		return err
	}
	for i, e := range p.Env {
		if e.Key == "" {
			return fmt.Errorf("env[%d]: key is required", i)
		}
		switch {
		case e.Value != "" && e.Ref != "":
			return fmt.Errorf("env[%d] %q: 'value' and 'ref' are mutually exclusive", i, e.Key)
		case e.Value == "" && e.Ref == "":
			return fmt.Errorf("env[%d] %q: one of 'value' or 'ref' must be set", i, e.Key)
		}
	}
	return nil
}

// DisplayLabel returns Label when set, otherwise Name, with the color emoji
// glyph prefixed when known. Idempotent: if the underlying string already
// starts with one of the 16 known color emoji, it is returned unchanged.
func (p *Profile) DisplayLabel() string {
	base := p.Label
	if base == "" {
		base = p.Name
	}
	return ApplyColorEmojiPrefix(base, p.Color)
}

// ColorEmoji returns the emoji glyph for a profile color, or "" if unknown.
func ColorEmoji(color string) string {
	return profileColorEmoji[color]
}

// ColorHex returns the hex background color (e.g. "#0078d4") used by the
// oh-my-posh prompt segment for a profile color, or "" if unknown.
// Callers should fall back to a sensible default when the result is empty.
func ColorHex(color string) string {
	return profileColorHex[color]
}

// ApplyColorEmojiPrefix returns label with the matching color emoji prefix.
// Existing known emoji prefixes are preserved so re-saves stay idempotent and
// user-selected emoji choices are not overwritten.
func ApplyColorEmojiPrefix(label, color string) string {
	if label == "" {
		return label
	}
	for _, glyph := range profileColorEmoji {
		if strings.HasPrefix(label, glyph) {
			return label
		}
	}
	emoji := ColorEmoji(color)
	if emoji == "" {
		return label
	}
	return emoji + " " + label
}

// ReplaceColorEmojiPrefix strips any known color emoji prefix (plus the
// single trailing space we add in ApplyColorEmojiPrefix) and reapplies the
// glyph for the new color. Unlike ApplyColorEmojiPrefix this is *not*
// idempotent-against-mismatched-colors — it actively swaps the prefix.
// Use when the color has just changed and the label needs to follow.
func ReplaceColorEmojiPrefix(label, color string) string {
	if label == "" {
		return label
	}
	for _, glyph := range profileColorEmoji {
		if strings.HasPrefix(label, glyph) {
			label = strings.TrimPrefix(label, glyph)
			label = strings.TrimPrefix(label, " ")
			break
		}
	}
	if label == "" {
		// stripped down to nothing — return new glyph alone if any
		return ColorEmoji(color)
	}
	emoji := ColorEmoji(color)
	if emoji == "" {
		return label
	}
	return emoji + " " + label
}

// Load reads a profile TOML file from disk and validates it.
func Load(path string) (*Profile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read profile %s: %w", path, err)
	}
	var p Profile
	if err := toml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse profile %s: %w", path, err)
	}
	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("validate profile %s: %w", path, err)
	}
	return &p, nil
}

// Save validates the profile and atomically writes it to disk
// (write-temp + rename in the same directory, never partial files).
func (p *Profile) Save(path string) error {
	if err := p.Validate(); err != nil {
		return err
	}
	data, err := toml.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshal profile: %w", err)
	}
	return atomicWrite(path, data, 0o644)
}

// atomicWrite writes data to a temp file in the destination directory,
// fsyncs it, then renames over the target. Callers get either the old
// contents or the new contents — never a half-written file.
func atomicWrite(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create dir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".pm-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("sync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		cleanup()
		return fmt.Errorf("chmod temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("rename %s -> %s: %w", tmpName, path, err)
	}
	return nil
}
