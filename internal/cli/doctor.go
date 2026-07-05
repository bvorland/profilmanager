package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/bvorland/profilmanager/internal/core"
	"github.com/bvorland/profilmanager/internal/state"
)

// CheckStatus enumerates the three outcomes a diagnostic can produce.
// Strings are stable JSON values — bump the schema version if you add
// or rename one.
type CheckStatus string

const (
	StatusOK   CheckStatus = "ok"
	StatusWarn CheckStatus = "warn"
	StatusFail CheckStatus = "fail"
)

// CheckResult is the JSON shape `pm doctor` emits for each check. Name
// uses kebab-case (`profiles-dir-exists`) so it greps cleanly in CI logs
// and reads as a sentence in human output.
type CheckResult struct {
	Name    string      `json:"name"`
	Status  CheckStatus `json:"status"`
	Message string      `json:"message"`
	Fix     string      `json:"fix,omitempty"`
}

// CheckFn returns one CheckResult. Receives no context: every built-in
// check is cheap and side-effect-free except state-dir-writable, which
// touches a temp file under StateDir. Provider checks registered by
// External checks should also stay cheap (sub-second total). If a check would
// block on network I/O, gate it behind a flag.
type CheckFn func() CheckResult

// registeredCheck pairs a stable name with its function. The name is
// used to override built-ins (a registered check with the same name
// replaces the built-in) and to provide stable ordering in output.
type registeredCheck struct {
	name string
	fn   CheckFn
}

var (
	// externalChecks is populated by RegisterCheck. Provider
	// init() functions will append here at import time.
	externalChecks []registeredCheck
)

// RegisterCheck adds (or replaces, by name) a diagnostic available to
// `pm doctor`. Intended for use from a provider package's init() —
// e.g. internal/providers/azure can call:
//
//	func init() { cli.RegisterCheck("az-cli-installed", azCheck) }
//
// The check is invoked on every `pm doctor` run; keep it cheap.
//
// Calling RegisterCheck after Execute has started is technically safe
// but the new check will not appear in any already-running invocation.
func RegisterCheck(name string, fn CheckFn) {
	if name == "" || fn == nil {
		return
	}
	// Replace if name already registered.
	for i := range externalChecks {
		if externalChecks[i].name == name {
			externalChecks[i].fn = fn
			return
		}
	}
	externalChecks = append(externalChecks, registeredCheck{name: name, fn: fn})
}

// ---------- pm doctor ----------

func newDoctorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Run diagnostic checks and report status",
		Long: `Run a fixed set of cheap, side-effect-free diagnostics and report status.

Each check is one of: ok | warn | fail. Built-in checks cover:

  profiles-dir-exists       — the per-OS profiles directory is readable
  state-dir-writable        — the per-OS state directory is writable
  session-id-source         — where the current PM_SESSION_ID came from
  agent-context-has-profile — AI agent sessions have an active pm profile
  shell-init-wrapper-loaded — PowerShell shell-init wrapper is loaded
  profiles-not-in-git       — profiles dir is not inside a git worktree
  mcp-registered            — CWD's .copilot/mcp-config.json has a pm entry
  tool-available:<name>     — <name> is on PATH (az, azd, gh, kubectl, git, pwsh)

Providers add their own checks at import time via cli.RegisterCheck.

Default output is a colored table. With --json the result is a stable
array of {name,status,message,fix?} objects.`,
		Args: cobra.NoArgs,
		RunE: runDoctor,
	}
	addJSONFlag(cmd)
	return cmd
}

func runDoctor(cmd *cobra.Command, _ []string) error {
	results := runAllChecks()

	if jsonRequested(cmd) {
		out := struct {
			Checks []CheckResult `json:"checks"`
		}{Checks: results}
		return writeJSON(cmd.OutOrStdout(), out)
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	for _, r := range results {
		var tag string
		switch r.Status {
		case StatusOK:
			tag = styleOK.Render("[ OK ]")
		case StatusWarn:
			tag = styleWarn.Render("[WARN]")
		case StatusFail:
			tag = styleError.Render("[FAIL]")
		default:
			tag = "[????]"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", tag, r.Name, r.Message)
		if r.Fix != "" {
			fmt.Fprintf(w, "\t\t%s\n", styleDim.Render("fix: "+r.Fix))
		}
	}
	_ = w.Flush()

	// Aggregate: doctor exits non-zero only when there is a hard fail.
	for _, r := range results {
		if r.Status == StatusFail {
			return WithExitCode(ExitError, errors.New("one or more checks failed"))
		}
	}
	return nil
}

// runAllChecks runs every built-in plus every externally-registered
// check in a stable order: built-ins first (in their declared order),
// then externals (in registration order), then tool-availability checks
// last (they're informational).
func runAllChecks() []CheckResult {
	results := []CheckResult{
		checkProfilesDir(),
		checkStateDirWritable(),
		checkSessionIDSource(),
		checkAgentContextHasProfile(),
		checkShellInitWrapperLoaded(),
		checkProfilesNotInGit(),
		checkMCPRegistered(),
	}
	// External checks (providers etc.) in registration order.
	for _, rc := range externalChecks {
		results = append(results, rc.fn())
	}
	// Tool availability — sorted for stable output.
	tools := []string{"az", "azd", "gh", "kubectl", "git", "pwsh"}
	sort.Strings(tools)
	for _, t := range tools {
		results = append(results, checkToolAvailable(t))
	}
	return results
}

// ---------- built-in checks ----------

func checkProfilesDir() CheckResult {
	dir, err := core.ProfilesDir()
	if err != nil {
		return CheckResult{
			Name: "profiles-dir-exists", Status: StatusFail,
			Message: fmt.Sprintf("cannot resolve profiles dir: %v", err),
			Fix:     "set APPDATA / XDG_CONFIG_HOME and rerun",
		}
	}
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		return CheckResult{
			Name: "profiles-dir-exists", Status: StatusFail,
			Message: fmt.Sprintf("profiles dir missing: %s", dir),
		}
	}
	return CheckResult{
		Name: "profiles-dir-exists", Status: StatusOK,
		Message: dir,
	}
}

func checkStateDirWritable() CheckResult {
	dir, err := core.StateDir()
	if err != nil {
		return CheckResult{
			Name: "state-dir-writable", Status: StatusFail,
			Message: fmt.Sprintf("cannot resolve state dir: %v", err),
		}
	}
	// Probe with an atomic-ish write/delete.
	f, err := os.CreateTemp(dir, ".pm-doctor-*.tmp")
	if err != nil {
		return CheckResult{
			Name: "state-dir-writable", Status: StatusFail,
			Message: fmt.Sprintf("cannot write to %s: %v", dir, err),
			Fix:     "check permissions or free disk space",
		}
	}
	name := f.Name()
	_, werr := f.Write([]byte("pm-doctor"))
	_ = f.Close()
	_ = os.Remove(name)
	if werr != nil {
		return CheckResult{
			Name: "state-dir-writable", Status: StatusFail,
			Message: fmt.Sprintf("write to %s failed: %v", dir, werr),
		}
	}
	return CheckResult{
		Name: "state-dir-writable", Status: StatusOK,
		Message: dir,
	}
}

func checkSessionIDSource() CheckResult {
	id, src := state.SessionID()
	switch src {
	case state.SourcePPIDFallback:
		return CheckResult{
			Name: "session-id-source", Status: StatusWarn,
			Message: fmt.Sprintf("session id %q derived from PPID — fragile under sudo / tmux / process recycling", id),
			Fix:     "set PM_SESSION_ID via `pm session init` to silence",
		}
	case state.SourcePMSession:
		return CheckResult{
			Name: "session-id-source", Status: StatusOK,
			Message: fmt.Sprintf("PM_SESSION_ID is set (id=%s)", id),
		}
	default:
		return CheckResult{
			Name: "session-id-source", Status: StatusOK,
			Message: fmt.Sprintf("derived from %s (id=%s)", src, id),
		}
	}
}

func checkAgentContextHasProfile() CheckResult {
	if ok, v := core.InAgentContext(); ok {
		activeProfile, source, err := resolveActiveProfile()
		if err != nil {
			return CheckResult{
				Name:    "agent-context-has-profile",
				Status:  StatusFail,
				Message: fmt.Sprintf("could not resolve active profile: %v", err),
			}
		}
		if activeProfile == "" {
			return CheckResult{
				Name:    "agent-context-has-profile",
				Status:  StatusWarn,
				Message: fmt.Sprintf("Inside an AI agent (%s set) without an active pm profile — your tools will use host config. Run: pm env apply <name> | Invoke-Expression", v),
			}
		}
		detail := "shell"
		if source == activeProfileSourceSession {
			detail = "session"
		}
		return CheckResult{
			Name:    "agent-context-has-profile",
			Status:  StatusOK,
			Message: fmt.Sprintf("active profile %q (agent: %s, source: %s)", activeProfile, v, detail),
		}
	}
	return CheckResult{
		Name:    "agent-context-has-profile",
		Status:  StatusOK,
		Message: "not in agent context (skipped)",
	}
}

func checkShellInitWrapperLoaded() CheckResult {
	if runtime.GOOS != "windows" {
		return CheckResult{
			Name:    "shell-init-wrapper-loaded",
			Status:  StatusOK,
			Message: "not on Windows (skipped; pwsh wrapper check only applies on Windows today)",
		}
	}
	if detectShell() != "pwsh" {
		return CheckResult{
			Name:    "shell-init-wrapper-loaded",
			Status:  StatusOK,
			Message: "not in pwsh (skipped; wrapper check only applies to PowerShell today)",
		}
	}
	if os.Getenv("PM_SHELL_INIT_LOADED") == "" {
		return CheckResult{
			Name:    "shell-init-wrapper-loaded",
			Status:  StatusWarn,
			Message: "pm shell-init pwsh wrapper is not loaded; auto-apply for `pm profile new` and future tool shims will not work. Add to your $PROFILE: pm shell-init pwsh | Out-String | Invoke-Expression",
			Fix:     "Add to your $PROFILE: pm shell-init pwsh | Out-String | Invoke-Expression",
		}
	}
	return CheckResult{
		Name:    "shell-init-wrapper-loaded",
		Status:  StatusOK,
		Message: "pm shell-init pwsh wrapper is loaded",
	}
}

// checkProfilesNotInGit walks parents of the profiles dir looking for a
// .git entry. If found, warns: profile TOMLs contain sensitive metadata
// (tenant/subscription IDs, secret refs) and should not be inside a git
// worktree even if .gitignored. Defense in depth.
func checkProfilesNotInGit() CheckResult {
	dir, err := core.ProfilesDir()
	if err != nil {
		return CheckResult{
			Name: "profiles-not-in-git", Status: StatusFail,
			Message: fmt.Sprintf("cannot resolve profiles dir: %v", err),
		}
	}
	cur := dir
	for {
		if _, err := os.Stat(filepath.Join(cur, ".git")); err == nil {
			return CheckResult{
				Name: "profiles-not-in-git", Status: StatusWarn,
				Message: fmt.Sprintf("profiles dir %s is inside git worktree at %s", dir, cur),
				Fix:     "move profiles out of the repo (default location: per-OS user config dir)",
			}
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	return CheckResult{
		Name: "profiles-not-in-git", Status: StatusOK,
		Message: "profiles dir is outside any git worktree",
	}
}

// checkMCPRegistered looks for `.copilot/mcp-config.json` in CWD and
// checks whether `mcpServers.profilmanager` (or `mcpServers.pm`) is
// present. Absent is a warning, not a failure — pm works without MCP
// registration; the agent integration just won't be auto-wired.
//
// Also probe the global ~/.copilot/mcp-config.json.
func checkMCPRegistered() CheckResult {
	const path = ".copilot/mcp-config.json"
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return CheckResult{
				Name: "mcp-registered", Status: StatusWarn,
				Message: "no .copilot/mcp-config.json in CWD",
				Fix:     "(if you want agent integration) add a 'profilmanager' entry pointing at `pm mcp`",
			}
		}
		return CheckResult{
			Name: "mcp-registered", Status: StatusWarn,
			Message: fmt.Sprintf("cannot read %s: %v", path, err),
		}
	}
	var cfg struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return CheckResult{
			Name: "mcp-registered", Status: StatusWarn,
			Message: fmt.Sprintf("cannot parse %s: %v", path, err),
		}
	}
	for _, key := range []string{"profilmanager", "pm"} {
		if _, ok := cfg.MCPServers[key]; ok {
			return CheckResult{
				Name: "mcp-registered", Status: StatusOK,
				Message: fmt.Sprintf("entry %q found in %s", key, path),
			}
		}
	}
	return CheckResult{
		Name: "mcp-registered", Status: StatusWarn,
		Message: fmt.Sprintf("no 'profilmanager' or 'pm' entry in %s", path),
		Fix:     "add an entry pointing at `pm mcp` once the MCP server lands",
	}
}

func checkToolAvailable(tool string) CheckResult {
	name := "tool-available:" + tool
	path, err := exec.LookPath(tool)
	if err != nil {
		return CheckResult{
			Name: name, Status: StatusWarn,
			Message: fmt.Sprintf("%s not on PATH", tool),
			Fix:     fmt.Sprintf("install %s if you plan to use the matching provider", tool),
		}
	}
	return CheckResult{
		Name: name, Status: StatusOK,
		Message: path,
	}
}

// silence unused-import warnings if strings becomes unused after edits.
var _ = strings.TrimSpace
