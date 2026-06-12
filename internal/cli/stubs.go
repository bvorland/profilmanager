package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// This file holds CLI verbs whose RunE is a skeleton: --help is real,
// the surface is documented for operators, but the body returns exit
// code 64 ("not yet implemented — wired in next phase"). The next phase
// fills these in once internal/providers, internal/secrets, internal/mcp
// land. This file MUST NOT import any of those packages.
//
// Each stub follows the same pattern:
//   - Real Use, Short, Long describing the eventual behaviour
//   - Args wired correctly (so `pm <verb>` argument-count errors look right)
//   - RunE returns errStub(verb, deps)

// ---------- pm switch ----------
// Implemented in switch.go. Stub removed.

// ---------- pm whoami ----------
//
// Wired in providers phase — see internal/cli/whoami.go.

// ---------- pm env apply ----------
// Implemented in env.go. Stub removed.

// ---------- pm exec ----------
// Implemented in exec.go. Stub removed.

// ---------- pm shell ----------
// Implemented in shell.go. Stub removed.

// ---------- pm import-mj ----------
// Implemented in import_mj.go (PowerShell shell-out + dotenv).

// ---------- pm mcp ----------
// Implemented in mcp.go (`pm mcp serve` over stdio).

// ---------- pm secret ----------

func newSecretCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secret",
		Short: "Secret reference helpers (add, list, rm, browse)",
		Long: `Manage secret references attached to profiles. Resolved values never
leave the pm process — these verbs only manipulate refs and metadata.

NOT YET IMPLEMENTED — wired in the secrets phase.`,
		Args: cobra.NoArgs,
	}
	for _, sub := range []struct {
		use, short string
	}{
		{"add <profile> <key> <ref>", "Attach a secret ref to a profile env entry"},
		{"list <profile>", "List secret refs for a profile (metadata only)"},
		{"rm <profile> <key>", "Remove a secret ref from a profile"},
		{"browse <profile>", "Open the underlying secret store (op://, wincred, etc.) for a profile"},
	} {
		s := sub
		cmd.AddCommand(&cobra.Command{
			Use:   s.use,
			Short: s.short,
			Args:  cobra.ArbitraryArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				verb := fmt.Sprintf("pm secret %s", cmd.Name())
				return emitError(cmd, errStub(verb, "secrets, providers"))
			},
		})
	}
	return cmd
}
