package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/bvorland/profilmanager/internal/core"
	"github.com/bvorland/profilmanager/internal/state"
)

// callRequestWith builds a CallToolRequest with the given arguments
// map — the SDK's public types don't expose a constructor we can use
// from tests, so we hand-build the shape the GetString / RequireString
// accessors read.
func callRequestWith(args map[string]any) mcp.CallToolRequest {
	r := mcp.CallToolRequest{}
	r.Params.Name = "test"
	r.Params.Arguments = args
	return r
}

// extractStructured unwraps a *mcp.CallToolResult into its
// StructuredContent decoded as the requested generic type. Tests use
// this so they assert on a typed value rather than parsing the
// JSON-string fallback.
func extractStructured[T any](t *testing.T, res *mcp.CallToolResult) T {
	t.Helper()
	if res == nil {
		t.Fatal("nil tool result")
	}
	if res.IsError {
		// Render the text content for the failure message.
		var txt string
		for _, c := range res.Content {
			if tc, ok := c.(mcp.TextContent); ok {
				txt = tc.Text
			}
		}
		t.Fatalf("tool returned error: %s", txt)
	}
	var out T
	if res.StructuredContent != nil {
		// Round-trip through JSON to handle both map[string]any and
		// concrete struct paths.
		b, err := json.Marshal(res.StructuredContent)
		if err != nil {
			t.Fatalf("re-marshal structured content: %v", err)
		}
		if err := json.Unmarshal(b, &out); err != nil {
			t.Fatalf("unmarshal structured content into %T: %v\n%s", out, err, b)
		}
		return out
	}
	// Fallback: parse the text JSON.
	for _, c := range res.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			if err := json.Unmarshal([]byte(tc.Text), &out); err != nil {
				t.Fatalf("unmarshal text content: %v", err)
			}
			return out
		}
	}
	t.Fatal("no content in result")
	return out
}

// TestListProfilesEmptyDirectory exercises the empty-state path: a
// fresh ProfilesDir should yield an empty list and a populated
// resolvers map.
func TestListProfilesEmptyDirectory(t *testing.T) {
	withFreshState(t)
	res, err := handleListProfiles(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	out := extractStructured[listProfilesResult](t, res)
	if out.ProfileCount != 0 {
		t.Errorf("ProfileCount = %d, want 0", out.ProfileCount)
	}
	if out.Resolvers == nil {
		t.Errorf("Resolvers map missing")
	}
	if _, ok := out.Resolvers["op"]; !ok {
		t.Errorf("resolvers map missing op entry: %+v", out.Resolvers)
	}
}

// TestListProfilesWithSavedProfile verifies a saved profile shows up
// with the expected metadata and that resolver availability is included.
func TestListProfilesWithSavedProfile(t *testing.T) {
	withFreshState(t)
	writeProfile(t, "Alpha", []core.EnvEntry{
		{Key: "FOO", Value: "bar"},
		{Key: "BAR", Ref: "op://V/Item/field"},
	})
	res, err := handleListProfiles(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	out := extractStructured[listProfilesResult](t, res)
	if out.ProfileCount != 1 {
		t.Fatalf("ProfileCount = %d, want 1", out.ProfileCount)
	}
	if out.Profiles[0].Name != "Alpha" {
		t.Errorf("Name = %q, want Alpha", out.Profiles[0].Name)
	}
	if out.Profiles[0].EnvCount != 2 {
		t.Errorf("EnvCount = %d, want 2", out.Profiles[0].EnvCount)
	}
}

// TestGetProfileSurfacesRefs verifies that refs come through in the
// returned body but resolved values do not (no resolution happens).
func TestGetProfileSurfacesRefs(t *testing.T) {
	withFreshState(t)
	writeProfile(t, "Beta", []core.EnvEntry{
		{Key: "API_KEY", Ref: "op://V/Item/credential"},
	})
	res, err := handleGetProfile(context.Background(),
		callRequestWith(map[string]any{"name": "Beta"}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	out := extractStructured[getProfileResult](t, res)
	if out.Name != "Beta" {
		t.Errorf("Name = %q, want Beta", out.Name)
	}
	b, _ := json.Marshal(out.Profile)
	if !strings.Contains(string(b), "op://V/Item/credential") {
		t.Errorf("ref missing from profile body: %s", b)
	}
}

// TestGetProfileNotFoundReturnsToolError verifies the not-found path
// returns an IsError result rather than panicking or returning a Go
// error to the SDK (which would surface as an internal error).
func TestGetProfileNotFoundReturnsToolError(t *testing.T) {
	withFreshState(t)
	res, err := handleGetProfile(context.Background(),
		callRequestWith(map[string]any{"name": "nonexistent"}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !res.IsError {
		t.Errorf("expected IsError=true for missing profile")
	}
}

// TestSwitchProfileSetsActive checks the happy path: switch records
// the active profile and the metadata file persists.
func TestSwitchProfileSetsActive(t *testing.T) {
	withFreshState(t)
	writeProfile(t, "Gamma", nil)

	res, err := handleSwitchProfile(context.Background(),
		callRequestWith(map[string]any{"name": "Gamma"}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	out := extractStructured[switchProfileResult](t, res)
	if out.Active != "Gamma" {
		t.Errorf("Active = %q, want Gamma", out.Active)
	}
	got, _, err := state.GetActiveProfile()
	if err != nil {
		t.Fatalf("GetActiveProfile: %v", err)
	}
	if got != "Gamma" {
		t.Errorf("state.GetActiveProfile = %q, want Gamma", got)
	}
}

// TestSwitchProfileEmptyClears verifies the empty-name path clears
// the active marker.
func TestSwitchProfileEmptyClears(t *testing.T) {
	withFreshState(t)
	writeProfile(t, "Delta", nil)
	if err := state.SetActiveProfile("Delta"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	res, err := handleSwitchProfile(context.Background(),
		callRequestWith(map[string]any{"name": ""}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	out := extractStructured[switchProfileResult](t, res)
	if out.Active != "" {
		t.Errorf("Active = %q, want empty", out.Active)
	}
	got, _, _ := state.GetActiveProfile()
	if got != "" {
		t.Errorf("active not cleared: %q", got)
	}
}

// TestSwitchProfileUnknownReturnsToolError covers the "agent typoed
// the name" case — must not panic, must not crash, must surface a
// clean error to the agent.
func TestSwitchProfileUnknownReturnsToolError(t *testing.T) {
	withFreshState(t)
	res, err := handleSwitchProfile(context.Background(),
		callRequestWith(map[string]any{"name": "nobody-home"}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !res.IsError {
		t.Errorf("expected IsError=true for unknown profile")
	}
}

// TestGetActiveProfileReflectsState confirms get_active_profile returns
// whatever switch_profile wrote.
func TestGetActiveProfileReflectsState(t *testing.T) {
	withFreshState(t)
	writeProfile(t, "Eps", nil)
	if err := state.SetActiveProfile("Eps"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	res, err := handleGetActiveProfile(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	out := extractStructured[getActiveProfileResult](t, res)
	if out.Active != "Eps" {
		t.Errorf("Active = %q, want Eps", out.Active)
	}
	if out.SessionID == "" {
		t.Errorf("SessionID empty")
	}
}

// TestResolveSecretRefReturnsMetadataOnly is the iron-rule test: even
// a literal "value" ref must come back as metadata only — never the
// resolved bytes.
func TestResolveSecretRefReturnsMetadataOnly(t *testing.T) {
	withFreshState(t)
	res, err := handleResolveSecretRef(context.Background(),
		callRequestWith(map[string]any{"ref": "plain-literal-value"}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	b, _ := json.Marshal(res.StructuredContent)
	// "plain-literal-value" is the ref string itself — it's allowed
	// in the response (it's the ref, by definition). What we MUST
	// not see is a "value" field with the resolved bytes.
	if strings.Contains(string(b), `"value":`) {
		t.Fatalf("response contains a value field — iron rule violation: %s", b)
	}
	out := extractStructured[resolveSecretRefResult](t, res)
	if !strings.Contains(out.Note, "NEVER returned") {
		t.Errorf("Note missing the NEVER-returned reminder: %q", out.Note)
	}
}

// TestResolveSecretRefAuditsEveryCall ensures every resolve_secret_ref
// call lands in mcp.log — every secret-related access must be
// auditable even when the call is just metadata.
func TestResolveSecretRefAuditsEveryCall(t *testing.T) {
	withFreshState(t)
	if _, err := handleResolveSecretRef(context.Background(),
		callRequestWith(map[string]any{"ref": "op://V/Item/field"})); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	dir, _ := AuditDir()
	entries := readAuditLines(t, dir)
	if len(entries) == 0 {
		t.Fatal("no audit entries written for resolve_secret_ref")
	}
	last := entries[len(entries)-1]
	if last.Tool != "resolve_secret_ref" {
		t.Errorf("Tool field wrong: %q", last.Tool)
	}
	if last.Ref != "op://V/Item/field" {
		t.Errorf("Ref field wrong: %q", last.Ref)
	}
}

// TestWhoamiReturnsProvidersAndResolvers smoke-tests the whoami tool:
// every registered provider appears in the response, and the resolver
// availability map is populated.
func TestWhoamiReturnsProvidersAndResolvers(t *testing.T) {
	withFreshState(t)
	res, err := handleWhoami(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	out := extractStructured[whoamiResult](t, res)
	if len(out.Providers) == 0 {
		t.Errorf("Providers list empty — provider registry not wired")
	}
	if out.Resolvers == nil || len(out.Resolvers) == 0 {
		t.Errorf("Resolvers map empty")
	}
}
