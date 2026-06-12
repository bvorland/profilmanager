package wizard

import (
	"fmt"
	"strings"

	"github.com/bvorland/profilmanager/internal/core"
)

// LoadTemplate reads an existing profile and projects reusable fields into a
// wizard State for newName. Name/Label are reset; profile-specific config-dir
// suffixes replace the old name with the new one.
func LoadTemplate(templateName, newName string) (*State, error) {
	if err := core.ValidateName(newName); err != nil {
		return nil, err
	}
	path, err := core.ProfilePath(templateName)
	if err != nil {
		return nil, err
	}
	p, err := core.Load(path)
	if err != nil {
		return nil, fmt.Errorf("load template %q: %w", templateName, err)
	}

	s := &State{
		Name:   newName,
		Label:  newName,
		Color:  p.Color,
		Preset: inferPreset(p),
	}
	if p.Azure != nil {
		s.Tenant = p.Azure.TenantID
		s.Subscription = p.Azure.SubscriptionID
		s.AzureConfigDir = substituteName(p.Azure.ConfigDir, p.Name, newName)
	}
	if p.Azd != nil {
		if s.Subscription == "" {
			s.Subscription = p.Azd.SubscriptionID
		}
		s.AzdConfigDir = substituteName(p.Azd.ConfigDir, p.Name, newName)
	}
	if p.GitHub != nil {
		s.GhAccount = p.GitHub.Account
		if len(p.GitHub.Hosts) > 0 {
			s.GhHost = p.GitHub.Hosts[0]
		}
	}
	if p.Kube != nil {
		s.KubeContext = p.Kube.Context
		s.KubeNamespace = p.Kube.Namespace
	}
	if p.Git != nil {
		s.GitAuthor = p.Git.UserName
		s.GitEmail = p.Git.UserEmail
	}
	if len(p.Env) > 0 {
		s.EnvVars = map[string]string{}
		for _, e := range p.Env {
			if e.Ref != "" {
				s.EnvVars[e.Key] = e.Ref
			} else {
				s.EnvVars[e.Key] = e.Value
			}
		}
	}
	return s, nil
}

func inferPreset(p *core.Profile) string {
	if p.GitHub != nil || p.Kube != nil || p.Git != nil {
		return PresetFullDevOps
	}
	if p.Azd != nil {
		return PresetAzureAzd
	}
	return PresetAzureOnly
}

func substituteName(path, oldName, newName string) string {
	if path == "" || oldName == "" {
		return path
	}
	return strings.ReplaceAll(path, oldName, newName)
}
