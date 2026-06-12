// Package runner composes the full env map for a profile by stacking
// provider-contributed vars, user-defined env entries, and (optionally)
// resolved secret values. It is the single source of truth for "what
// env does this profile contribute?" so `pm exec`, `pm shell`, and
// `pm env apply` cannot drift apart.
//
// Layering rules (later wins):
//
//  1. Built-in provider Apply() vars (az/azd/gh/kubectl/git).
//  2. Profile [[env]] entries with `value = "..."` (literal).
//  3. Profile [[env]] entries with `ref = "..."` — secrets are NOT
//     resolved here; the entry surfaces in RefKeys for callers that
//     need to choose between "refuse" and "resolve".
//
// Resolution of secret refs is opt-in via [Compose] options so that
// `pm env apply` (which prints env to a shell) never accidentally writes
// secrets to stdout. Only `pm exec`/`pm shell` request resolution.
package runner

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/bvorland/profilmanager/internal/core"
	"github.com/bvorland/profilmanager/internal/providers"
	"github.com/bvorland/profilmanager/internal/secrets"
)

// ComposeOpts controls how Compose handles secret refs.
type ComposeOpts struct {
	// ResolveSecrets, when true, dispatches each ref through
	// secrets.ResolveRef and inlines the plaintext into the env map.
	// The returned cleanup closure zeroes every resolved secret.
	//
	// When false, refs are NOT touched; the caller can inspect Plan.Refs
	// and decide whether to refuse, passthrough, or resolve later.
	ResolveSecrets bool
}

// Plan is the composed env for a profile. Env always reflects the
// fully-stacked literal env (provider vars + value-only entries). Refs
// lists every ref-typed entry by key in stable order so callers can
// either resolve them (exec/shell) or refuse them (env apply).
//
// ProviderErrors is non-empty when one or more provider Apply() calls
// failed. We collect rather than abort so a missing kube config dir
// doesn't blow up a profile that only cares about az.
type Plan struct {
	Env            map[string]string
	Refs           []RefEntry
	ProviderErrors []ProviderError
}

// RefEntry is one (Key, Ref) pair contributed by the profile's [[env]]
// table. Refs survive into the Plan even when ResolveSecrets is false.
type RefEntry struct {
	Key string
	Ref string
}

// ProviderError pairs a provider name with the error its Apply produced.
type ProviderError struct {
	Provider string
	Err      error
}

func (p ProviderError) Error() string {
	return fmt.Sprintf("provider %s: %v", p.Provider, p.Err)
}

// Compose builds the env Plan for profile. When opts.ResolveSecrets is
// true, the returned cleanup closure MUST be called once the env has
// been consumed (typically right after the child process exits) so the
// underlying secret bytes are zeroed.
//
// The cleanup is never nil — it's a no-op when no secrets were resolved,
// so callers can `defer cleanup()` unconditionally.
func Compose(ctx context.Context, profile *core.Profile, opts ComposeOpts) (Plan, func(), error) {
	cleanup := func() {}
	if profile == nil {
		return Plan{}, cleanup, errors.New("compose: profile is nil")
	}

	out := map[string]string{}

	// 1. Provider-contributed vars. Built-ins are deterministic so we
	// iterate providers.All() (already sorted by name).
	var provErrs []ProviderError
	for _, p := range providers.All() {
		env, err := p.Apply(profile)
		if err != nil {
			provErrs = append(provErrs, ProviderError{Provider: p.Name(), Err: err})
			continue
		}
		for k, v := range env {
			out[k] = v
		}
	}

	// 2. & 3. Profile [[env]] entries — literals override provider vars
	// (operator wins). Refs are collected separately.
	var refs []RefEntry
	for _, e := range profile.Env {
		switch {
		case e.Ref != "":
			refs = append(refs, RefEntry{Key: e.Key, Ref: e.Ref})
		case e.Value != "":
			out[e.Key] = e.Value
		}
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].Key < refs[j].Key })

	plan := Plan{Env: out, Refs: refs, ProviderErrors: provErrs}

	if !opts.ResolveSecrets || len(refs) == 0 {
		return plan, cleanup, nil
	}

	// Resolve refs in memory. We collect every Secret so the cleanup
	// closure can Zero all of them — even on partial failure.
	resolved := make([]secrets.Secret, 0, len(refs))
	cleanup = func() {
		for i := range resolved {
			resolved[i].Zero()
		}
	}
	for _, r := range refs {
		s, err := secrets.ResolveRef(ctx, r.Ref)
		if err != nil {
			cleanup()
			return Plan{}, func() {}, fmt.Errorf("resolve %s (%s): %w", r.Key, r.Ref, err)
		}
		resolved = append(resolved, s)
		out[r.Key] = s.Reveal()
	}
	return plan, cleanup, nil
}

// EnvSlice returns env as a sorted KEY=VALUE slice suitable for
// exec.Cmd.Env merging. Keys are deduplicated against existing (which
// is typically os.Environ()): later wins, with the profile env winning
// over the operator's existing env so the per-profile config dirs are
// guaranteed to apply.
func EnvSlice(existing []string, env map[string]string) []string {
	merged := make(map[string]string, len(existing)+len(env))
	for _, kv := range existing {
		k, v, ok := splitKV(kv)
		if !ok {
			continue
		}
		merged[k] = v
	}
	for k, v := range env {
		merged[k] = v
	}
	keys := make([]string, 0, len(merged))
	for k := range merged {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+merged[k])
	}
	return out
}

func splitKV(kv string) (string, string, bool) {
	for i := 0; i < len(kv); i++ {
		if kv[i] == '=' {
			return kv[:i], kv[i+1:], true
		}
	}
	return "", "", false
}
