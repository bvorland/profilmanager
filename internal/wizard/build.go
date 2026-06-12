package wizard

import (
	"errors"
	"fmt"
	"strings"

	"github.com/bvorland/profilmanager/internal/core"
)

// Build assembles a core.Profile from a populated State and runs final
// cross-field validation. EnvVars are intentionally data-only here; future TUI
// editing can decide whether to expose them as a separate step.
func Build(s *State) (*core.Profile, error) {
	if s == nil {
		return nil, errors.New("wizard state is nil")
	}
	preset := strings.TrimSpace(s.Preset)
	if preset == "" {
		preset = PresetAzureAzd
	}
	if !validPreset(preset) {
		return nil, fmt.Errorf("invalid preset %q", s.Preset)
	}
	color := strings.TrimSpace(s.Color)
	if color != "" {
		c, ok := CanonicalColor(color)
		if !ok {
			return nil, fmt.Errorf("invalid color %q", s.Color)
		}
		color = c
	}
	p := &core.Profile{
		Schema: core.SchemaVersion,
		Name:   strings.TrimSpace(s.Name),
		Label:  s.Label,
		Color:  color,
		Env:    envEntries(s.EnvVars),
	}
	if err := core.ValidateName(p.Name); err != nil {
		return nil, err
	}

	if includesAzure(preset) {
		if err := validateRequiredUUID("tenant", s.Tenant); err != nil {
			return nil, err
		}
		if strings.TrimSpace(s.Subscription) != "" {
			if err := validateRequiredUUID("subscription", s.Subscription); err != nil {
				return nil, err
			}
		}
		azureDir, err := requiredExpandedPath("azure config dir", s.AzureConfigDir)
		if err != nil {
			return nil, err
		}
		p.Azure = &core.AzureProfile{
			ConfigDir:      azureDir,
			SubscriptionID: strings.TrimSpace(s.Subscription),
			TenantID:       strings.TrimSpace(s.Tenant),
		}
	}
	if includesAzd(preset) {
		azdDir, err := requiredExpandedPath("azd config dir", s.AzdConfigDir)
		if err != nil {
			return nil, err
		}
		p.Azd = &core.AzdProfile{
			ConfigDir:      azdDir,
			SubscriptionID: strings.TrimSpace(s.Subscription),
		}
	}
	if preset == PresetFullDevOps {
		if err := (ghAccountStep{}).Validate(s.GhAccount); err != nil {
			return nil, err
		}
		if strings.TrimSpace(s.GhAccount) == "" {
			return nil, errors.New("GitHub account is required")
		}
		host := strings.TrimSpace(s.GhHost)
		if host == "" {
			host = "github.com"
		}
		p.GitHub = &core.GitHubProfile{Account: strings.TrimSpace(s.GhAccount), Hosts: []string{host}}
		p.Kube = &core.KubeProfile{Context: strings.TrimSpace(s.KubeContext), Namespace: strings.TrimSpace(s.KubeNamespace)}
		p.Git = &core.GitIdentity{UserName: strings.TrimSpace(s.GitAuthor), UserEmail: strings.TrimSpace(s.GitEmail)}
	}

	p.Label = core.ApplyColorEmojiPrefix(p.Label, p.Color)
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return p, nil
}

func requiredExpandedPath(field, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	out, err := expandHome(value)
	if err != nil {
		return "", err
	}
	return out, nil
}
