package wizard

// State accumulates field values as the user advances through steps.
type State struct {
	Name           string
	Label          string
	Color          string
	Preset         string
	Tenant         string
	Subscription   string
	AzureConfigDir string
	AzdConfigDir   string
	GhAccount      string
	GhHost         string
	KubeContext    string
	KubeNamespace  string
	GitAuthor      string
	GitEmail       string
	EnvVars        map[string]string
}

// Step is a single question in the wizard.
type Step interface {
	ID() string
	Title() string
	Help() string
	Default(s *State) string
	Validate(input string) error
	Apply(s *State, input string)
	Skippable(s *State) bool
}

// Steps returns the canonical step order for a new-profile wizard.
func Steps() []Step {
	return []Step{
		nameStep{},
		labelStep{},
		colorStep{},
		presetStep{},
		tenantStep{},
		subscriptionStep{},
		azureConfigDirStep{},
		azdConfigDirStep{},
		ghAccountStep{},
		ghHostStep{},
		kubeContextStep{},
		kubeNamespaceStep{},
		gitAuthorStep{},
		gitEmailStep{},
	}
}
