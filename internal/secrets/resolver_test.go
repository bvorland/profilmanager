package secrets

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeResolver is a configurable in-memory resolver for dispatch tests.
type fakeResolver struct {
	name      string
	scheme    string
	available bool
	resolveFn func(ctx context.Context, ref string) (Secret, error)
}

func (f *fakeResolver) Name() string   { return f.name }
func (f *fakeResolver) Scheme() string { return f.scheme }
func (f *fakeResolver) Available() bool {
	return f.available
}
func (f *fakeResolver) Resolve(ctx context.Context, ref string) (Secret, error) {
	if f.resolveFn != nil {
		return f.resolveFn(ctx, ref)
	}
	return NewSecretString("from-" + f.name + ":" + ref), nil
}
func (f *fakeResolver) Describe(_ context.Context, ref string) (Metadata, error) {
	return Metadata{Scheme: f.name, Backend: f.name, Ref: ref, Exists: true}, nil
}

// withFreshRegistry swaps the package registry for the duration of the
// test, restoring the original (and rotation cfg) on cleanup.
func withFreshRegistry(t *testing.T) {
	t.Helper()
	registryMu.Lock()
	saved := registry
	registry = map[string]Resolver{}
	registryMu.Unlock()
	t.Cleanup(func() {
		registryMu.Lock()
		registry = saved
		registryMu.Unlock()
	})
}

func TestRegisterAndGet(t *testing.T) {
	withFreshRegistry(t)
	r := &fakeResolver{name: "test", scheme: "test://", available: true}
	Register(r)
	got, ok := Get("test")
	if !ok || got != r {
		t.Fatalf("Get(test): ok=%v got=%v", ok, got)
	}
	all := All()
	if len(all) != 1 || all[0].Name() != "test" {
		t.Fatalf("All(): %+v", all)
	}
	Unregister("test")
	if _, ok := Get("test"); ok {
		t.Fatalf("Unregister did not remove")
	}
}

func TestRegisterNilPanics(t *testing.T) {
	withFreshRegistry(t)
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on Register(nil)")
		}
	}()
	Register(nil)
}

func TestResolveRefDispatchByScheme(t *testing.T) {
	withFreshRegistry(t)
	op := &fakeResolver{name: "op", scheme: "op://", available: true}
	wc := &fakeResolver{name: "wincred", scheme: "wincred://", available: true}
	de := &fakeResolver{name: "dotenv", scheme: "", available: true}
	Register(op)
	Register(wc)
	Register(de)

	cases := []struct {
		ref  string
		want string
	}{
		{"op://Personal/X/y", "op"},
		{"wincred://Target", "wincred"},
		{"dotenv:///tmp/.env#KEY", "dotenv"}, // by-name routing
		{"just-a-literal", "dotenv"},         // empty-scheme literal
	}
	for _, tc := range cases {
		t.Run(tc.ref, func(t *testing.T) {
			r, err := resolverForRef(tc.ref)
			if err != nil {
				t.Fatalf("resolverForRef: %v", err)
			}
			if r.Name() != tc.want {
				t.Fatalf("ref %q routed to %q, want %q", tc.ref, r.Name(), tc.want)
			}
		})
	}
}

func TestResolveRefUnknownScheme(t *testing.T) {
	withFreshRegistry(t)
	Register(&fakeResolver{name: "op", scheme: "op://", available: true})
	_, err := ResolveRef(context.Background(), "vault://x/y")
	if !errors.Is(err, ErrNoResolver) {
		t.Fatalf("want ErrNoResolver, got %v", err)
	}
}

func TestResolveRefUnavailable(t *testing.T) {
	withFreshRegistry(t)
	Register(&fakeResolver{name: "op", scheme: "op://", available: false})
	_, err := ResolveRef(context.Background(), "op://V/I/F")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
}

func TestResolveRefSuccessReturnsSecret(t *testing.T) {
	withFreshRegistry(t)
	Register(&fakeResolver{
		name: "op", scheme: "op://", available: true,
		resolveFn: func(_ context.Context, ref string) (Secret, error) {
			return NewSecretString("hello-" + ref), nil
		},
	})
	// Audit log must not interfere — direct it to t.TempDir().
	SetAuditDir(t.TempDir())
	t.Cleanup(func() { SetAuditDir("") })

	s, err := ResolveRef(context.Background(), "op://V/I/F")
	if err != nil {
		t.Fatalf("ResolveRef: %v", err)
	}
	defer s.Zero()
	if got := s.Reveal(); !strings.HasPrefix(got, "hello-op://") {
		t.Fatalf("Reveal: %q", got)
	}
}

func TestDescribeRefRouting(t *testing.T) {
	withFreshRegistry(t)
	Register(&fakeResolver{name: "op", scheme: "op://", available: true})
	md, err := DescribeRef(context.Background(), "op://V/I/F")
	if err != nil {
		t.Fatalf("DescribeRef: %v", err)
	}
	if md.Backend != "op" {
		t.Fatalf("Backend: %q", md.Backend)
	}
	// Unknown scheme should populate Error rather than dispatching.
	md2, err := DescribeRef(context.Background(), "vault://x/y")
	if err == nil {
		t.Fatal("expected error for unknown scheme")
	}
	if md2.Error == "" {
		t.Fatalf("Error not populated: %+v", md2)
	}
}

func TestSchemeOf(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"op://x":           "op://",
		"OP://x":           "OP://", // preserved verbatim; matcher lowercases
		"dotenv://path#K":  "dotenv://",
		"wincred://Target": "wincred://",
		"":                 "",
		"plain-literal":    "",
		"weird:thing":      "",
		"with-dash://x":    "with-dash://",
		"://leading-empty": "",
	}
	for in, want := range cases {
		if got := schemeOf(in); got != want {
			t.Errorf("schemeOf(%q)=%q, want %q", in, got, want)
		}
	}
}
