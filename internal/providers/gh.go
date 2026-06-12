package providers

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/bvorland/profilmanager/internal/core"
)

// ghProvider integrates the GitHub CLI.
//
// gh isolation knob: GH_CONFIG_DIR (gh ≥ 2.x). The older `GITHUB_TOKEN`
// env override only swaps the auth token; it doesn't isolate
// configuration. GH_HOME does NOT exist (operators sometimes guess
// it). We chose GH_CONFIG_DIR because it's the only documented switch
// that redirects both hosts.yml (auth tokens) and config.yml
// (per-profile settings).
//
// JSON inspection: `gh auth status --json hosts` (gh ≥ 2.40-ish) is
// the only documented JSON entrypoint for auth state. Each host maps
// to an array of accounts; recent gh supports multiple accounts per
// host with one marked `active: true`.
type ghProvider struct{}

func GhProvider() Provider { return ghProvider{} }

func (ghProvider) Name() string { return "gh" }

func (ghProvider) Available() bool {
	_, err := lookPath("gh")
	return err == nil
}

func (ghProvider) Apply(p *core.Profile) (map[string]string, error) {
	env := map[string]string{}
	if p == nil {
		return env, nil
	}
	root, err := core.StateDir()
	if err != nil {
		return nil, errf("gh", "resolve state dir", err)
	}
	dir := filepath.Join(root, "gh", p.Name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, errf("gh", "create GH_CONFIG_DIR", err)
	}
	env["GH_CONFIG_DIR"] = dir
	return env, nil
}

func (ghProvider) Whoami(ctx context.Context) (Status, error) {
	st := Status{Provider: "gh"}
	if _, err := lookPath("gh"); err != nil {
		st.Error = "gh CLI not installed"
		return st, nil
	}
	ctx, cancel := withDefaultTimeout(ctx)
	defer cancel()
	stdout, stderr, err := runCmd(ctx, "gh", "auth", "status", "--json", "hosts")
	if err != nil {
		if isExitErr(err) {
			st.Error = "not logged in"
			if hint := truncStderr(stderr); hint != "" {
				st.Error = hint
			}
			return st, nil
		}
		return st, errf("gh", "auth status", err)
	}
	// Try the newer wrapped+arrays shape first; fall back to the
	// older bare-object shape used by some test scripts and very
	// old gh versions.
	if st, ok := parseGhV2(stdout); ok {
		return st, nil
	}
	if st, ok := parseGhV1(stdout); ok {
		return st, nil
	}
	st.Error = "could not parse gh auth status JSON"
	return st, nil
}

// parseGhV2 handles the gh ≥ 2.40 shape:
//
//	{"hosts": {"github.com": [{"login":"u","active":true,...}, ...]}}
func parseGhV2(stdout []byte) (Status, bool) {
	var wrapped struct {
		Hosts map[string][]ghHostV2 `json:"hosts"`
	}
	if err := json.Unmarshal(stdout, &wrapped); err != nil || len(wrapped.Hosts) == 0 {
		return Status{}, false
	}
	primary := pickPrimaryHost(keysOfHostMapV2(wrapped.Hosts))
	accounts := wrapped.Hosts[primary]
	st := Status{Provider: "gh", LoggedIn: true, Extra: map[string]string{}}
	st.Subscription = primary
	st.Extra["host"] = primary
	// Pick the active account; fall back to first.
	pick := -1
	for i, a := range accounts {
		if a.Active {
			pick = i
			break
		}
	}
	if pick < 0 && len(accounts) > 0 {
		pick = 0
	}
	if pick >= 0 {
		a := accounts[pick]
		st.Account = a.Login
		if a.Scopes != "" {
			st.Extra["scopes"] = a.Scopes
		}
		if a.GitProtocol != "" {
			st.Extra["git_protocol"] = a.GitProtocol
		}
		if a.TokenSource != "" {
			st.Extra["token_source"] = a.TokenSource
		}
	}
	if len(accounts) > 1 {
		// Surface the count so operators see "huh, I have 3 logins"
		// without us echoing all 3.
		st.Extra["accounts_on_host"] = intToString(len(accounts))
	}
	if len(wrapped.Hosts) > 1 {
		others := make([]string, 0, len(wrapped.Hosts)-1)
		for k := range wrapped.Hosts {
			if k != primary {
				others = append(others, k)
			}
		}
		if len(others) > 0 {
			st.Extra["other_hosts"] = strings.Join(sortStrings(others), ",")
		}
	}
	return st, true
}

// parseGhV1 handles a legacy / test-script bare-map shape:
//
//	{"github.com": {"user":"u", ...}}
//
// or
//
//	{"hosts": {"github.com": {"user":"u", ...}}}.
//
// We never see this in production today, but keeping it lets the
// existing fixtures and any older gh versions work.
func parseGhV1(stdout []byte) (Status, bool) {
	var bare map[string]ghHostV1
	if err := json.Unmarshal(stdout, &bare); err == nil && len(bare) > 0 {
		if _, ok := bare["hosts"]; !ok {
			st := buildGhV1Status(bare)
			return st, true
		}
	}
	var wrapped struct {
		Hosts map[string]ghHostV1 `json:"hosts"`
	}
	if err := json.Unmarshal(stdout, &wrapped); err == nil && len(wrapped.Hosts) > 0 {
		return buildGhV1Status(wrapped.Hosts), true
	}
	return Status{}, false
}

func buildGhV1Status(hosts map[string]ghHostV1) Status {
	st := Status{Provider: "gh", LoggedIn: true, Extra: map[string]string{}}
	primary := pickPrimaryHost(keysOfHostMapV1(hosts))
	h := hosts[primary]
	st.Account = h.User
	st.Subscription = primary
	st.Extra["host"] = primary
	if len(h.Scopes) > 0 {
		st.Extra["scopes"] = strings.Join(h.Scopes, ",")
	}
	if h.GitProto != "" {
		st.Extra["git_protocol"] = h.GitProto
	}
	if len(hosts) > 1 {
		others := make([]string, 0, len(hosts)-1)
		for k := range hosts {
			if k != primary {
				others = append(others, k)
			}
		}
		if len(others) > 0 {
			st.Extra["other_hosts"] = strings.Join(sortStrings(others), ",")
		}
	}
	return st
}

// pickPrimaryHost prefers github.com when present, else the first
// hostname in the sorted set so output is deterministic.
func pickPrimaryHost(hosts []string) string {
	for _, h := range hosts {
		if h == "github.com" {
			return h
		}
	}
	sorted := sortStrings(hosts)
	if len(sorted) > 0 {
		return sorted[0]
	}
	return ""
}

func keysOfHostMapV1(m map[string]ghHostV1) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
func keysOfHostMapV2(m map[string][]ghHostV2) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// ghHostV2 mirrors one entry in `gh auth status --json hosts` (gh ≥
// 2.40). Note the camelCase field names — they differ from V1.
type ghHostV2 struct {
	State       string `json:"state"`
	Active      bool   `json:"active"`
	Host        string `json:"host"`
	Login       string `json:"login"`
	TokenSource string `json:"tokenSource"`
	Scopes      string `json:"scopes"`
	GitProtocol string `json:"gitProtocol"`
}

// ghHostV1 mirrors a much older / test-script shape with snake_case
// fields and a single account per host.
type ghHostV1 struct {
	User     string   `json:"user"`
	Active   bool     `json:"active"`
	GitProto string   `json:"git_protocol"`
	Scopes   []string `json:"scopes"`
	TokenSrc string   `json:"token_source"`
}

func intToString(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if neg {
		digits = append([]byte{'-'}, digits...)
	}
	return string(digits)
}
