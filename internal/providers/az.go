package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bvorland/profilmanager/internal/core"
)

// azProvider integrates Azure CLI 2.x.
//
// WAM mitigation (see docs/isolation-matrix.md "Known unknowns" #1):
// Azure CLI ≥ 2.61 on Windows enables the Web Account Manager broker by
// default, which stashes tokens in %LOCALAPPDATA%\.IdentityService —
// independent of AZURE_CONFIG_DIR. To make per-profile isolation
// actually mean what operators think it means, Apply writes a baseline
// config file into the profile's AZURE_CONFIG_DIR with
// `core.enable_broker_on_windows=false` and `core.output=json`. The
// write is idempotent (we update in place, never clobber other keys).
type azProvider struct{}

// AzProvider returns the singleton az adapter. Useful for tests; the
// init() in registry.go wires it for production.
func AzProvider() Provider { return azProvider{} }

func (azProvider) Name() string { return "az" }

func (azProvider) Available() bool {
	_, err := lookPath("az")
	return err == nil
}

func (azProvider) Apply(p *core.Profile) (map[string]string, error) {
	env := map[string]string{
		// Predictable output everywhere. We override per-call with -o
		// json on our own invocations, but operator `az` calls inside
		// `pm exec` should also default to json.
		"AZURE_CORE_OUTPUT": "json",
	}
	if p == nil || p.Azure == nil || p.Azure.ConfigDir == "" {
		// Profile doesn't pin a config dir: still set the output var,
		// but don't synthesize a config dir the operator didn't ask
		// for.
		return env, nil
	}
	dir := expandHome(p.Azure.ConfigDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, errf("az", "create AZURE_CONFIG_DIR", err)
	}
	if err := ensureAzConfigDefaults(dir); err != nil {
		return nil, errf("az", "write baseline config", err)
	}
	env["AZURE_CONFIG_DIR"] = dir
	return env, nil
}

func (azProvider) Whoami(ctx context.Context) (Status, error) {
	st := Status{Provider: "az"}
	if _, err := lookPath("az"); err != nil {
		st.Error = "az CLI not installed"
		return st, nil
	}
	ctx, cancel := withDefaultTimeout(ctx)
	defer cancel()
	stdout, stderr, err := runCmd(ctx, "az", "account", "show", "-o", "json")
	if err != nil {
		// Non-zero exit means "not logged in" in the typical case. We
		// don't try to distinguish — `az` prints "Please run 'az
		// login'" to stderr. Surfacing that verbatim is informative
		// and harmless.
		if isExitErr(err) {
			st.Error = "not logged in"
			if hint := truncStderr(stderr); hint != "" {
				st.Error = hint
			}
			return st, nil
		}
		return st, errf("az", "account show", err)
	}
	var acct azAccount
	if err := json.Unmarshal(stdout, &acct); err != nil {
		return st, errf("az", "parse account show", err)
	}
	st.LoggedIn = true
	if acct.User != nil {
		st.Account = acct.User.Name
	}
	st.Tenant = acct.TenantID
	st.Subscription = acct.ID
	if acct.Name != "" {
		st.Extra = map[string]string{"subscription_name": acct.Name}
	}
	return st, nil
}

// azAccount is the subset of `az account show -o json` we use.
type azAccount struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	TenantID string  `json:"tenantId"`
	User     *azUser `json:"user"`
}

type azUser struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// expandHome resolves a leading "~/" or "~" prefix to the operator's
// home dir. Profiles may store config_dir as "~/.azure-Foo"; on disk we
// need an absolute path.
func expandHome(p string) string {
	if p == "" {
		return p
	}
	if p == "~" {
		if h, err := os.UserHomeDir(); err == nil {
			return h
		}
		return p
	}
	if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, p[2:])
		}
	}
	return p
}

// ensureAzConfigDefaults writes/updates the `config` file inside dir to
// enforce the WAM-off and json-output defaults. The file format is a
// classic INI; we parse it line-by-line, set the two keys under [core],
// and write the result back atomically.
//
// We do NOT call `az config set` here because that would require `az` to
// be on PATH at Apply time (we want Apply to work even if `az` isn't
// installed yet — operator may install later) and because shelling out
// from Apply violates the "Apply is pure: env vars + dir layout" rule.
func ensureAzConfigDefaults(dir string) error {
	path := filepath.Join(dir, "config")
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	updated := upsertINI(string(data), "core", map[string]string{
		"enable_broker_on_windows": "false",
		"output":                   "json",
	})
	if updated == string(data) {
		return nil
	}
	tmp, err := os.CreateTemp(dir, ".pm-az-config-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.WriteString(updated); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return err
	}
	return nil
}

// upsertINI ensures section contains every key→value in kv, preserving
// any other content. Sections are recognised by [name] headers; keys
// inside the target section that match by name are updated in place.
// Missing keys are appended at the end of the section. If the section
// itself is missing, it's appended to the file.
func upsertINI(src, section string, kv map[string]string) string {
	lines := strings.Split(src, "\n")
	var (
		out          []string
		inSection    bool
		sectionStart = -1
		sectionEnd   = -1
		header       = "[" + section + "]"
		setKeys      = map[string]bool{}
	)
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		switch {
		case trim == header:
			inSection = true
			sectionStart = len(out)
			out = append(out, line)
			continue
		case strings.HasPrefix(trim, "[") && strings.HasSuffix(trim, "]"):
			if inSection && sectionEnd < 0 {
				sectionEnd = len(out)
			}
			inSection = false
			out = append(out, line)
			continue
		}
		if inSection && trim != "" && !strings.HasPrefix(trim, "#") && !strings.HasPrefix(trim, ";") {
			if eq := strings.Index(trim, "="); eq > 0 {
				key := strings.TrimSpace(trim[:eq])
				if v, ok := kv[key]; ok {
					out = append(out, fmt.Sprintf("%s = %s", key, v))
					setKeys[key] = true
					continue
				}
			}
		}
		out = append(out, line)
		if inSection {
			sectionEnd = len(out)
		}
		_ = i
	}
	missing := make([]string, 0, len(kv))
	for k := range kv {
		if !setKeys[k] {
			missing = append(missing, k)
		}
	}
	if len(missing) == 0 {
		return strings.Join(out, "\n")
	}
	// Deterministic order so tests don't flake.
	keysSorted := sortStrings(missing)
	var insert []string
	for _, k := range keysSorted {
		insert = append(insert, fmt.Sprintf("%s = %s", k, kv[k]))
	}
	if sectionStart < 0 {
		// Append section at end.
		if len(out) > 0 && strings.TrimSpace(out[len(out)-1]) != "" {
			out = append(out, "")
		}
		out = append(out, header)
		out = append(out, insert...)
		return strings.Join(out, "\n")
	}
	if sectionEnd < 0 {
		sectionEnd = len(out)
	}
	// Insert missing keys at end of section.
	merged := make([]string, 0, len(out)+len(insert))
	merged = append(merged, out[:sectionEnd]...)
	merged = append(merged, insert...)
	merged = append(merged, out[sectionEnd:]...)
	return strings.Join(merged, "\n")
}

func sortStrings(in []string) []string {
	out := make([]string, len(in))
	copy(out, in)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}
