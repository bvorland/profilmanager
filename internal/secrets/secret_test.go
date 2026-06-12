package secrets

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
)

func TestSecretZeroClearsValue(t *testing.T) {
	t.Parallel()
	s := NewSecretString("super-secret-token")
	if s.Reveal() != "super-secret-token" {
		t.Fatalf("Reveal before Zero: got %q", s.Reveal())
	}
	if s.Len() != len("super-secret-token") {
		t.Fatalf("Len before Zero: got %d", s.Len())
	}
	s.Zero()
	if got := s.Reveal(); got != "" {
		t.Fatalf("Reveal after Zero: want empty, got %q", got)
	}
	if got := s.Len(); got != 0 {
		t.Fatalf("Len after Zero: want 0, got %d", got)
	}
	// Idempotent.
	s.Zero()
}

func TestSecretZeroWipesUnderlyingBytes(t *testing.T) {
	t.Parallel()
	plaintext := []byte("hunter2")
	original := make([]byte, len(plaintext))
	copy(original, plaintext)
	s := NewSecret(plaintext)
	// After Zero, plaintext (the backing array) should be all zeroes
	// — even though we still hold a reference to it from the test.
	s.Zero()
	for i, b := range plaintext {
		if b != 0 {
			t.Fatalf("byte %d not zeroed: %x", i, b)
		}
	}
	// And of course the original copy is untouched (the test owns it).
	if !bytes.Equal(original, []byte("hunter2")) {
		t.Fatalf("test-owned copy was clobbered")
	}
}

func TestSecretStringIsRedacted(t *testing.T) {
	t.Parallel()
	s := NewSecretString("AKIA-LEAKY-1234")
	for _, fmtStr := range []string{"%s", "%v", "%#v", "%q"} {
		out := fmt.Sprintf(fmtStr, s)
		if strings.Contains(out, "AKIA-LEAKY-1234") {
			t.Fatalf("format %q leaked secret: %q", fmtStr, out)
		}
		if !strings.Contains(out, "REDACTED") && !strings.Contains(out, "empty") {
			t.Fatalf("format %q did not produce a redaction marker: %q", fmtStr, out)
		}
	}
	emptyS := NewSecretString("")
	out := fmt.Sprintf("%v", emptyS)
	if !strings.Contains(out, "empty") {
		t.Fatalf("empty secret format: %q", out)
	}
}

func TestSecretJSONRefusesToMarshal(t *testing.T) {
	t.Parallel()
	s := NewSecretString("xyzzy")
	b, err := json.Marshal(s)
	if err == nil {
		t.Fatalf("json.Marshal(Secret) should error, got %q", b)
	}
	if !strings.Contains(err.Error(), "refusing") {
		t.Fatalf("error should explain refusal, got %v", err)
	}
	// And via a wrapping struct — make sure the marshal error bubbles up.
	wrap := struct {
		Token Secret `json:"token"`
	}{Token: s}
	if _, err := json.Marshal(wrap); err == nil {
		t.Fatal("marshaling a struct containing a Secret should error")
	}
}

// TestMetadataIsMetadataOnly is the defence-in-depth assertion:
// marshaling Metadata for every backend's Describe path produces JSON
// that does NOT contain the known-secret canaries we'd pass to a fake.
// If a future field accidentally leaks the value, this test fails.
func TestMetadataIsMetadataOnly(t *testing.T) {
	t.Parallel()
	const canary = "CANARY-VALUE-DO-NOT-LEAK-DEADBEEF"
	md := Metadata{
		Scheme:  "op",
		Backend: "op",
		Ref:     "op://Personal/GitHub Token/credential",
		Vault:   "Personal",
		Item:    "GitHub Token",
		Field:   "credential",
		Exists:  true,
	}
	b, err := json.Marshal(md)
	if err != nil {
		t.Fatalf("marshal Metadata: %v", err)
	}
	if bytes.Contains(b, []byte(canary)) {
		t.Fatalf("metadata JSON contains canary: %s", b)
	}
	// Make sure there's no field name suggesting a value is being emitted.
	leakyNames := regexp.MustCompile(`(?i)"(value|secret|password|token|credential_value|plaintext)"\s*:`)
	if leakyNames.MatchString(string(b)) {
		t.Fatalf("metadata JSON has a suspicious field name: %s", b)
	}
}
