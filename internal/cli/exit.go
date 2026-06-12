package cli

import (
	"errors"
	"fmt"

	"github.com/bvorland/profilmanager/internal/core"
)

// Exit codes used across the CLI. The convention is intentionally small
// so operators and scripts can branch reliably:
//
//	0  success
//	1  generic error
//	2  invalid usage (bad flags / args / missing required input)
//	64 command available but not yet wired (skeleton stub)
//
// Code 64 is borrowed from BSD sysexits.h's EX_USAGE slot — picked for
// "stub" rather than the more obvious 2 so that callers can distinguish
// "not yet implemented" from "you used this command wrong".
const (
	ExitOK          = 0
	ExitError       = 1
	ExitUsage       = 2
	ExitUnwiredStub = 64
)

// ExitError carries a specific process exit code through cobra's error
// return so cmd/pm can translate it. Use [errInvalidUsage], [errStub], or
// wrap any other error with [WithExitCode].
type ExitErrorT struct {
	Code int
	Err  error
}

func (e *ExitErrorT) Error() string {
	if e == nil || e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e *ExitErrorT) Unwrap() error { return e.Err }

// WithExitCode wraps err so cmd/pm can extract the desired exit code.
// Returns nil if err is nil.
func WithExitCode(code int, err error) error {
	if err == nil {
		return nil
	}
	return &ExitErrorT{Code: code, Err: err}
}

// CodeFor extracts the exit code from err. Returns [ExitOK] for nil and
// [ExitError] for any non-nil error that does not wrap [ExitErrorT].
func CodeFor(err error) int {
	if err == nil {
		return ExitOK
	}
	if errors.Is(err, core.ErrCancelled) {
		return ExitOK
	}
	var ee *ExitErrorT
	if errors.As(err, &ee) {
		return ee.Code
	}
	return ExitError
}

// errInvalidUsage builds an ExitErrorT with [ExitUsage] for arg/flag
// problems. Use this instead of returning bare errors from RunE when the
// fault is operator input.
func errInvalidUsage(format string, args ...any) error {
	return &ExitErrorT{Code: ExitUsage, Err: fmt.Errorf(format, args...)}
}

// errStub returns the standard "not yet implemented" error for skeleton
// stubs. deps is the human description of what the verb is waiting on
// (e.g. "providers, secrets"). emitError will prefix the command path.
func errStub(_ string, deps string) error {
	return &ExitErrorT{
		Code: ExitUnwiredStub,
		Err:  fmt.Errorf("not yet implemented — wired in next phase (depends on: %s — see .squad/decisions.md)", deps),
	}
}
