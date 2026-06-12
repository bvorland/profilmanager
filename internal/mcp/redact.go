package mcp

import (
	"bytes"
	"sort"
	"strings"
)

// redactedMarker is what replaces every occurrence of a known secret
// value in tool output / audit args. Constant on purpose: agents can
// detect it, log scrapers can grep for it.
const redactedMarker = "<REDACTED>"

// minRedactLen is the lower bound below which we refuse to redact even
// if the caller supplies a "secret" value that short. A 1-char secret
// would erase every occurrence of that character in stdout — useless and
// destructive. 4 is the smallest value that still catches typical short
// tokens (PIN-ish) without disfiguring normal text.
const minRedactLen = 4

// Redactor scans byte streams and replaces every occurrence of a
// pre-registered secret value with [redactedMarker].
//
// # Why a separate type?
//
// The MCP iron rule says resolved secret values never cross the
// protocol boundary. [Exec] resolves them into the child process env
// and then needs to scan the child's stdout/stderr before returning the
// captured bytes to the agent. The Redactor is the one place the
// resolved values live (briefly) in the parent process — it owns them,
// performs the scan, and zeros them when [Redactor.Reset] is called.
//
// # Algorithm
//
// On every input byte stream we replace each registered value with the
// marker. Replacement is done longest-first so a short secret that is a
// substring of a longer secret does not pre-empt it. This is O(n*k)
// where n is the input size and k is the secret count — fine for our
// expected inputs (≤ 1 MiB output, ≤ a dozen secrets per profile).
type Redactor struct {
	// values holds the secret byte sequences to scrub. We store as
	// []byte (not string) so we can zero them after use. Order does NOT
	// matter for storage; Redact() sorts a working copy longest-first.
	values [][]byte
}

// NewRedactor returns an empty Redactor. Call [Redactor.Add] for each
// resolved secret, then [Redactor.Redact] on the streams to scrub, then
// [Redactor.Reset] to zero the held values.
func NewRedactor() *Redactor {
	return &Redactor{}
}

// Add registers a value to scrub. Empty values and values shorter than
// [minRedactLen] are silently ignored — see the minRedactLen comment.
//
// The caller MUST NOT mutate v after this call; Redactor owns the slice
// until [Redactor.Reset]. Pass a freshly-allocated copy if v is shared.
func (r *Redactor) Add(v []byte) {
	if len(v) < minRedactLen {
		return
	}
	r.values = append(r.values, v)
}

// AddString is the string convenience form. Allocates a backing slice
// copy of s (Go strings are immutable, so we cannot zero them later) —
// prefer [Redactor.Add] when callers already have a []byte.
func (r *Redactor) AddString(s string) {
	if len(s) < minRedactLen {
		return
	}
	b := make([]byte, len(s))
	copy(b, s)
	r.values = append(r.values, b)
}

// Reset zeros every held value and drops the references. Idempotent.
// Safe to call on a nil receiver (no-op) so deferred Reset is always
// safe.
func (r *Redactor) Reset() {
	if r == nil {
		return
	}
	for i, v := range r.values {
		for j := range v {
			v[j] = 0
		}
		r.values[i] = nil
	}
	r.values = nil
}

// Len reports the number of registered values. Useful for tests and
// log lines like "scrubbed N secrets from output".
func (r *Redactor) Len() int {
	if r == nil {
		return 0
	}
	return len(r.values)
}

// Redact returns a copy of in with every registered value replaced by
// [redactedMarker]. The input slice is not modified.
//
// nil/empty Redactor is a pass-through (returns the input verbatim).
func (r *Redactor) Redact(in []byte) []byte {
	if r == nil || len(r.values) == 0 || len(in) == 0 {
		return in
	}
	// Sort a working copy longest-first so a short secret that is a
	// substring of a longer one does not pre-empt the longer match.
	sorted := make([][]byte, len(r.values))
	copy(sorted, r.values)
	sort.SliceStable(sorted, func(i, j int) bool {
		return len(sorted[i]) > len(sorted[j])
	})

	out := in
	for _, v := range sorted {
		if len(v) == 0 {
			continue
		}
		if !bytes.Contains(out, v) {
			continue
		}
		out = bytes.ReplaceAll(out, v, []byte(redactedMarker))
	}
	return out
}

// RedactString is the string convenience form of [Redactor.Redact].
func (r *Redactor) RedactString(in string) string {
	if r == nil || len(r.values) == 0 || in == "" {
		return in
	}
	out := []byte(in)
	out = r.Redact(out)
	return string(out)
}

// RedactArgs scrubs each element of args by exact-equality and by
// substring match. We accept the cost of the substring pass because
// agents sometimes embed a token into a longer argument
// ("--header=Authorization: Bearer xyz"), and stripping just the
// "Bearer xyz" suffix is more useful than dropping the whole arg.
func (r *Redactor) RedactArgs(args []string) []string {
	if r == nil || len(r.values) == 0 || len(args) == 0 {
		return args
	}
	out := make([]string, len(args))
	for i, a := range args {
		// First: exact-match short-circuit so a bare secret arg is
		// replaced by the marker instead of `<REDACTED>` substring
		// substitutions (cosmetic — the result is identical when a
		// matches v exactly).
		replaced := false
		for _, v := range r.values {
			if len(v) > 0 && a == string(v) {
				out[i] = redactedMarker
				replaced = true
				break
			}
		}
		if !replaced {
			out[i] = r.RedactString(a)
		}
	}
	return out
}

// firstNRedacted returns up to n bytes from in with redactor scrubbing
// applied. Useful for log previews where we want a bounded snippet and
// don't care about preserving the rest.
func firstNRedacted(r *Redactor, in []byte, n int) string {
	if n <= 0 || len(in) == 0 {
		return ""
	}
	cut := in
	if len(cut) > n {
		cut = cut[:n]
	}
	if r != nil {
		cut = r.Redact(cut)
	}
	// Replace control characters that would corrupt a JSON log line with
	// spaces. The audit logger uses json.Marshal which escapes these
	// anyway, but a multi-line snippet in an editor renders cleanly.
	return strings.Map(func(rn rune) rune {
		if rn == '\n' || rn == '\r' || rn == '\t' {
			return ' '
		}
		return rn
	}, string(cut))
}
