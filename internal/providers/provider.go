package providers

import (
	"context"
	"os/exec"
	"sort"
	"sync"

	"github.com/bvorland/profilmanager/internal/core"
)

// Provider is the contract every per-tool integration implements.
//
// Apply mutates whatever state the tool requires (typically a config dir
// initialised on first use) and returns the env vars that should be
// injected into a `pm exec` child process to activate the profile. The
// returned map MUST be safe to merge into os.Environ() without escaping
// or quoting — keys are simple identifiers, values are plain strings.
//
// Whoami inspects the current logged-in state of the underlying CLI
// without ever triggering an interactive flow. A tool that would prompt
// for credentials is reported as `LoggedIn: false` with `Error`
// populated; the surrounding code never sees a hang.
type Provider interface {
	// Name returns the stable provider identifier (e.g. "az", "gh").
	Name() string
	// Available reports whether the underlying CLI is on PATH.
	Available() bool
	// Apply returns the env vars to inject into a child process to
	// activate this profile for the provider's tool. It may create or
	// initialise on-disk state (config dirs, baseline config files) as a
	// side effect — those operations MUST be idempotent.
	Apply(p *core.Profile) (env map[string]string, err error)
	// Whoami inspects current state. Never prompts. A non-nil error is
	// only returned for true infrastructure failures (e.g. failure to
	// stat a directory we just created); "not logged in" is a normal
	// Status with LoggedIn=false and Error populated.
	Whoami(ctx context.Context) (Status, error)
}

// Status is the observable state of one tool at one moment.
//
// JSON shape is stable and goes out over MCP / `pm whoami --json`.
// Adding fields is non-breaking; renaming or removing is not.
type Status struct {
	Provider     string            `json:"provider"`
	LoggedIn     bool              `json:"logged_in"`
	Account      string            `json:"account,omitempty"`
	Tenant       string            `json:"tenant,omitempty"`
	Subscription string            `json:"subscription,omitempty"`
	Extra        map[string]string `json:"extra,omitempty"`
	Error        string            `json:"error,omitempty"`
}

// lookPath is the CLI-detection seam. Tests override it to simulate a
// missing tool without poking PATH globally.
var lookPath = exec.LookPath

var (
	regMu sync.RWMutex
	reg   = map[string]Provider{}
)

// Register adds p to the global provider registry, replacing any prior
// entry with the same Name(). Adapters call this from their init()
// functions in registry.go.
func Register(p Provider) {
	if p == nil {
		return
	}
	regMu.Lock()
	defer regMu.Unlock()
	reg[p.Name()] = p
}

// Get returns the registered provider by name, or (nil, false) if not
// registered.
func Get(name string) (Provider, bool) {
	regMu.RLock()
	defer regMu.RUnlock()
	p, ok := reg[name]
	return p, ok
}

// All returns the registered providers sorted by Name() so callers and
// JSON output get a deterministic order.
func All() []Provider {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]Provider, 0, len(reg))
	for _, p := range reg {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

// reset clears the registry. Test-only helper.
func reset() {
	regMu.Lock()
	defer regMu.Unlock()
	reg = map[string]Provider{}
}
