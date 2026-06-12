package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/bvorland/profilmanager/internal/secrets"
)

// Version is the embedded MCP server's reported version, surfaced to
// clients during the MCP handshake. Bumped in lockstep with the pm
// binary but kept distinct because the MCP tool surface and the CLI
// surface evolve independently.
const Version = "0.1.0"

// ServerName is the identifier the MCP handshake reports to clients
// (Copilot CLI, Squad, Claude Desktop). Stable; agents may key on it.
const ServerName = "profilmanager"

// Server bundles the mcp-go MCPServer + StdioServer wiring so the CLI
// (and tests) can construct it once and run it.
//
// The Server is single-use: each call to [Server.Serve] /
// [Server.ServeOn] starts an MCP session that persists until stdin
// closes or the context is cancelled. Construct a fresh Server for
// each session.
type Server struct {
	mcp        *server.MCPServer
	stdio      *server.StdioServer
	logger     *log.Logger
	registered []string // names of tools we added; kept for Tools()
}

// Option configures a [Server]. Used by tests to silence the SDK's
// stderr logger; the production CLI uses [NewServer] with no options.
type Option func(*Server)

// WithErrorLogger overrides the stderr-bound logger that the MCP SDK
// uses for its own diagnostics. Tests pass log.New(io.Discard, …, 0)
// to keep test output clean.
func WithErrorLogger(logger *log.Logger) Option {
	return func(s *Server) {
		s.logger = logger
	}
}

// NewServer constructs the MCP server, registers every v1 tool, and
// wires the secret resolvers. Returned Server is ready for
// [Server.Serve] (stdio) or [Server.HandleMessage] (tests).
//
// Side effect: calls [secrets.RegisterBuiltins], which is idempotent.
// The provider registry populates itself via its own init() and needs
// no explicit call.
func NewServer(opts ...Option) *Server {
	// Idempotent — safe to call from CLI init, tests, and the MCP
	// constructor without double-registering.
	secrets.RegisterBuiltins()

	mcpSrv := server.NewMCPServer(
		ServerName,
		Version,
		server.WithToolCapabilities(false),
		server.WithRecovery(),
	)

	s := &Server{
		mcp:    mcpSrv,
		logger: log.New(os.Stderr, "mcp: ", log.LstdFlags),
	}
	for _, opt := range opts {
		opt(s)
	}

	registerTools(s)
	return s
}

// Serve runs the server on stdio (stdin/stdout). Blocks until the
// client disconnects, the context is cancelled, or SIGINT/SIGTERM
// causes the SDK to shut down.
//
// Diagnostic output goes to the configured logger (stderr by default).
// The SDK guarantees nothing it writes goes to stdout — stdout is the
// JSON-RPC channel.
func (s *Server) Serve(ctx context.Context) error {
	s.stdio = server.NewStdioServer(s.mcp)
	s.stdio.SetErrorLogger(s.logger)
	return s.stdio.Listen(ctx, os.Stdin, os.Stdout)
}

// ServeOn is the tests-and-piping variant: caller supplies the
// reader/writer pair so we can drive the server from a bytes.Buffer
// in unit tests. Same semantics as [Server.Serve] otherwise.
func (s *Server) ServeOn(ctx context.Context, in io.Reader, out io.Writer) error {
	s.stdio = server.NewStdioServer(s.mcp)
	s.stdio.SetErrorLogger(s.logger)
	return s.stdio.Listen(ctx, in, out)
}

// HandleMessage exposes the raw JSON-RPC entry point. Useful in tests
// to drive a single request through the server without spinning up
// stdio plumbing. Returns nil for notifications.
func (s *Server) HandleMessage(ctx context.Context, message []byte) mcp.JSONRPCMessage {
	return s.mcp.HandleMessage(ctx, message)
}

// Tools returns the list of MCP tool names this server has registered,
// in registration order. Stable; intended for `pm doctor` and tests.
func (s *Server) Tools() []string {
	return append([]string(nil), s.registered...)
}

// String — convenience for debug output / log lines.
func (s *Server) String() string {
	return fmt.Sprintf("mcp.Server(%s v%s, %d tools)", ServerName, Version, len(s.registered))
}

// addTool registers a single tool with the underlying MCPServer and
// tracks its name on the Server. Centralised so tests can assert on
// the registered set in one place.
func (s *Server) addTool(tool mcp.Tool, handler server.ToolHandlerFunc) {
	s.mcp.AddTool(tool, handler)
	s.registered = append(s.registered, tool.Name)
}

// jsonResult is a tiny helper that turns a Go value into a
// CallToolResult carrying both a JSON text representation (so any
// MCP client can render it) and the structured form (for clients that
// support StructuredContent). On marshal failure it returns a tool-
// error result rather than propagating an error to the SDK — tool
// errors are an MCP-protocol concept and stay inside the response.
func jsonResult(v any) *mcp.CallToolResult {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return mcp.NewToolResultErrorf("internal: marshal tool result: %v", err)
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{mcp.TextContent{
			Type: "text",
			Text: string(b),
		}},
		StructuredContent: v,
	}
}
