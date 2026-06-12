package secrets

// Secret holds a resolved secret value with an explicit,
// grep-friendly accessor.
//
// # Why an opaque type?
//
// Returning `string` from a resolver would let secrets fall into any
// `fmt.Sprintf("%v", ...)` or structured log call by accident. By
// returning [Secret], callers must call [Secret.Reveal] to read the
// plaintext — and "Reveal(" is greppable, so a reviewer (Rai) can audit
// every leak surface in the repo in one query.
//
// # Why []byte under the hood?
//
// `string` in Go is immutable; once a secret crosses into a `string` we
// cannot zero it. The internal store is a byte slice so [Secret.Zero]
// can overwrite the bytes when the caller is done.
type Secret struct {
	// value holds the plaintext bytes. Unexported so callers cannot read
	// it without going through [Reveal]; we want every read site to be
	// greppable.
	value []byte
}

// NewSecret takes ownership of v. Callers MUST NOT reuse the slice after
// this call — Secret.Zero will overwrite it.
func NewSecret(v []byte) Secret {
	return Secret{value: v}
}

// NewSecretString copies s into a fresh byte slice and returns a Secret
// over it. The input string remains in memory (Go strings are
// immutable) — prefer NewSecret with []byte where possible.
func NewSecretString(s string) Secret {
	if s == "" {
		return Secret{}
	}
	b := make([]byte, len(s))
	copy(b, s)
	return Secret{value: b}
}

// Reveal returns the plaintext value as a string. This is the ONE place
// in the codebase that materialises the secret as a Go string; reviewers
// should grep for ".Reveal(" to enumerate every leak surface.
//
// The returned string lives as long as Go's runtime decides. Callers
// SHOULD pass it directly to whatever needs it (e.g., set on a child
// process env) and then call [Secret.Zero].
func (s Secret) Reveal() string {
	if len(s.value) == 0 {
		return ""
	}
	return string(s.value)
}

// RevealBytes returns the underlying byte slice without copying. The
// returned slice aliases the secret's storage — callers MUST NOT retain
// it past [Secret.Zero], and MUST NOT mutate it.
func (s Secret) RevealBytes() []byte {
	return s.value
}

// Len reports the byte length of the secret without revealing the value.
// Useful for logs ("resolved 24-byte secret") without leaking content.
func (s Secret) Len() int { return len(s.value) }

// Zero overwrites the backing byte slice with zeroes, then drops the
// reference. Idempotent. Safe to call on the zero Secret.
//
// Note: this is best-effort memory hygiene, not a security guarantee.
// The Go runtime may have already copied the value (escape analysis,
// stack copies, GC). It still raises the bar — a heap dump taken after
// Zero will not contain the plaintext at the original address.
func (s *Secret) Zero() {
	if s == nil {
		return
	}
	for i := range s.value {
		s.value[i] = 0
	}
	s.value = nil
}

// String deliberately does NOT reveal the value — accidental
// `fmt.Println(secret)` or `log.Printf("%v", secret)` produces a redacted
// marker, not the plaintext. Length is included so a debugger can tell
// "empty vs not" without seeing the bytes.
func (s Secret) String() string {
	if len(s.value) == 0 {
		return "secrets.Secret{empty}"
	}
	return "secrets.Secret{REDACTED}"
}

// GoString matches String — keeps `%#v` from leaking the value either.
func (s Secret) GoString() string { return s.String() }

// MarshalJSON refuses to serialise a resolved value. Any attempt to
// json-encode a Secret returns an error rather than silently emitting
// `null` or `{}` — we want the test suite (and reviewers) to notice.
func (s Secret) MarshalJSON() ([]byte, error) {
	return nil, errSecretMarshal
}

// MarshalText mirrors MarshalJSON for `encoding/text` consumers.
func (s Secret) MarshalText() ([]byte, error) {
	return nil, errSecretMarshal
}

type secretMarshalError struct{}

func (secretMarshalError) Error() string {
	return "secrets.Secret: refusing to marshal resolved value (use Reveal() if you intentionally need the plaintext)"
}

var errSecretMarshal error = secretMarshalError{}
