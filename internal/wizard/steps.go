package wizard

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/bvorland/profilmanager/internal/core"
)

const (
	PresetAzureOnly  = "azure-only"
	PresetAzureAzd   = "azure-azd"
	PresetFullDevOps = "full-devops"
)

var (
	uuidRe      = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	ghUserRe    = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$`)
	colors      = []string{"Cyan", "Magenta", "Yellow", "Blue", "Green", "Red", "White", "DarkCyan", "DarkMagenta", "DarkYellow", "DarkBlue", "DarkGreen", "DarkRed", "Gray", "DarkGray", "Black"}
	colorLookup = makeColorLookup(colors)
)

type nameStep struct{}
type labelStep struct{}
type colorStep struct{}
type presetStep struct{}
type tenantStep struct{}
type subscriptionStep struct{}
type azureConfigDirStep struct{}
type azdConfigDirStep struct{}
type ghAccountStep struct{}
type ghHostStep struct{}
type kubeContextStep struct{}
type kubeNamespaceStep struct{}
type gitAuthorStep struct{}
type gitEmailStep struct{}

func (nameStep) ID() string                { return "name" }
func (nameStep) Title() string             { return "Profile name" }
func (nameStep) Help() string              { return "Use Group.Project format, ASCII only" }
func (nameStep) Default(*State) string     { return "" }
func (nameStep) Validate(in string) error  { return core.ValidateName(strings.TrimSpace(in)) }
func (nameStep) Apply(s *State, in string) { s.Name = strings.TrimSpace(in) }
func (nameStep) Skippable(*State) bool     { return false }

func (labelStep) ID() string              { return "label" }
func (labelStep) Title() string           { return "Profile label" }
func (labelStep) Help() string            { return "Free-form display label; defaults to profile name" }
func (labelStep) Default(s *State) string { return s.Name }
func (labelStep) Validate(string) error   { return nil }
func (labelStep) Apply(s *State, in string) {
	if strings.TrimSpace(in) == "" {
		s.Label = s.Name
		return
	}
	s.Label = in
}
func (labelStep) Skippable(*State) bool { return false }

func (colorStep) ID() string              { return "color" }
func (colorStep) Title() string           { return "Profile color" }
func (colorStep) Help() string            { return "PowerShell color name, e.g. Cyan, Magenta, Yellow" }
func (colorStep) Default(s *State) string { return suggestColor(s.Name) }
func (colorStep) Validate(in string) error {
	if _, ok := CanonicalColor(in); !ok {
		return fmt.Errorf("invalid color %q", in)
	}
	return nil
}
func (colorStep) Apply(s *State, in string) {
	if c, ok := CanonicalColor(in); ok {
		s.Color = c
	}
}
func (colorStep) Skippable(*State) bool { return false }

func (presetStep) ID() string            { return "preset" }
func (presetStep) Title() string         { return "Profile preset" }
func (presetStep) Help() string          { return "azure-only, azure-azd, or full-devops" }
func (presetStep) Default(*State) string { return PresetAzureAzd }
func (presetStep) Validate(in string) error {
	if !validPreset(strings.TrimSpace(in)) {
		return fmt.Errorf("invalid preset %q", in)
	}
	return nil
}
func (presetStep) Apply(s *State, in string) { s.Preset = strings.TrimSpace(in) }
func (presetStep) Skippable(*State) bool     { return false }

func (tenantStep) ID() string                { return "tenant" }
func (tenantStep) Title() string             { return "Azure tenant ID" }
func (tenantStep) Help() string              { return "Azure tenant GUID" }
func (tenantStep) Default(*State) string     { return "" }
func (tenantStep) Validate(in string) error  { return validateRequiredUUID("tenant", in) }
func (tenantStep) Apply(s *State, in string) { s.Tenant = strings.TrimSpace(in) }
func (tenantStep) Skippable(s *State) bool   { return !includesAzure(s.Preset) }

func (subscriptionStep) ID() string    { return "subscription" }
func (subscriptionStep) Title() string { return "Azure subscription ID" }
func (subscriptionStep) Help() string {
	return "Optional Azure subscription GUID; az login can surface it later"
}
func (subscriptionStep) Default(*State) string { return "" }
func (subscriptionStep) Validate(in string) error {
	in = strings.TrimSpace(in)
	if in == "" {
		return nil
	}
	return validateRequiredUUID("subscription", in)
}
func (subscriptionStep) Apply(s *State, in string) { s.Subscription = strings.TrimSpace(in) }
func (subscriptionStep) Skippable(*State) bool     { return false }

func (azureConfigDirStep) ID() string              { return "azure_config_dir" }
func (azureConfigDirStep) Title() string           { return "Azure config directory" }
func (azureConfigDirStep) Help() string            { return "Per-profile AZURE_CONFIG_DIR" }
func (azureConfigDirStep) Default(s *State) string { return homePath(".azure-" + s.Name) }
func (azureConfigDirStep) Validate(in string) error {
	return validateRequiredPath("azure config dir", in)
}
func (azureConfigDirStep) Apply(s *State, in string) {
	s.AzureConfigDir = mustExpandHome(strings.TrimSpace(in))
}
func (azureConfigDirStep) Skippable(s *State) bool { return !includesAzure(s.Preset) }

func (azdConfigDirStep) ID() string               { return "azd_config_dir" }
func (azdConfigDirStep) Title() string            { return "azd config directory" }
func (azdConfigDirStep) Help() string             { return "Per-profile AZD_CONFIG_DIR" }
func (azdConfigDirStep) Default(s *State) string  { return homePath(".azd-" + s.Name) }
func (azdConfigDirStep) Validate(in string) error { return validateRequiredPath("azd config dir", in) }
func (azdConfigDirStep) Apply(s *State, in string) {
	s.AzdConfigDir = mustExpandHome(strings.TrimSpace(in))
}
func (azdConfigDirStep) Skippable(s *State) bool { return !includesAzd(s.Preset) }

func (ghAccountStep) ID() string            { return "gh_account" }
func (ghAccountStep) Title() string         { return "GitHub account" }
func (ghAccountStep) Help() string          { return "GitHub username" }
func (ghAccountStep) Default(*State) string { return "" }
func (ghAccountStep) Validate(in string) error {
	in = strings.TrimSpace(in)
	if in == "" {
		return nil
	}
	if !ghUserRe.MatchString(in) {
		return fmt.Errorf("invalid GitHub username %q", in)
	}
	return nil
}
func (ghAccountStep) Apply(s *State, in string) { s.GhAccount = strings.TrimSpace(in) }
func (ghAccountStep) Skippable(s *State) bool   { return s.Preset != PresetFullDevOps }

func (ghHostStep) ID() string            { return "gh_host" }
func (ghHostStep) Title() string         { return "GitHub host" }
func (ghHostStep) Help() string          { return "GitHub hostname, e.g. github.com" }
func (ghHostStep) Default(*State) string { return "github.com" }
func (ghHostStep) Validate(in string) error {
	if strings.TrimSpace(in) == "" {
		return errors.New("GitHub host is required")
	}
	return nil
}
func (ghHostStep) Apply(s *State, in string) { s.GhHost = strings.TrimSpace(in) }
func (ghHostStep) Skippable(s *State) bool   { return s.Preset != PresetFullDevOps }

func (kubeContextStep) ID() string                { return "kube_context" }
func (kubeContextStep) Title() string             { return "Kubernetes context" }
func (kubeContextStep) Help() string              { return "kubectl context name" }
func (kubeContextStep) Default(*State) string     { return "" }
func (kubeContextStep) Validate(string) error     { return nil }
func (kubeContextStep) Apply(s *State, in string) { s.KubeContext = strings.TrimSpace(in) }
func (kubeContextStep) Skippable(s *State) bool   { return s.Preset != PresetFullDevOps }

func (kubeNamespaceStep) ID() string                { return "kube_namespace" }
func (kubeNamespaceStep) Title() string             { return "Kubernetes namespace" }
func (kubeNamespaceStep) Help() string              { return "Default kubectl namespace" }
func (kubeNamespaceStep) Default(*State) string     { return "" }
func (kubeNamespaceStep) Validate(string) error     { return nil }
func (kubeNamespaceStep) Apply(s *State, in string) { s.KubeNamespace = strings.TrimSpace(in) }
func (kubeNamespaceStep) Skippable(s *State) bool   { return s.Preset != PresetFullDevOps }

func (gitAuthorStep) ID() string                { return "git_author" }
func (gitAuthorStep) Title() string             { return "Git author name" }
func (gitAuthorStep) Help() string              { return "Default from git config user.name" }
func (gitAuthorStep) Default(*State) string     { return gitConfig("user.name") }
func (gitAuthorStep) Validate(string) error     { return nil }
func (gitAuthorStep) Apply(s *State, in string) { s.GitAuthor = strings.TrimSpace(in) }
func (gitAuthorStep) Skippable(s *State) bool   { return s.Preset != PresetFullDevOps }

func (gitEmailStep) ID() string                { return "git_email" }
func (gitEmailStep) Title() string             { return "Git author email" }
func (gitEmailStep) Help() string              { return "Default from git config user.email" }
func (gitEmailStep) Default(*State) string     { return gitConfig("user.email") }
func (gitEmailStep) Validate(string) error     { return nil }
func (gitEmailStep) Apply(s *State, in string) { s.GitEmail = strings.TrimSpace(in) }
func (gitEmailStep) Skippable(s *State) bool   { return s.Preset != PresetFullDevOps }

// CanonicalColor returns the PowerShell-style canonical color casing.
func CanonicalColor(in string) (string, bool) {
	c, ok := colorLookup[strings.ToLower(strings.TrimSpace(in))]
	return c, ok
}

func makeColorLookup(in []string) map[string]string {
	out := make(map[string]string, len(in)+2)
	for _, c := range in {
		out[strings.ToLower(c)] = c
	}
	out["grey"] = "Gray"
	out["darkgrey"] = "DarkGray"
	return out
}

func suggestColor(name string) string {
	used := map[string]struct{}{}
	for _, tmpl := range core.SuggestTemplates(name) {
		path, err := core.ProfilePath(tmpl)
		if err != nil {
			continue
		}
		p, err := core.Load(path)
		if err != nil {
			continue
		}
		if c, ok := CanonicalColor(p.Color); ok {
			used[strings.ToLower(c)] = struct{}{}
		}
	}
	for _, c := range colors {
		if _, ok := used[strings.ToLower(c)]; !ok {
			return c
		}
	}
	return colors[0]
}

func validateRequiredUUID(field, in string) error {
	in = strings.TrimSpace(in)
	if in == "" {
		return fmt.Errorf("%s is required", field)
	}
	if !uuidRe.MatchString(in) {
		return fmt.Errorf("%s must be a UUID", field)
	}
	return nil
}

func validateRequiredPath(field, in string) error {
	if strings.TrimSpace(in) == "" {
		return fmt.Errorf("%s is required", field)
	}
	_, err := expandHome(strings.TrimSpace(in))
	return err
}

func validPreset(in string) bool {
	switch in {
	case PresetAzureOnly, PresetAzureAzd, PresetFullDevOps:
		return true
	default:
		return false
	}
}

func includesAzure(preset string) bool {
	return preset == "" || preset == PresetAzureOnly || preset == PresetAzureAzd || preset == PresetFullDevOps
}

func includesAzd(preset string) bool {
	return preset == "" || preset == PresetAzureAzd || preset == PresetFullDevOps
}

func homePath(name string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, name)
}

func expandHome(path string) (string, error) {
	if path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("locate home: %w", err)
		}
		return home, nil
	}
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("locate home: %w", err)
		}
		return filepath.Join(home, path[2:]), nil
	}
	return path, nil
}

func mustExpandHome(path string) string {
	out, err := expandHome(path)
	if err != nil {
		return path
	}
	return out
}

func gitConfig(key string) string {
	out, err := exec.Command("git", "config", "--get", key).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func envEntries(vars map[string]string) []core.EnvEntry {
	if len(vars) == 0 {
		return nil
	}
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]core.EnvEntry, 0, len(keys))
	for _, k := range keys {
		v := vars[k]
		if strings.HasPrefix(v, "op://") {
			out = append(out, core.EnvEntry{Key: k, Ref: v})
			continue
		}
		out = append(out, core.EnvEntry{Key: k, Value: v})
	}
	return out
}
