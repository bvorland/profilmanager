package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/bvorland/profilmanager/internal/providers"
	"github.com/bvorland/profilmanager/internal/state"
)

func init() {
	// Surface registered providers in `pm doctor` so operators can see
	// what pm thinks is wired without running `pm whoami`. One line per
	// provider with its Available() result.
	RegisterCheck("providers-registered", func() CheckResult {
		all := providers.All()
		if len(all) == 0 {
			return CheckResult{
				Name: "providers-registered", Status: StatusFail,
				Message: "no providers registered (this is a build bug)",
			}
		}
		var parts []string
		anyMissing := false
		for _, p := range all {
			tag := "ok"
			if !p.Available() {
				tag = "missing"
				anyMissing = true
			}
			parts = append(parts, fmt.Sprintf("%s(%s)", p.Name(), tag))
		}
		status := StatusOK
		msg := strings.Join(parts, " ")
		if anyMissing {
			status = StatusWarn
			msg = "some provider CLIs not on PATH — " + msg
		}
		return CheckResult{
			Name: "providers-registered", Status: status,
			Message: msg,
		}
	})
}

// newWhoamiCmd implements `pm whoami` — a status aggregator over every
// registered provider plus a drift report.
//
// Iron rule: never trigger interactive flows. Whoami is
// lazy; if a tool would prompt, it's reported as "not logged in" and
// we move on.
//
// With --profile-name, the verb degrades to a single-line print of the
// active profile name for the current session (used by shell shims; see
// internal/cli/shellinit.go).
func newWhoamiCmd() *cobra.Command {
	var profileNameOnly bool
	cmd := &cobra.Command{
		Use:   "whoami",
		Short: "Show current per-provider login state and cross-tool drift",
		Long: "whoami inspects every available provider (az, azd, gh, kubectl, git)\n" +
			"and prints a status block per tool plus any cross-tool drift\n" +
			"(e.g. az and azd targeting different subscriptions). Never\n" +
			"prompts; never logs you in.\n\n" +
			"With --json, emits a stable machine-readable report on stdout.\n" +
			"With --profile-name, prints only the active profile name for the\n" +
			"current session (one line, empty if none) — used by shell shims.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if profileNameOnly {
				name, _, err := state.GetActiveProfile()
				if err != nil {
					return emitError(cmd, err)
				}
				if name != "" {
					fmt.Fprintln(cmd.OutOrStdout(), name)
				}
				return nil
			}
			return runWhoami(cmd.Context(), cmd.OutOrStdout(), jsonRequested(cmd))
		},
	}
	addJSONFlag(cmd)
	cmd.Flags().BoolVar(&profileNameOnly, "profile-name", false, "print only the active profile name for the current session (one line)")
	return cmd
}

// whoamiReport is the JSON shape `pm whoami --json` returns. Stable;
// fields may be added, not renamed.
type whoamiReport struct {
	ActiveProfile string             `json:"active_profile"`
	Providers     []providers.Status `json:"providers"`
	Drift         []providers.Drift  `json:"drift"`
}

func runWhoami(ctx context.Context, out io.Writer, asJSON bool) error {
	if ctx == nil {
		ctx = context.Background()
	}
	// Cap the whole call at 30s so a wedged provider doesn't strand
	// the operator. Each individual provider also has its own 15s cap.
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	all := providers.All()
	statuses := make([]providers.Status, 0, len(all))
	for _, p := range all {
		if !p.Available() {
			statuses = append(statuses, providers.Status{
				Provider: p.Name(),
				Error:    p.Name() + " not installed",
			})
			continue
		}
		st, err := p.Whoami(ctx)
		if err != nil {
			st.Provider = p.Name()
			st.Error = err.Error()
		}
		statuses = append(statuses, st)
	}
	drift := providers.DetectDrift(statuses)

	if asJSON {
		return writeJSON(out, whoamiReport{ActiveProfile: activeProfileDisplayName(), Providers: statuses, Drift: drift})
	}
	return renderWhoami(out, statuses, drift)
}

func renderWhoami(out io.Writer, statuses []providers.Status, drift []providers.Drift) error {
	active := activeProfileDisplayName()
	fmt.Fprintf(out, "── active profile: %s ──\n", active)
	if activeProfileEnvName() != "" {
		fmt.Fprintln(out, "  (profile applied via pm env apply or pm exec)")
	} else {
		fmt.Fprintln(out, "  (no pm profile applied to this shell; tools see host config)")
	}
	fmt.Fprintln(out)

	for _, s := range statuses {
		fmt.Fprintf(out, "── %s ──\n", s.Provider)
		switch {
		case s.Error != "" && !s.LoggedIn:
			fmt.Fprintf(out, "  %s\n", s.Error)
		case s.LoggedIn:
			if s.Account != "" {
				fmt.Fprintf(out, "  Account:      %s\n", s.Account)
			}
			if s.Tenant != "" {
				fmt.Fprintf(out, "  Tenant:       %s\n", s.Tenant)
			}
			if s.Subscription != "" {
				fmt.Fprintf(out, "  Subscription: %s\n", s.Subscription)
			}
			for _, k := range sortedKeys(s.Extra) {
				fmt.Fprintf(out, "  %-13s %s\n", k+":", s.Extra[k])
			}
		default:
			fmt.Fprintf(out, "  (not logged in)\n")
		}
		fmt.Fprintln(out)
	}
	if len(drift) > 0 {
		fmt.Fprintln(out, "── drift ──")
		for _, d := range drift {
			fmt.Fprintf(out, "  [%s] %s — %s\n", d.Severity, d.Code, d.Message)
			if d.Fix != "" {
				fmt.Fprintf(out, "    fix: %s\n", d.Fix)
			}
		}
		fmt.Fprintln(out)
	}
	return nil
}

func activeProfileEnvName() string {
	return strings.TrimSpace(os.Getenv("PM_ACTIVE_PROFILE"))
}

func activeProfileDisplayName() string {
	if name := activeProfileEnvName(); name != "" {
		return name
	}
	return "(none — host config)"
}

// sortedKeys returns the keys of m sorted ascending. Small maps; we use
// a tiny insertion sort instead of importing sort just for this.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j-1] > keys[j]; j-- {
			keys[j-1], keys[j] = keys[j], keys[j-1]
		}
	}
	return keys
}
