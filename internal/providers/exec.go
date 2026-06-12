package providers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// defaultTimeout caps any single Whoami shell-out. The isolation
// probes use 15s; we match that so a single hung CLI never wedges
// `pm whoami` for a whole console session.
const defaultTimeout = 15 * time.Second

// runCmd is the indirection adapters use to invoke their CLI. Tests can
// override this to inject canned output; production runs use realRun.
var runCmd = realRun

// realRun executes name with args under ctx and returns (stdout, stderr,
// error). A non-nil error is returned for exec/OS failures and non-zero
// exit codes; stdout/stderr are populated regardless so callers can show
// hints like "Please run 'az login'".
func realRun(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error) {
	var outBuf, errBuf bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	// Inherit the caller's env. Adapters that need a profile-specific
	// AZURE_CONFIG_DIR / AZD_CONFIG_DIR for Whoami should mutate the
	// process environment before calling (we don't, today: Whoami reads
	// the operator's *current* state, not a hypothetical profile's).
	cmd.Env = nil
	if err := cmd.Run(); err != nil {
		return outBuf.Bytes(), errBuf.Bytes(), err
	}
	return outBuf.Bytes(), errBuf.Bytes(), nil
}

// withDefaultTimeout returns ctx unchanged if it already has a deadline,
// otherwise wraps it with defaultTimeout.
func withDefaultTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, defaultTimeout)
}

// truncStderr returns the first 200 chars of stderr, single-lined, for
// inclusion in Status.Error. We never echo the full thing because some
// CLIs include device codes, account hints, or token-cache paths.
func truncStderr(b []byte) string {
	s := strings.TrimSpace(string(b))
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}

// isExitErr returns true if err is a *exec.ExitError (non-zero exit) as
// opposed to a "could not exec" error.
func isExitErr(err error) bool {
	var e *exec.ExitError
	return errors.As(err, &e)
}

// errf builds an error with the conventional "provider: action: detail"
// shape we use across this package.
func errf(provider, action string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %s: %w", provider, action, err)
}
