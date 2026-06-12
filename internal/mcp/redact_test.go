package mcp

import (
	"bytes"
	"strings"
	"testing"
)

// TestRedactor_NoSecretsIsPassthrough verifies that an empty redactor
// returns the input verbatim — including bit-identical bytes for
// callers that depend on the slice identity (we don't, but copying
// would be wasted work).
func TestRedactor_NoSecretsIsPassthrough(t *testing.T) {
	r := NewRedactor()
	in := []byte("hello world")
	out := r.Redact(in)
	if !bytes.Equal(in, out) {
		t.Fatalf("empty redactor altered input: got %q want %q", out, in)
	}
	if r.Len() != 0 {
		t.Fatalf("empty redactor.Len = %d, want 0", r.Len())
	}
}

// TestRedactor_ReplacesValue is the headline behavior: a registered
// secret value is replaced with the marker in the output stream.
func TestRedactor_ReplacesValue(t *testing.T) {
	r := NewRedactor()
	r.AddString("hunter2pass")
	in := []byte("logging in with hunter2pass and then doing stuff")
	out := r.Redact(in)
	want := "logging in with " + redactedMarker + " and then doing stuff"
	if string(out) != want {
		t.Fatalf("redact mismatch\n got: %q\nwant: %q", out, want)
	}
}

// TestRedactor_RejectsTooShort guards against the "secret is one
// character" footgun — we'd erase every occurrence of that character
// in output, which is both useless and visually destructive.
func TestRedactor_RejectsTooShort(t *testing.T) {
	r := NewRedactor()
	r.AddString("ab")    // 2 chars — below minRedactLen
	r.AddString("abcd")  // 4 chars — exactly minRedactLen, accepted
	r.AddString("abcde") // 5 chars — accepted
	if r.Len() != 2 {
		t.Fatalf("redactor.Len = %d, want 2 (rejected the 2-char value)", r.Len())
	}
}

// TestRedactor_LongestFirst ensures that when two registered values
// overlap, the longer one wins (otherwise the shorter prefix would
// match first and leave dangling characters in the output).
func TestRedactor_LongestFirst(t *testing.T) {
	r := NewRedactor()
	r.AddString("token")        // short prefix
	r.AddString("token-secret") // longer value containing the short one
	in := []byte("Authorization: Bearer token-secret here")
	out := r.RedactString(string(in))
	if strings.Contains(out, "token-secret") {
		t.Fatalf("longer secret not redacted: %s", out)
	}
	if !strings.Contains(out, redactedMarker) {
		t.Fatalf("marker missing: %s", out)
	}
	// And the short "token" shouldn't have eaten part of the longer
	// value first leaving a stray "-secret":
	if strings.Contains(out, "-secret") {
		t.Fatalf("longer match was pre-empted by shorter prefix: %s", out)
	}
}

// TestRedactor_RedactArgsExactAndSubstring covers both the exact-match
// path (a bare secret as one arg) and the substring path (a secret
// embedded in a longer flag value).
func TestRedactor_RedactArgsExactAndSubstring(t *testing.T) {
	r := NewRedactor()
	r.AddString("s3cr3tValue")
	args := []string{
		"s3cr3tValue",                 // exact
		"--header=Bearer s3cr3tValue", // substring
		"--unrelated",
	}
	out := r.RedactArgs(args)
	if out[0] != redactedMarker {
		t.Errorf("exact-match arg not replaced with marker: %q", out[0])
	}
	if !strings.Contains(out[1], redactedMarker) {
		t.Errorf("substring arg not scrubbed: %q", out[1])
	}
	if strings.Contains(out[1], "s3cr3tValue") {
		t.Errorf("substring still contains secret: %q", out[1])
	}
	if out[2] != "--unrelated" {
		t.Errorf("unrelated arg mutated: %q", out[2])
	}
}

// TestRedactor_ResetZerosValues confirms that Reset overwrites the
// backing byte slices (greppable: the secret bytes are gone from
// memory at the address we held).
func TestRedactor_ResetZerosValues(t *testing.T) {
	r := NewRedactor()
	original := []byte("zero-me-please")
	// Keep an alias to the slice so we can inspect after Reset.
	alias := original
	r.Add(original)
	r.Reset()
	for i, b := range alias {
		if b != 0 {
			t.Fatalf("alias[%d] = %d, want 0 (Reset failed to wipe)", i, b)
		}
	}
	if r.Len() != 0 {
		t.Fatalf("Len after Reset = %d, want 0", r.Len())
	}
}

// TestRedactor_NilSafe confirms that calls on a nil *Redactor are
// no-ops — this lets call sites use a nil sentinel when no redaction
// is needed without a separate code path.
func TestRedactor_NilSafe(t *testing.T) {
	var r *Redactor
	if got := r.Redact([]byte("hi")); string(got) != "hi" {
		t.Errorf("nil.Redact = %q, want %q", got, "hi")
	}
	if got := r.RedactString("hi"); got != "hi" {
		t.Errorf("nil.RedactString = %q, want %q", got, "hi")
	}
	if got := r.RedactArgs([]string{"a"}); got[0] != "a" {
		t.Errorf("nil.RedactArgs = %v, want [a]", got)
	}
	r.Reset() // must not panic
}

// TestFirstNRedacted verifies the audit-log preview helper: capped to
// n bytes, redacted, and control chars replaced with spaces so a JSON
// log line stays single-line-readable.
func TestFirstNRedacted(t *testing.T) {
	r := NewRedactor()
	r.AddString("supersecret")
	in := []byte("hello\nsupersecret\tworld" + strings.Repeat("X", 1024))
	got := firstNRedacted(r, in, 64)
	if len(got) > 64+len("<REDACTED>") { // marker may extend a line slightly
		t.Errorf("preview too long: %d bytes", len(got))
	}
	if strings.Contains(got, "supersecret") {
		t.Errorf("preview contains secret: %q", got)
	}
	if strings.Contains(got, "\n") || strings.Contains(got, "\t") {
		t.Errorf("preview has unconverted control chars: %q", got)
	}
}
