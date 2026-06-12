//go:build !windows

package secrets

import (
	"context"
	"errors"
)

// WinCredResolver is the non-Windows stub. Available() returns false so
// Register-ing it on macOS/Linux is harmless — ResolveRef will report
// [ErrUnavailable] rather than dispatching to a backend that can't work.
type WinCredResolver struct{}

// NewWinCredResolver returns a stub on non-Windows OSes.
func NewWinCredResolver() *WinCredResolver { return &WinCredResolver{} }

// Name returns "wincred".
func (*WinCredResolver) Name() string { return "wincred" }

// Scheme returns "wincred://".
func (*WinCredResolver) Scheme() string { return "wincred://" }

// Available returns false on non-Windows.
func (*WinCredResolver) Available() bool { return false }

// Resolve always fails on non-Windows.
func (r *WinCredResolver) Resolve(_ context.Context, ref string) (Secret, error) {
	err := errors.New("wincred: only supported on Windows")
	LogResolve(r.Name(), ref, AuditError, AuditOptions{Error: err.Error()})
	return Secret{}, err
}

// Describe returns metadata-only with Exists=false on non-Windows.
func (*WinCredResolver) Describe(_ context.Context, ref string) (Metadata, error) {
	return Metadata{
		Scheme:  "wincred",
		Backend: "wincred",
		Ref:     ref,
		Exists:  false,
		Error:   "wincred: only supported on Windows",
	}, nil
}
