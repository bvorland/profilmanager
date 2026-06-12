package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"strings"
	"testing"
)

// TestServer_RegistersAllTools is the smoke test: NewServer brings up
// the v1 tool set with the names we promised in the architecture memo.
// If a tool is added/renamed, this test must be updated in lockstep.
func TestServer_RegistersAllTools(t *testing.T) {
	withFreshState(t)
	srv := NewServer(WithErrorLogger(log.New(io.Discard, "", 0)))

	want := []string{
		"list_profiles",
		"get_profile",
		"get_active_profile",
		"switch_profile",
		"whoami",
		"resolve_secret_ref",
		"exec_with_profile",
	}
	got := srv.Tools()
	if len(got) != len(want) {
		t.Fatalf("tool count = %d, want %d (got=%v)", len(got), len(want), got)
	}
	for i, name := range want {
		if got[i] != name {
			t.Errorf("Tools()[%d] = %q, want %q", i, got[i], name)
		}
	}
}

// TestServer_HandleMessage_ToolsList drives a real JSON-RPC tools/list
// call through HandleMessage and asserts the listed tools match what
// the server registered. This proves the SDK plumbing is correct.
func TestServer_HandleMessage_ToolsList(t *testing.T) {
	withFreshState(t)
	srv := NewServer(WithErrorLogger(log.New(io.Discard, "", 0)))

	// Per MCP, callers must initialize before invoking tools/list.
	// (HandleMessage routes both messages; the server holds the state.)
	initReq := []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"mcp-test","version":"0"}}}`)
	if resp := srv.HandleMessage(context.Background(), initReq); resp == nil {
		t.Fatal("initialize response was nil")
	}
	// Send the initialized notification to complete the handshake.
	notif := []byte(`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`)
	_ = srv.HandleMessage(context.Background(), notif)

	listReq := []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)
	resp := srv.HandleMessage(context.Background(), listReq)
	if resp == nil {
		t.Fatal("tools/list response was nil")
	}
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	for _, name := range srv.Tools() {
		if !bytes.Contains(b, []byte(`"`+name+`"`)) {
			t.Errorf("tools/list missing %q in response: %s", name, b)
		}
	}
}

// TestServer_ServeOn drives the stdio loop against bytes buffers,
// proving the end-to-end pipe works (write a JSON-RPC line in, read a
// JSON-RPC line out). This is the closest we get to a real Copilot CLI
// session without spawning processes.
func TestServer_ServeOn(t *testing.T) {
	withFreshState(t)
	srv := NewServer(WithErrorLogger(log.New(io.Discard, "", 0)))

	in := &bytes.Buffer{}
	out := &bytes.Buffer{}

	// Pre-load the initialize + initialized + tools/list pipeline.
	in.WriteString(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"mcp-test","version":"0"}}}` + "\n")
	in.WriteString(`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}` + "\n")
	in.WriteString(`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}` + "\n")
	// Closing the input (EOF) is the cleanest shutdown signal for the
	// SDK's stdio loop — it returns nil rather than an error.

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ServeOn(ctx, in, out) }()

	// Wait for the loop to exit. EOF on `in` will trigger return.
	if err := <-errCh; err != nil {
		t.Fatalf("ServeOn: %v", err)
	}

	body := out.String()
	if !strings.Contains(body, `"jsonrpc":"2.0"`) {
		t.Fatalf("no JSON-RPC response on stdout: %q", body)
	}
	if !strings.Contains(body, `"tools"`) {
		t.Fatalf("tools/list payload missing in stdout: %q", body)
	}
}
