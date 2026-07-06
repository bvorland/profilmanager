package runner

import (
	"context"
	"strings"
	"testing"

	"github.com/bvorland/profilmanager/internal/core"
	"github.com/bvorland/profilmanager/internal/secrets"
)

func TestComposeNilProfile(t *testing.T) {
	_, cleanup, err := Compose(context.Background(), nil, ComposeOpts{})
	defer cleanup()
	if err == nil {
		t.Fatal("expected error on nil profile")
	}
}

func TestComposeStacksLiteralEnvOverProviders(t *testing.T) {
	// AzProvider always sets AZURE_CORE_OUTPUT=json. An operator who
	// wants xml output should be able to override that via [[env]].
	p := &core.Profile{
		Schema: core.SchemaVersion,
		Name:   "stack",
		Env: []core.EnvEntry{
			{Key: "AZURE_CORE_OUTPUT", Value: "xml"},
			{Key: "MY_VAR", Value: "hello"},
		},
	}
	plan, cleanup, err := Compose(context.Background(), p, ComposeOpts{})
	defer cleanup()
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	if got := plan.Env["AZURE_CORE_OUTPUT"]; got != "xml" {
		t.Fatalf("AZURE_CORE_OUTPUT = %q, want xml (operator override of provider default)", got)
	}
	if got := plan.Env["MY_VAR"]; got != "hello" {
		t.Fatalf("MY_VAR = %q, want hello", got)
	}
	if len(plan.Refs) != 0 {
		t.Fatalf("expected no refs, got %v", plan.Refs)
	}
}

func TestComposeRefsListedNotResolvedByDefault(t *testing.T) {
	p := &core.Profile{
		Schema: core.SchemaVersion,
		Name:   "refs",
		Env: []core.EnvEntry{
			{Key: "B_TOKEN", Ref: "op://Vault/Item/Field"},
			{Key: "A_TOKEN", Ref: "op://Vault/OtherItem/Field"},
		},
	}
	plan, cleanup, err := Compose(context.Background(), p, ComposeOpts{ResolveSecrets: false})
	defer cleanup()
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	if _, ok := plan.Env["B_TOKEN"]; ok {
		t.Fatalf("B_TOKEN must not appear in env when refs are not resolved")
	}
	if len(plan.Refs) != 2 {
		t.Fatalf("want 2 refs, got %d", len(plan.Refs))
	}
	if plan.Refs[0].Key != "A_TOKEN" || plan.Refs[1].Key != "B_TOKEN" {
		t.Fatalf("refs must be sorted by key, got %v", plan.Refs)
	}
}

func TestComposeResolvesRefsWhenAsked(t *testing.T) {
	// Stand up a fake resolver that returns the ref's tail as the value.
	secrets.Register(&fakeResolver{})
	defer secrets.Unregister("fake-runner")

	p := &core.Profile{
		Schema: core.SchemaVersion,
		Name:   "resolve",
		Env: []core.EnvEntry{
			{Key: "TOKEN", Ref: "fake://hello"},
		},
	}
	plan, cleanup, err := Compose(context.Background(), p, ComposeOpts{ResolveSecrets: true})
	defer cleanup()
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	if got := plan.Env["TOKEN"]; got != "hello" {
		t.Fatalf("TOKEN = %q, want hello", got)
	}
}

func TestEnvSliceMergeProfileWinsOverExisting(t *testing.T) {
	existing := []string{"FOO=bar", "AZURE_CONFIG_DIR=/old"}
	env := map[string]string{"AZURE_CONFIG_DIR": "/new", "EXTRA": "1"}
	got := EnvSlice(existing, env)
	want := map[string]string{
		"FOO":              "bar",
		"AZURE_CONFIG_DIR": "/new",
		"EXTRA":            "1",
	}
	for _, kv := range got {
		k, v, ok := splitKV(kv)
		if !ok {
			t.Fatalf("malformed entry: %q", kv)
		}
		if want[k] != v {
			t.Fatalf("%s = %q, want %q", k, v, want[k])
		}
	}
	// Sorted output is part of the contract.
	if !sortedAscending(got) {
		t.Fatalf("EnvSlice output not sorted: %v", got)
	}
}

func sortedAscending(s []string) bool {
	for i := 1; i < len(s); i++ {
		if s[i-1] > s[i] {
			return false
		}
	}
	return true
}

// fakeResolver echoes the ref's tail back as the secret value. Used to
// verify ResolveSecrets actually wires through secrets.ResolveRef.
type fakeResolver struct{}

func (*fakeResolver) Name() string    { return "fake-runner" }
func (*fakeResolver) Scheme() string  { return "fake://" }
func (*fakeResolver) Available() bool { return true }
func (*fakeResolver) Resolve(_ context.Context, ref string) (secrets.Secret, error) {
	tail := strings.TrimPrefix(ref, "fake://")
	return secrets.NewSecretString(tail), nil
}
func (*fakeResolver) Describe(_ context.Context, ref string) (secrets.Metadata, error) {
	return secrets.Metadata{Scheme: "fake://", Backend: "fake-runner", Ref: ref, Exists: true}, nil
}
