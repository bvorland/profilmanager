package providers

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/bvorland/profilmanager/internal/core"
)

// kubeProvider integrates kubectl.
//
// Isolation knob: KUBECONFIG. kubectl reads KUBECONFIG as a search path
// (colon/semicolon separated). We give each profile its own kubeconfig
// file path so concurrent profiles don't fight over a shared file.
// KUBE_CONTEXT and KUBE_NAMESPACE are informational env vars — kubectl
// itself reads them from the kubeconfig, not from env — but other tools
// (helm, k9s) and shell prompts may consume them.
type kubeProvider struct{}

func KubeProvider() Provider { return kubeProvider{} }

func (kubeProvider) Name() string { return "kubectl" }

func (kubeProvider) Available() bool {
	_, err := lookPath("kubectl")
	return err == nil
}

func (kubeProvider) Apply(p *core.Profile) (map[string]string, error) {
	env := map[string]string{}
	if p == nil {
		return env, nil
	}
	root, err := core.StateDir()
	if err != nil {
		return nil, errf("kubectl", "resolve state dir", err)
	}
	dir := filepath.Join(root, "kube", p.Name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, errf("kubectl", "create kube state dir", err)
	}
	env["KUBECONFIG"] = filepath.Join(dir, "config")
	if p.Kube == nil {
		return env, nil
	}
	if p.Kube.Context != "" {
		env["KUBE_CONTEXT"] = p.Kube.Context
	}
	if p.Kube.Namespace != "" {
		env["KUBE_NAMESPACE"] = p.Kube.Namespace
	}
	return env, nil
}

func (kubeProvider) Whoami(ctx context.Context) (Status, error) {
	st := Status{Provider: "kubectl"}
	if _, err := lookPath("kubectl"); err != nil {
		st.Error = "kubectl not installed"
		return st, nil
	}
	ctx, cancel := withDefaultTimeout(ctx)
	defer cancel()
	stdout, stderr, err := runCmd(ctx, "kubectl", "config", "current-context")
	if err != nil {
		if isExitErr(err) {
			st.Error = "no current context"
			if hint := truncStderr(stderr); hint != "" {
				st.Error = hint
			}
			return st, nil
		}
		return st, errf("kubectl", "config current-context", err)
	}
	context := strings.TrimSpace(string(stdout))
	st.LoggedIn = true // kubectl is "logged in" if it has a context
	st.Subscription = context
	st.Extra = map[string]string{"context": context}

	// Now get cluster/namespace via minified view.
	stdout, stderr, err = runCmd(ctx, "kubectl", "config", "view", "--minify", "-o", "json")
	if err != nil {
		// Best-effort: keep context but flag the secondary failure.
		if hint := truncStderr(stderr); hint != "" {
			st.Extra["view_error"] = hint
		}
		return st, nil
	}
	var view kubeConfigView
	if err := json.Unmarshal(stdout, &view); err != nil {
		st.Extra["view_error"] = "could not parse minified kubeconfig"
		return st, nil
	}
	if len(view.Contexts) > 0 {
		c := view.Contexts[0].Context
		if c.User != "" {
			st.Account = c.User
		}
		if c.Namespace != "" {
			st.Extra["namespace"] = c.Namespace
		}
		if c.Cluster != "" {
			st.Extra["cluster"] = c.Cluster
		}
	}
	return st, nil
}

// kubeConfigView mirrors `kubectl config view --minify -o json`. We only
// peel out the few fields we display.
type kubeConfigView struct {
	Contexts []struct {
		Name    string `json:"name"`
		Context struct {
			Cluster   string `json:"cluster"`
			User      string `json:"user"`
			Namespace string `json:"namespace"`
		} `json:"context"`
	} `json:"contexts"`
}
