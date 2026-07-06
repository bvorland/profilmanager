package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/bvorland/profilmanager/internal/core"
	"github.com/bvorland/profilmanager/internal/providers"
	"github.com/bvorland/profilmanager/internal/secrets"
	"github.com/bvorland/profilmanager/internal/state"
)

// registerTools adds every v1 MCP tool to the server. Tools are added
// in a deterministic order so [Server.Tools] / `pm doctor` output is
// stable across runs.
func registerTools(s *Server) {
	s.addTool(toolListProfiles(), handleListProfiles)
	s.addTool(toolGetProfile(), handleGetProfile)
	s.addTool(toolGetActiveProfile(), handleGetActiveProfile)
	s.addTool(toolSwitchProfile(), handleSwitchProfile)
	s.addTool(toolWhoami(), handleWhoami)
	s.addTool(toolResolveSecretRef(), handleResolveSecretRef)
	s.addTool(toolExecWithProfile(), handleExecWithProfile)
}

// ---------- shared response shapes ----------

// resolverAvailability is the metadata block attached to list_profiles
// / whoami / get_profile responses so an agent can see at a glance
// whether a backend (op, wincred, dotenv) can serve secret resolutions
// right now.
//
// Design decision: we attach availability
// metadata rather than returning a tool error when `op` is not signed
// in. An agent can then proactively ask the operator to `op signin`
// instead of waiting until exec time to discover the problem.
type resolverAvailability struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

// currentResolverAvailability snapshots every registered resolver's
// Available() state. We never call into Resolve here — only the cheap
// availability probe each resolver caches.
func currentResolverAvailability() map[string]resolverAvailability {
	out := map[string]resolverAvailability{}
	for _, r := range secrets.All() {
		ra := resolverAvailability{Available: r.Available()}
		if !ra.Available {
			ra.Reason = r.Name() + " is not available (not installed, not signed in, or unsupported on this OS)"
		}
		out[r.Name()] = ra
	}
	return out
}

// profileMeta is the metadata-only shape returned by list_profiles /
// get_profile.has_* fields. Same fields as internal/cli/profile.go's
// profileMeta — duplicated rather than imported so internal/mcp does
// not depend on internal/cli.
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

// loadAllProfiles is the equivalent of internal/cli.listAllProfiles —
// duplicated to keep internal/mcp from importing internal/cli (the CLI
// package is allowed to import us, not the reverse). Bad files are
// skipped with their errors collected so the tool result can still
// return the good ones.
func loadAllProfiles() ([]*core.Profile, []string, []string, error) {
	dir, err := core.ProfilesDir()
	if err != nil {
		return nil, nil, nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, nil, nil
		}
		return nil, nil, nil, fmt.Errorf("read profiles dir: %w", err)
	}
	var (
		profiles []*core.Profile
		paths    []string
		loadErrs []string
	)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".toml") {
			continue
		}
		if strings.HasPrefix(name, ".pm-") {
			continue
		}
		path := filepath.Join(dir, name)
		p, err := core.Load(path)
		if err != nil {
			loadErrs = append(loadErrs, err.Error())
			continue
		}
		profiles = append(profiles, p)
		paths = append(paths, path)
	}
	type pair struct {
		p    *core.Profile
		path string
	}
	pairs := make([]pair, len(profiles))
	for i := range profiles {
		pairs[i] = pair{p: profiles[i], path: paths[i]}
	}
	sort.Slice(pairs, func(i, j int) bool {
		return strings.ToLower(pairs[i].p.Name) < strings.ToLower(pairs[j].p.Name)
	})
	for i, pr := range pairs {
		profiles[i] = pr.p
		paths[i] = pr.path
	}
	return profiles, paths, loadErrs, nil
}

// ---------- list_profiles ----------

type listProfilesResult struct {
	Profiles     []profileMeta                   `json:"profiles"`
	LoadErrors   []string                        `json:"load_errors,omitempty"`
	Resolvers    map[string]resolverAvailability `json:"resolvers"`
	ProfilesDir  string                          `json:"profiles_dir"`
	ProfileCount int                             `json:"profile_count"`
}

func toolListProfiles() mcp.Tool {
	return mcp.NewTool("list_profiles",
		mcp.WithDescription(
			"List every profile known to pm. Returns metadata only — no "+
				"env vars, no secret refs, no values. Includes a per-resolver "+
				"availability map (op, wincred, dotenv) so the agent can warn "+
				"the user if a backend is not signed in or not installed.",
		),
	)
}

func handleListProfiles(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	profiles, paths, loadErrs, err := loadAllProfiles()
	if err != nil {
		return mcp.NewToolResultErrorFromErr("list_profiles", err), nil
	}
	dir, _ := core.ProfilesDir()
	out := listProfilesResult{
		Profiles:     make([]profileMeta, 0, len(profiles)),
		LoadErrors:   loadErrs,
		Resolvers:    currentResolverAvailability(),
		ProfilesDir:  dir,
		ProfileCount: len(profiles),
	}
	for i, p := range profiles {
		out.Profiles = append(out.Profiles, metaFor(p, paths[i]))
	}
	return jsonResult(out), nil
}

// ---------- get_profile ----------

type getProfileResult struct {
	Name      string                          `json:"name"`
	Path      string                          `json:"path"`
	Profile   map[string]any                  `json:"profile"`
	Resolvers map[string]resolverAvailability `json:"resolvers"`
}

func toolGetProfile() mcp.Tool {
	return mcp.NewTool("get_profile",
		mcp.WithDescription(
			"Return the full profile body for the named profile. Secret "+
				"refs are returned verbatim (metadata only — they tell you "+
				"WHERE a secret lives, not what it is). Resolved secret "+
				"values are NEVER included; call exec_with_profile to use them.",
		),
		mcp.WithString("name",
			mcp.Required(),
			mcp.Description("Profile name as it appears in `pm profile list`."),
		),
	)
}

func handleGetProfile(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, err := req.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := core.ValidateName(name); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	path, err := core.ProfilePath(name)
	if err != nil {
		return mcp.NewToolResultErrorFromErr("get_profile", err), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return mcp.NewToolResultErrorf("profile %q not found at %s", name, path), nil
		}
		return mcp.NewToolResultErrorFromErr("get_profile", err), nil
	}
	var m map[string]any
	if err := toml.Unmarshal(data, &m); err != nil {
		return mcp.NewToolResultErrorf("parse %s: %v", path, err), nil
	}
	return jsonResult(getProfileResult{
		Name:      name,
		Path:      path,
		Profile:   m,
		Resolvers: currentResolverAvailability(),
	}), nil
}

// ---------- get_active_profile ----------

type getActiveProfileResult struct {
	Active        string `json:"active,omitempty"`
	SessionID     string `json:"session_id"`
	SessionSource string `json:"session_source"`
}

func toolGetActiveProfile() mcp.Tool {
	return mcp.NewTool("get_active_profile",
		mcp.WithDescription(
			"Return the name of the profile currently marked active for "+
				"this MCP session. Returns null/empty when no profile is "+
				"active. Active-profile state is session-scoped metadata — "+
				"it does NOT mutate the calling shell's env.",
		),
	)
}

func handleGetActiveProfile(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, source, err := state.GetActiveProfile()
	if err != nil {
		return mcp.NewToolResultErrorFromErr("get_active_profile", err), nil
	}
	id, _ := state.SessionID()
	return jsonResult(getActiveProfileResult{
		Active:        name,
		SessionID:     id,
		SessionSource: source,
	}), nil
}

// ---------- switch_profile ----------

type switchProfileResult struct {
	Active        string `json:"active"`
	Previous      string `json:"previous,omitempty"`
	SessionID     string `json:"session_id"`
	SessionSource string `json:"session_source"`
	Note          string `json:"note"`
}

func toolSwitchProfile() mcp.Tool {
	return mcp.NewTool("switch_profile",
		mcp.WithDescription(
			"Set the active profile for this MCP session. This writes a "+
				"session-scoped marker that pm exec, exec_with_profile, and "+
				"the optional shell shims read; it does NOT mutate the "+
				"calling shell. Pass an empty string to clear the active "+
				"profile.",
		),
		mcp.WithString("name",
			mcp.Required(),
			mcp.Description("Profile name to make active, or empty to clear."),
		),
	)
}

func handleSwitchProfile(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name := strings.TrimSpace(req.GetString("name", ""))
	previous, _, _ := state.GetActiveProfile()
	id, source := state.SessionID()

	if name == "" {
		if err := state.ClearActiveProfile(); err != nil {
			return mcp.NewToolResultErrorFromErr("clear active profile", err), nil
		}
		return jsonResult(switchProfileResult{
			Active:        "",
			Previous:      previous,
			SessionID:     id,
			SessionSource: source,
			Note:          "active profile cleared for this session",
		}), nil
	}

	if err := core.ValidateName(name); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	// Confirm the profile exists before we record it as active.
	path, err := core.ProfilePath(name)
	if err != nil {
		return mcp.NewToolResultErrorFromErr("switch_profile", err), nil
	}
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return mcp.NewToolResultErrorf("profile %q not found at %s", name, path), nil
		}
		return mcp.NewToolResultErrorFromErr("switch_profile", err), nil
	}
	if err := state.SetActiveProfile(name); err != nil {
		return mcp.NewToolResultErrorFromErr("set active profile", err), nil
	}
	// "last profile" tracks operator's most recent switch across sessions
	// so the CLI's `pm switch -` UX can later return here.
	_ = state.SetLastProfile(name)

	return jsonResult(switchProfileResult{
		Active:        name,
		Previous:      previous,
		SessionID:     id,
		SessionSource: source,
		Note:          "active-profile metadata updated; calling shell env is NOT mutated — use exec_with_profile or `pm exec` to run commands with this profile",
	}), nil
}

// ---------- whoami ----------

type whoamiResult struct {
	Providers []providers.Status              `json:"providers"`
	Drift     []providers.Drift               `json:"drift"`
	Resolvers map[string]resolverAvailability `json:"resolvers"`
}

func toolWhoami() mcp.Tool {
	return mcp.NewTool("whoami",
		mcp.WithDescription(
			"Aggregate every registered provider's logged-in state "+
				"(az, azd, gh, kubectl, git) plus cross-tool drift "+
				"(e.g. az vs azd disagreeing on subscription). Never "+
				"triggers interactive login — a tool that would prompt "+
				"is reported as not-logged-in. Resolver availability "+
				"(op, wincred, dotenv) is included so the agent can spot "+
				"a missing secret backend without a second tool call.",
		),
	)
}

func handleWhoami(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	// Same 30s ceiling as `pm whoami` CLI — keeps a wedged provider
	// from stranding the agent.
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	all := providers.All()
	statuses := make([]providers.Status, 0, len(all))
	for _, p := range all {
		if !p.Available() {
			statuses = append(statuses, providers.Status{
				Provider: p.Name(),
				Error:    p.Name() + " not installed",
			})
			continue
		}
		st, err := p.Whoami(ctx)
		if err != nil {
			st.Provider = p.Name()
			st.Error = err.Error()
		}
		statuses = append(statuses, st)
	}
	return jsonResult(whoamiResult{
		Providers: statuses,
		Drift:     providers.DetectDrift(statuses),
		Resolvers: currentResolverAvailability(),
	}), nil
}

// ---------- resolve_secret_ref ----------

type resolveSecretRefResult struct {
	Ref            string `json:"ref"`
	Resolver       string `json:"resolver"`
	Available      bool   `json:"available"`
	Exists         bool   `json:"exists"`
	LastResolvedAt string `json:"last_resolved_at,omitempty"`
	Metadata       any    `json:"metadata,omitempty"`
	// Note is a verbose reminder that this tool NEVER returns the value.
	// Embedded so an agent reading the tool result alone (without the
	// description) can't miss it.
	Note string `json:"note"`
}

func toolResolveSecretRef() mcp.Tool {
	return mcp.NewTool("resolve_secret_ref",
		mcp.WithDescription(
			"Look up a secret reference's METADATA only — backend, ref, "+
				"existence, last-resolved timestamp (if available). This "+
				"tool NEVER returns the resolved value. To actually use a "+
				"secret, call exec_with_profile with a profile whose env "+
				"includes the ref; pm will materialise the value into the "+
				"child process env only.",
		),
		mcp.WithString("ref",
			mcp.Required(),
			mcp.Description(`Secret reference (e.g. "op://Personal/GitHub Token/credential", "wincred://my-secret", "dotenv:///home/me/.env#TOKEN").`),
		),
	)
}

func handleResolveSecretRef(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ref, err := req.RequireString("ref")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	md, descErr := secrets.DescribeRef(ctx, ref)

	// Audit every resolve_secret_ref call. Per the iron rule we log the
	// REF (metadata), never the value — DescribeRef returns no value to
	// begin with, but we record the access attempt either way.
	auditResult := "ok"
	auditErr := ""
	if descErr != nil {
		auditResult = "error"
		auditErr = descErr.Error()
		if errors.Is(descErr, secrets.ErrUnavailable) {
			auditResult = "miss"
		}
	} else if !md.Exists {
		auditResult = "miss"
	}
	logEntry(AuditEntry{
		Tool:   "resolve_secret_ref",
		Ref:    ref,
		Result: auditResult,
		Error:  auditErr,
	})

	if descErr != nil && !errors.Is(descErr, secrets.ErrUnavailable) && !errors.Is(descErr, secrets.ErrNoResolver) {
		// Hard error (parse failure, unexpected backend error). Do NOT
		// reflect the backend error text to the MCP peer: a resolver error
		// can embed bytes read from an attacker-influenced path (e.g. a
		// dotenv:// ref pointed at ~/.git-credentials, whose first line has
		// no '=' and would otherwise be echoed back). The operator can read
		// the detailed reason from the local, 0600 audit log.
		return mcp.NewToolResultErrorf("resolve_secret_ref %q: reference could not be described (backend error; see local audit log)", ref), nil
	}

	// Compose response. Whether descErr is set or not, md is populated
	// with at least Ref + Error fields by DescribeRef on the failure
	// path.
	// Defense in depth: never reflect a backend-populated free-text error
	// over MCP. Structured Available/Exists already convey state, and an
	// error string could carry bytes from an attacker-influenced path.
	md.Error = ""

	resolver, available := lookupResolverForRef(ref)
	out := resolveSecretRefResult{
		Ref:       ref,
		Resolver:  resolver,
		Available: available,
		Exists:    md.Exists,
		Metadata:  md,
		Note:      "resolved value is NEVER returned over MCP — use exec_with_profile to consume secrets",
	}
	return jsonResult(out), nil
}

// lookupResolverForRef returns the resolver name that would claim ref,
// and whether that resolver is Available() right now. Falls back to
// ("", false) if no resolver matches.
//
// Implemented here (rather than in internal/secrets) because we want a
// metadata-only probe — calling secrets.ResolveRef would attempt the
// actual lookup, which is the wrong shape for this tool.
func lookupResolverForRef(ref string) (string, bool) {
	for _, r := range secrets.All() {
		s := r.Scheme()
		if s != "" && strings.HasPrefix(strings.ToLower(ref), strings.ToLower(s)) {
			return r.Name(), r.Available()
		}
	}
	// Fall back to "<name>://" match (covers dotenv://).
	if i := strings.Index(ref, "://"); i > 0 {
		bare := strings.ToLower(ref[:i])
		for _, r := range secrets.All() {
			if strings.EqualFold(r.Name(), bare) {
				return r.Name(), r.Available()
			}
		}
	}
	// Literal (no scheme) — first resolver with empty scheme wins.
	if !strings.Contains(ref, "://") {
		for _, r := range secrets.All() {
			if r.Scheme() == "" {
				return r.Name(), r.Available()
			}
		}
	}
	return "", false
}

// ---------- exec_with_profile ----------

func toolExecWithProfile() mcp.Tool {
	return mcp.NewTool("exec_with_profile",
		mcp.WithDescription(
			"Run a child process with the named profile's env vars "+
				"(literals + resolved secret refs) applied. Heavily guarded:\n"+
				"  - command must be in the allowlist (default: az azd gh kubectl git)\n"+
				"  - no shell — explicit argv only, never interpreted\n"+
				"  - timeout default 60s, hard cap 300s\n"+
				"  - every resolved secret value is redacted from stdout/stderr\n"+
				"  - every invocation is appended to the mcp.log audit file\n"+
				"Resolved secret values NEVER appear in the response.",
		),
		mcp.WithString("command",
			mcp.Required(),
			mcp.Description("Executable name (basename). Must be in the allowlist."),
		),
		mcp.WithArray("args",
			mcp.Description("Arguments passed verbatim to the executable."),
			mcp.Items(map[string]any{"type": "string"}),
		),
		mcp.WithString("profile",
			mcp.Description("Profile name. Empty falls back to the session's active profile."),
		),
		mcp.WithNumber("timeout_seconds",
			mcp.Description("Override the default 60s timeout. Clamped to 300s."),
		),
		mcp.WithString("stdin",
			mcp.Description("Optional UTF-8 input piped to the child's stdin."),
		),
	)
}

func handleExecWithProfile(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	command, err := req.RequireString("command")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	args := req.GetStringSlice("args", nil)
	profile := req.GetString("profile", "")
	stdinStr := req.GetString("stdin", "")
	timeoutAny := req.GetArguments()["timeout_seconds"]
	timeoutSecs := 0
	switch v := timeoutAny.(type) {
	case float64:
		timeoutSecs = int(v)
	case int:
		timeoutSecs = v
	case int64:
		timeoutSecs = int(v)
	case json.Number:
		if i, err := v.Int64(); err == nil {
			timeoutSecs = int(i)
		}
	}

	res, runErr := Exec(ctx, ExecRequest{
		Profile:        profile,
		Command:        command,
		Args:           args,
		TimeoutSeconds: timeoutSecs,
		Stdin:          []byte(stdinStr),
	})
	if runErr != nil {
		// ErrCommandNotAllowed and ErrNoProfile are user-fixable; surface
		// them as tool errors (the SDK marks IsError=true) but stay on the
		// happy path so the agent gets a parseable response.
		return mcp.NewToolResultErrorFromErr("exec_with_profile", runErr), nil
	}
	return jsonResult(res), nil
}

// ensure the SDK signature matches our handler type at compile time.
var (
	_ mcpserver.ToolHandlerFunc = handleListProfiles
	_ mcpserver.ToolHandlerFunc = handleGetProfile
	_ mcpserver.ToolHandlerFunc = handleGetActiveProfile
	_ mcpserver.ToolHandlerFunc = handleSwitchProfile
	_ mcpserver.ToolHandlerFunc = handleWhoami
	_ mcpserver.ToolHandlerFunc = handleResolveSecretRef
	_ mcpserver.ToolHandlerFunc = handleExecWithProfile
)
