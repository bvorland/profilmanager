package secrets

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
)

// DotEnvResolver handles two ref shapes:
//
//  1. "" (no scheme) — literal passthrough. Used when a profile [[env]]
//     entry has `value = "..."` instead of `ref = "..."`. The "ref"
//     passed in is the literal value itself; we wrap it in a Secret so
//     downstream code uses the same API everywhere.
//  2. "dotenv://path/to/file#KEY" — read KEY from the .env file at path.
//     Path may use ~ for home. We support quoted values and comments;
//     no shell expansion (intentional — we don't want surprises).
type DotEnvResolver struct{}

// NewDotEnvResolver returns the dotenv resolver. Stateless.
func NewDotEnvResolver() *DotEnvResolver { return &DotEnvResolver{} }

// Name returns "dotenv".
func (*DotEnvResolver) Name() string { return "dotenv" }

// Scheme returns "" — this resolver claims literals. The dispatcher
// (resolverForRef) also routes "dotenv://" refs here, since no other
// resolver claims that scheme; Describe/Resolve sniff the prefix.
//
// We picked the empty scheme as the registry key because:
//
//   - It's the natural way to say "I am the default".
//   - The dispatcher already handles "" specially.
//   - The "dotenv://" form is exotic; we don't want it to be the primary
//     identity of the resolver in `pm whoami` output.
func (*DotEnvResolver) Scheme() string { return "" }

// Available is always true. Literal values are always resolvable; file
// reads are deferred to Resolve so a missing file shows up as a clean
// error, not "this backend is unavailable".
func (*DotEnvResolver) Available() bool { return true }

// Resolve returns the secret for ref.
func (r *DotEnvResolver) Resolve(_ context.Context, ref string) (Secret, error) {
	if strings.HasPrefix(ref, "dotenv://") {
		return r.resolveDotEnvFile(ref)
	}
	// Literal passthrough. Even an empty literal is permitted at the
	// resolver layer; the profile validator already enforces that
	// [[env]] entries have one of Value/Ref set.
	LogResolve(r.Name(), redactLiteral(ref), AuditOK, AuditOptions{})
	return NewSecretString(ref), nil
}

// Describe returns metadata for the ref. For literals, we don't even
// echo the literal value back — we report scheme="dotenv" backend="dotenv"
// with Exists=true and no Vault/Item/Field. For dotenv:// refs we
// confirm the file is readable and the key is present.
func (r *DotEnvResolver) Describe(_ context.Context, ref string) (Metadata, error) {
	if strings.HasPrefix(ref, "dotenv://") {
		path, key, err := parseDotEnvRef(ref)
		md := Metadata{Scheme: "dotenv", Backend: r.Name(), Ref: ref, Item: path, Field: key}
		if err != nil {
			md.Error = err.Error()
			return md, err
		}
		vals, err := readDotEnv(path)
		if err != nil {
			md.Error = err.Error()
			return md, err
		}
		_, ok := vals[key]
		md.Exists = ok
		if !ok {
			md.Error = fmt.Sprintf("key %q not present in %s", key, path)
		}
		return md, nil
	}
	// Literal: don't echo the value. The presence of a literal is the
	// metadata.
	return Metadata{
		Scheme:  "dotenv",
		Backend: r.Name(),
		Ref:     "<literal>",
		Exists:  true,
	}, nil
}

func (r *DotEnvResolver) resolveDotEnvFile(ref string) (Secret, error) {
	path, key, err := parseDotEnvRef(ref)
	if err != nil {
		LogResolve(r.Name(), ref, AuditError, AuditOptions{Error: err.Error()})
		return Secret{}, err
	}
	vals, err := readDotEnv(path)
	if err != nil {
		// File-not-found is a miss (ref doesn't resolve), not an error
		// (the backend itself is fine). Permission denied / IO errors
		// stay classified as AuditError.
		result := AuditError
		if errors.Is(err, os.ErrNotExist) {
			result = AuditMiss
		}
		LogResolve(r.Name(), ref, result, AuditOptions{Error: err.Error()})
		return Secret{}, err
	}
	v, ok := vals[key]
	if !ok {
		werr := fmt.Errorf("dotenv: key %q not present in %s", key, path)
		LogResolve(r.Name(), ref, AuditMiss, AuditOptions{Error: werr.Error()})
		return Secret{}, werr
	}
	LogResolve(r.Name(), ref, AuditOK, AuditOptions{})
	return NewSecretString(v), nil
}

// parseDotEnvRef splits "dotenv://path#KEY" into path and key. The path
// is allowed to contain forward slashes; on Windows it can also use
// backslashes after the scheme.
func parseDotEnvRef(ref string) (path, key string, err error) {
	if !strings.HasPrefix(ref, "dotenv://") {
		return "", "", fmt.Errorf("dotenv: ref must start with dotenv://, got %q", ref)
	}
	rest := strings.TrimPrefix(ref, "dotenv://")
	hash := strings.LastIndex(rest, "#")
	if hash < 0 {
		return "", "", fmt.Errorf("dotenv: ref %q missing #KEY suffix", ref)
	}
	rawPath := rest[:hash]
	key = rest[hash+1:]
	if rawPath == "" {
		return "", "", fmt.Errorf("dotenv: empty path in %q", ref)
	}
	if key == "" {
		return "", "", fmt.Errorf("dotenv: empty key in %q", ref)
	}
	// Tolerate URL-encoded segments so an operator can write
	// "dotenv://C:/Users/foo/.env#KEY" or
	// "dotenv://%2Fhome%2Ffoo%2F.env#KEY".
	if decoded, derr := url.PathUnescape(rawPath); derr == nil {
		rawPath = decoded
	}
	if strings.HasPrefix(rawPath, "~") {
		home, herr := os.UserHomeDir()
		if herr == nil {
			rawPath = home + rawPath[1:]
		}
	}
	return rawPath, key, nil
}

// readDotEnv parses a .env file into a map. Semantics, intentionally
// conservative — we mimic the subset of common .env behaviour that does
// not depend on the user's shell:
//
//   - Lines starting with '#' (after optional whitespace) are comments.
//   - Blank lines are skipped.
//   - Lines are "KEY=VALUE". An optional leading "export " is stripped.
//   - VALUE may be wrapped in single or double quotes; the quotes are
//     removed, the contents are taken verbatim. **No** $VAR / ${VAR}
//     expansion. **No** escape sequences (operators who need those are
//     using the wrong tool).
//   - Whitespace around an unquoted value is trimmed. To preserve
//     leading or trailing whitespace, wrap the value in quotes.
func readDotEnv(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("dotenv read %s: %w", path, err)
	}
	defer f.Close()

	out := map[string]string{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimRight(sc.Text(), "\r")
		trimmed := strings.TrimLeft(line, " \t")
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "export ") {
			trimmed = strings.TrimPrefix(trimmed, "export ")
			trimmed = strings.TrimLeft(trimmed, " \t")
		}
		eq := strings.IndexByte(trimmed, '=')
		if eq <= 0 {
			return nil, fmt.Errorf("dotenv %s:%d: missing '=' in %q", path, lineNo, line)
		}
		key := strings.TrimSpace(trimmed[:eq])
		val := trimmed[eq+1:]
		switch {
		case len(val) >= 2 && val[0] == '"' && val[len(val)-1] == '"':
			val = val[1 : len(val)-1]
		case len(val) >= 2 && val[0] == '\'' && val[len(val)-1] == '\'':
			val = val[1 : len(val)-1]
		default:
			// Unquoted: trim surrounding whitespace (standard dotenv).
			val = strings.TrimSpace(val)
		}
		out[key] = val
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("dotenv scan %s: %w", path, err)
	}
	return out, nil
}

// redactLiteral returns a placeholder used in audit logs for literal
// values. We log the *fact* of a literal resolve (so an operator can
// notice unexpected activity) without ever logging the literal itself —
// even short, low-entropy literals can be sensitive (think "BUDGET_USD=").
func redactLiteral(_ string) string { return "<literal>" }
