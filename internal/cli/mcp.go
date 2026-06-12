package cli

import (
	"context"
	"errors"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/bvorland/profilmanager/internal/mcp"
)

// newMCPCmd wires `pm mcp` — the group for everything Model Context
// Protocol-related. Today only the `serve` subcommand exists; future
// subcommands (e.g. `mcp config` to print the JSON snippet for
// .copilot/mcp-config.json) belong here.
//
// This replaces the stub in stubs.go now that internal/mcp has landed.
func newMCPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Embedded Model Context Protocol server (serve)",
		Long: `Embedded MCP server commands.

` + "`pm mcp serve`" + ` starts the server on stdio so an MCP client
(Copilot CLI, Squad, Claude Desktop, …) can invoke pm's profile and
provider context as tools.

Register the server in .copilot/mcp-config.json (or your client's
equivalent):

    {
      "mcpServers": {
        "profilmanager": {
          "command": "pm",
          "args": ["mcp", "serve"]
        }
      }
    }`,
		Args: cobra.NoArgs,
	}
	cmd.AddCommand(newMCPServeCmd())
	return cmd
}

func newMCPServeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run the embedded MCP server on stdio",
		Long: `Run the embedded MCP server on stdio (stdin/stdout JSON-RPC).

Tools exposed (v1):
  list_profiles        — metadata-only profile list + resolver availability
  get_profile          — full TOML body (refs are metadata; values stay in pm)
  get_active_profile   — current session's active profile name
  switch_profile       — set/clear session-scoped active profile
  whoami               — provider drift report (az, azd, gh, kubectl, git)
  resolve_secret_ref   — metadata-only secret lookup (NEVER returns the value)
  exec_with_profile    — allowlisted, audited, redacted child process

Diagnostics go to stderr; stdout is reserved for the JSON-RPC channel.

The server exits gracefully on SIGINT / SIGTERM and when stdin closes.`,
		Args: cobra.NoArgs,
		RunE: runMCPServe,
	}
}

// runMCPServe blocks on the MCP server until the client disconnects or
// the context is cancelled by a signal. It returns nil on graceful
// shutdown (EOF on stdin); context cancellation is also treated as a
// clean shutdown so `pm mcp serve` does not look broken when an agent
// orderly closes the connection.
func runMCPServe(cmd *cobra.Command, _ []string) error {
	ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	srv := mcp.NewServer()
	err := srv.Serve(ctx)
	if err != nil && !errors.Is(err, context.Canceled) {
		return emitError(cmd, err)
	}
	return nil
}
