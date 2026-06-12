package secrets

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// OpResolver is the 1Password CLI (`op`) backend.
//
// # Refs
//
// We accept the canonical 1Password secret-reference URI:
//
//	op://<Vault>/<Item>/<Field>
//
// Vault, item and field segments may contain spaces; we don't try to be
// clever — we hand the ref to `op read` verbatim and let `op` parse it.
// The Describe path parses the ref shallowly to populate the metadata
// fields and to surface a clean error when the ref is malformed.
//
// # Availability
//
// Available() == true iff (a) the `op` binary is on PATH and (b)
// `op whoami --format=json` succeeds. The result is cached for the
// process lifetime — `op whoami` is fast but it still shells out, and
// `Available()` is called from `pm whoami` and `pm doctor` on every
// invocation. A successful resolve also flips the cache to true.
type OpResolver struct {
	// binary is the executable name; tests inject a fake (e.g.
	// "fake-op.ps1" via PATH manipulation by overriding lookPath).
	binary string

	// timeout is the per-call ceiling for op invocations.
	timeout time.Duration

	// lookPath / execCommandContext are seams for tests.
	lookPath           func(string) (string, error)
	execCommandContext func(ctx context.Context, name string, args ...string) *exec.Cmd

	mu             sync.Mutex
	availableCache *bool
}

// NewOpResolver returns a resolver backed by the system `op` binary.
func NewOpResolver() *OpResolver {
	return &OpResolver{
		binary:             "op",
		timeout:            15 * time.Second,
		lookPath:           exec.LookPath,
		execCommandContext: exec.CommandContext,
	}
}

// Name returns "op".
func (r *OpResolver) Name() string { return "op" }

// Scheme returns "op://".
func (r *OpResolver) Scheme() string { return "op://" }

// Available shells out to `op whoami` (cached). Returns false if `op` is
// missing or the user isn't signed in. We deliberately do NOT trigger
// `op signin` — interactive login during a list/whoami violates the
// charter.
func (r *OpResolver) Available() bool {
	r.mu.Lock()
	if r.availableCache != nil {
		v := *r.availableCache
		r.mu.Unlock()
		return v
	}
	r.mu.Unlock()

	v := r.probeAvailable()

	r.mu.Lock()
	r.availableCache = &v
	r.mu.Unlock()
	return v
}

func (r *OpResolver) probeAvailable() bool {
	if _, err := r.lookPath(r.binary); err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), r.timeout)
	defer cancel()
	cmd := r.execCommandContext(ctx, r.binary, "whoami", "--format=json")
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return false
	}
	// Loose validation — `op whoami` may print just a JSON object on
	// success. If JSON unmarshals to *anything* and exit was 0, accept.
	var sink any
	return json.Unmarshal(out.Bytes(), &sink) == nil
}

// invalidateAvailable forces the next Available() call to re-probe.
// Used by tests; exported via _test.go is not necessary because tests
// in this package can construct fresh resolvers.
func (r *OpResolver) invalidateAvailable() {
	r.mu.Lock()
	r.availableCache = nil
	r.mu.Unlock()
}

// Resolve invokes `op read "<ref>"`. The ref must be a full
// `op://Vault/Item/field` URI.
func (r *OpResolver) Resolve(ctx context.Context, ref string) (Secret, error) {
	if !strings.HasPrefix(ref, "op://") {
		err := fmt.Errorf("op: ref must start with op://, got %q", ref)
		LogResolve(r.Name(), ref, AuditError, AuditOptions{Error: err.Error()})
		return Secret{}, err
	}
	if r.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.timeout)
		defer cancel()
	}
	cmd := r.execCommandContext(ctx, r.binary, "read", ref)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errBuf.String())
		if msg == "" {
			msg = err.Error()
		}
		// Treat "isn't an item" / "no such field" as a miss so callers
		// can distinguish "the ref doesn't exist" from "op crashed".
		result := AuditError
		if isOpMiss(msg) {
			result = AuditMiss
		}
		wrapped := fmt.Errorf("op read %s: %s", ref, msg)
		LogResolve(r.Name(), ref, result, AuditOptions{Error: wrapped.Error()})
		return Secret{}, wrapped
	}
	value := bytes.TrimRight(out.Bytes(), "\r\n")
	if len(value) == 0 {
		err := fmt.Errorf("op read %s: empty value", ref)
		LogResolve(r.Name(), ref, AuditMiss, AuditOptions{Error: err.Error()})
		return Secret{}, err
	}
	LogResolve(r.Name(), ref, AuditOK, AuditOptions{})
	// Take ownership of `value` directly so Zero() can wipe it later.
	// Copy is needed because bytes.Buffer's slice may be reused.
	owned := make([]byte, len(value))
	copy(owned, value)
	for i := range value {
		value[i] = 0
	}
	return NewSecret(owned), nil
}

// Describe parses ref, then calls `op item get` to confirm the item and
// field exist. Returns metadata only — never the resolved value.
func (r *OpResolver) Describe(ctx context.Context, ref string) (Metadata, error) {
	md := Metadata{Scheme: "op", Backend: r.Name(), Ref: ref}
	vault, item, field, perr := parseOpRef(ref)
	if perr != nil {
		md.Error = perr.Error()
		return md, perr
	}
	md.Vault, md.Item, md.Field = vault, item, field

	if !r.Available() {
		md.Error = "op unavailable (not installed or not signed in)"
		return md, ErrUnavailable
	}

	if r.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.timeout)
		defer cancel()
	}
	cmd := r.execCommandContext(ctx, r.binary, "item", "get", item, "--vault", vault, "--format=json")
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errBuf.String())
		if msg == "" {
			msg = err.Error()
		}
		md.Error = msg
		md.Exists = false
		return md, fmt.Errorf("op item get %q --vault %q: %s", item, vault, msg)
	}

	type opField struct {
		ID    string `json:"id"`
		Label string `json:"label"`
	}
	var doc struct {
		Fields []opField `json:"fields"`
	}
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		md.Error = "op item get: unparseable JSON"
		return md, fmt.Errorf("parse op item get: %w", err)
	}
	for _, f := range doc.Fields {
		if strings.EqualFold(f.ID, field) || strings.EqualFold(f.Label, field) {
			md.Exists = true
			return md, nil
		}
	}
	md.Exists = false
	md.Error = fmt.Sprintf("field %q not found on item %q in vault %q", field, item, vault)
	return md, errors.New(md.Error)
}

// parseOpRef splits "op://Vault/Item/Field" into its parts. The vault,
// item and field segments may not contain '/' — 1Password itself encodes
// such characters when generating refs, so we don't try to undo any
// encoding here.
func parseOpRef(ref string) (vault, item, field string, err error) {
	if !strings.HasPrefix(ref, "op://") {
		return "", "", "", fmt.Errorf("op: ref must start with op://, got %q", ref)
	}
	rest := strings.TrimPrefix(ref, "op://")
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", "", "", fmt.Errorf("op: malformed ref %q (want op://Vault/Item/Field)", ref)
	}
	return parts[0], parts[1], parts[2], nil
}

// isOpMiss heuristically classifies an op stderr message as a miss
// (ref does not resolve) vs an error (op crashed / network down / not
// signed in).
func isOpMiss(stderr string) bool {
	s := strings.ToLower(stderr)
	for _, needle := range []string{
		"isn't an item",
		"no such item",
		"not found",
		"could not be found",
		"no item found",
	} {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}
