//go:build windows

package secrets

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/danieljoos/wincred"
)

// WinCredResolver reads secrets from the Windows Credential Manager via
// the `cred*` Win32 APIs (DPAPI-backed). Implemented as a thin wrapper
// over github.com/danieljoos/wincred so we don't carry our own cgo /
// syscall surface.
//
// # Refs
//
//	wincred://<TargetName>                — match by target
//	wincred://<TargetName>/<UserName>     — match by target + username
//
// The TargetName is what you see in `cmdkey /list` as the credential
// name. UserName is optional and only checked when present (so the
// resolver can pick the right "generic" credential when several share a
// target prefix).
type WinCredResolver struct{}

// NewWinCredResolver returns a Windows Credential Manager resolver.
// On Windows this is the real implementation; on other GOOSes the stub
// in wincred_other.go is built instead.
func NewWinCredResolver() *WinCredResolver { return &WinCredResolver{} }

// Name returns "wincred".
func (*WinCredResolver) Name() string { return "wincred" }

// Scheme returns "wincred://".
func (*WinCredResolver) Scheme() string { return "wincred://" }

// Available is always true on Windows. We don't pre-probe the credential
// store — its API is in-process and reliably present on every supported
// Windows SKU.
func (*WinCredResolver) Available() bool { return true }

// Resolve fetches the credential and returns its password.
func (r *WinCredResolver) Resolve(_ context.Context, ref string) (Secret, error) {
	target, user, err := parseWinCredRef(ref)
	if err != nil {
		LogResolve(r.Name(), ref, AuditError, AuditOptions{Error: err.Error()})
		return Secret{}, err
	}

	cred, err := wincred.GetGenericCredential(target)
	if err != nil {
		// danieljoos/wincred wraps ERROR_NOT_FOUND as a typed error.
		// Match by string to avoid an extra import.
		if isWinCredNotFound(err) {
			werr := fmt.Errorf("wincred: target %q not found", target)
			LogResolve(r.Name(), ref, AuditMiss, AuditOptions{Error: werr.Error()})
			return Secret{}, werr
		}
		werr := fmt.Errorf("wincred get %q: %w", target, err)
		LogResolve(r.Name(), ref, AuditError, AuditOptions{Error: werr.Error()})
		return Secret{}, werr
	}
	if user != "" && !strings.EqualFold(cred.UserName, user) {
		werr := fmt.Errorf("wincred: target %q exists but username %q does not match (have %q)", target, user, cred.UserName)
		LogResolve(r.Name(), ref, AuditMiss, AuditOptions{Error: werr.Error()})
		return Secret{}, werr
	}
	if len(cred.CredentialBlob) == 0 {
		werr := fmt.Errorf("wincred: target %q has empty credential blob", target)
		LogResolve(r.Name(), ref, AuditMiss, AuditOptions{Error: werr.Error()})
		return Secret{}, werr
	}

	// Copy so we own the slice and can zero it independently of whatever
	// the wincred package retains.
	owned := make([]byte, len(cred.CredentialBlob))
	copy(owned, cred.CredentialBlob)
	LogResolve(r.Name(), ref, AuditOK, AuditOptions{})
	return NewSecret(owned), nil
}

// Describe returns metadata for the credential: target, username, last
// modified. Never reveals the password.
func (r *WinCredResolver) Describe(_ context.Context, ref string) (Metadata, error) {
	md := Metadata{Scheme: "wincred", Backend: r.Name(), Ref: ref}
	target, user, err := parseWinCredRef(ref)
	if err != nil {
		md.Error = err.Error()
		return md, err
	}
	md.Item = target
	if user != "" {
		md.Field = user // re-use Field for the username here; close enough for a generic metadata view
	}

	cred, err := wincred.GetGenericCredential(target)
	if err != nil {
		if isWinCredNotFound(err) {
			md.Exists = false
			return md, nil
		}
		md.Error = err.Error()
		return md, fmt.Errorf("wincred get %q: %w", target, err)
	}
	if user != "" && !strings.EqualFold(cred.UserName, user) {
		md.Exists = false
		md.Error = fmt.Sprintf("username mismatch: have %q, want %q", cred.UserName, user)
		return md, nil
	}
	md.Exists = true
	// We add LastModified into Error-as-info? No — Metadata.Error is
	// reserved for failure messages. Leave the timestamp out of the
	// public surface for now and revisit in v1.1 if operators ask.
	_ = time.Time(cred.LastWritten)
	if md.Field == "" {
		md.Field = cred.UserName
	}
	return md, nil
}

// parseWinCredRef extracts target and optional username from a wincred URI.
func parseWinCredRef(ref string) (target, user string, err error) {
	if !strings.HasPrefix(ref, "wincred://") {
		return "", "", fmt.Errorf("wincred: ref must start with wincred://, got %q", ref)
	}
	rest := strings.TrimPrefix(ref, "wincred://")
	if rest == "" {
		return "", "", fmt.Errorf("wincred: empty ref")
	}
	if idx := strings.Index(rest, "/"); idx >= 0 {
		return rest[:idx], rest[idx+1:], nil
	}
	return rest, "", nil
}

// isWinCredNotFound checks for the ERROR_NOT_FOUND signal that
// danieljoos/wincred surfaces as a syscall.Errno (0x80070490 / 1168).
func isWinCredNotFound(err error) bool {
	if err == nil {
		return false
	}
	// syscall.Errno satisfies error; comparing by Error() text keeps us
	// out of a syscall import on non-Windows.
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "element not found") ||
		strings.Contains(msg, "cannot find the credential") ||
		strings.Contains(msg, "no such credential") {
		return true
	}
	// danieljoos/wincred returns a sentinel error in some versions.
	var notFound interface{ NotFound() bool }
	if errors.As(err, &notFound) {
		return notFound.NotFound()
	}
	return false
}
