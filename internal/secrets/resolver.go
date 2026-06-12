package secrets

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Resolver is the contract every backend implements. Each resolver owns a
// single scheme (or, for [DotEnvResolver], the empty scheme for literals).
type Resolver interface {
	// Name is the stable resolver identifier ("op", "wincred", "dotenv").
	Name() string
	// Scheme is the URI-style prefix this resolver claims (e.g. "op://",
	// "wincred://", "dotenv://"). The empty string means "no scheme —
	// this is a literal value passthrough".
	Scheme() string
	// Available reports whether the backend can be used on this host.
	// Must not perform interactive login. May cache its result.
	Available() bool
	// Resolve returns the secret for ref. Implementations MUST audit-log
	// the attempt (success or failure) via [LogResolve] without ever
	// logging the resolved value.
	Resolve(ctx context.Context, ref string) (Secret, error)
	// Describe returns metadata only — safe to embed in MCP responses
	// and `--json` output. MUST NOT call [LogResolve] (it is a
	// read-only metadata operation).
	Describe(ctx context.Context, ref string) (Metadata, error)
}

// Metadata is the read-only "what is this ref pointing at?" view. It is
// the only secret-related shape that may be returned over MCP or printed
// in JSON output. By construction it carries no resolved value.
type Metadata struct {
	Scheme  string `json:"scheme"`
	Backend string `json:"backend"`
	Ref     string `json:"ref"`
	Vault   string `json:"vault,omitempty"`
	Item    string `json:"item,omitempty"`
	Field   string `json:"field,omitempty"`
	Exists  bool   `json:"exists"`
	Error   string `json:"error,omitempty"`
}

// ErrNoResolver is returned by [ResolveRef] / [DescribeRef] when no
// registered resolver claims the ref's scheme.
var ErrNoResolver = errors.New("no resolver registered for scheme")

// ErrUnavailable is returned when a registered resolver cannot run on
// this host (e.g. wincred on Linux, op when the CLI isn't installed).
var ErrUnavailable = errors.New("resolver unavailable on this host")

var (
	registryMu sync.RWMutex
	registry   = map[string]Resolver{}
)

// Register installs r in the package-level registry, keyed by its
// [Resolver.Name]. Subsequent calls with the same name replace the prior
// entry (so tests can swap a real resolver for a fake without globals
// surgery). Safe for concurrent use.
func Register(r Resolver) {
	if r == nil {
		panic("secrets.Register: nil resolver")
	}
	name := r.Name()
	if name == "" {
		panic("secrets.Register: resolver has empty name")
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[name] = r
}

// Unregister removes the named resolver. Primarily for tests.
func Unregister(name string) {
	registryMu.Lock()
	defer registryMu.Unlock()
	delete(registry, name)
}

// Get returns the resolver registered under name.
func Get(name string) (Resolver, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	r, ok := registry[name]
	return r, ok
}

// All returns every registered resolver, sorted by name for deterministic
// iteration (whoami output, doctor reports, etc.).
func All() []Resolver {
	registryMu.RLock()
	names := make([]string, 0, len(registry))
	for n := range registry {
		names = append(names, n)
	}
	out := make([]Resolver, 0, len(registry))
	registryMu.RUnlock()
	sort.Strings(names)
	for _, n := range names {
		if r, ok := Get(n); ok {
			out = append(out, r)
		}
	}
	return out
}

// resolverForRef picks the backend that owns ref's scheme.
//
// Dispatch rules, in order:
//
//  1. "op://..."       → resolver whose [Resolver.Scheme] == "op://".
//  2. "wincred://..."  → resolver whose [Resolver.Scheme] == "wincred://".
//  3. "<name>://..."   → resolver whose [Resolver.Name] == "<name>"
//     (covers "dotenv://path#KEY" routing to the dotenv resolver, which
//     advertises Scheme() == "" because its primary identity is the
//     literal handler).
//  4. no scheme        → the "" (literal) resolver, if registered.
//
// Schemes are matched case-insensitively on the prefix only — the
// remainder is preserved verbatim (vault and item names may be
// case-sensitive on the backend).
func resolverForRef(ref string) (Resolver, error) {
	scheme := schemeOf(ref)
	registryMu.RLock()
	defer registryMu.RUnlock()
	// (1) and (2): match by declared Scheme().
	for _, r := range registry {
		rs := r.Scheme()
		if rs != "" && strings.EqualFold(rs, scheme) {
			return r, nil
		}
	}
	// (3): fall back to matching by "<name>://".
	if scheme != "" {
		bare := strings.ToLower(strings.TrimSuffix(scheme, "://"))
		for _, r := range registry {
			if strings.EqualFold(r.Name(), bare) {
				return r, nil
			}
		}
	}
	// (4): literal handler.
	if scheme == "" {
		for _, r := range registry {
			if r.Scheme() == "" {
				return r, nil
			}
		}
		return nil, fmt.Errorf("%w: literal (no scheme)", ErrNoResolver)
	}
	return nil, fmt.Errorf("%w: %s", ErrNoResolver, strings.TrimSuffix(scheme, "://"))
}

// schemeOf returns the "scheme://" prefix of ref, or "" if there is no
// scheme. Recognises the v1 set ("op", "wincred", "dotenv"); anything
// else with a "://" is also treated as a scheme so the registry can warn
// cleanly with [ErrNoResolver].
func schemeOf(ref string) string {
	idx := strings.Index(ref, "://")
	if idx <= 0 {
		return ""
	}
	for i := 0; i < idx; i++ {
		c := ref[i]
		switch {
		case c >= 'a' && c <= 'z',
			c >= 'A' && c <= 'Z',
			c >= '0' && c <= '9',
			c == '+', c == '-', c == '.':
			continue
		default:
			return ""
		}
	}
	return ref[:idx+3]
}

// ResolveRef dispatches to the resolver that claims ref's scheme and
// returns the secret. Callers MUST [Secret.Zero] the returned value when
// they no longer need the plaintext (typically: after copying into a
// child process env block).
func ResolveRef(ctx context.Context, ref string) (Secret, error) {
	r, err := resolverForRef(ref)
	if err != nil {
		return Secret{}, err
	}
	if !r.Available() {
		return Secret{}, fmt.Errorf("%w: %s", ErrUnavailable, r.Name())
	}
	return r.Resolve(ctx, ref)
}

// DescribeRef dispatches to the resolver that claims ref's scheme and
// returns metadata only. Safe for MCP responses and `--json` output.
func DescribeRef(ctx context.Context, ref string) (Metadata, error) {
	r, err := resolverForRef(ref)
	if err != nil {
		return Metadata{Ref: ref, Error: err.Error()}, err
	}
	return r.Describe(ctx, ref)
}
