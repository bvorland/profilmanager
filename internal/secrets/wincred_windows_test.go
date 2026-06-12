//go:build windows

package secrets

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"runtime"
	"strings"
	"testing"

	"github.com/danieljoos/wincred"
)

// randomTarget returns "pm-test-<16 hex>" — a target name that won't
// collide with any real credential on the developer's machine.
func randomTarget(t *testing.T) string {
	t.Helper()
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	return "pm-test-" + hex.EncodeToString(b[:])
}

func TestWinCredRoundTrip(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("wincred only on Windows")
	}
	SetAuditDir(t.TempDir())
	t.Cleanup(func() { SetAuditDir("") })

	target := randomTarget(t)
	const password = "p@55w0rd-canary"
	const username = "tester"

	cred := wincred.NewGenericCredential(target)
	cred.CredentialBlob = []byte(password)
	cred.UserName = username
	if err := cred.Write(); err != nil {
		t.Fatalf("write credential: %v", err)
	}
	t.Cleanup(func() {
		c, err := wincred.GetGenericCredential(target)
		if err == nil {
			_ = c.Delete()
		}
	})

	r := NewWinCredResolver()
	if !r.Available() {
		t.Fatal("wincred should be Available on Windows")
	}

	// Resolve by target only.
	s, err := r.Resolve(context.Background(), "wincred://"+target)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got := s.Reveal(); got != password {
		t.Fatalf("Reveal: got %q want %q", got, password)
	}
	s.Zero()

	// Resolve by target/username.
	s2, err := r.Resolve(context.Background(), "wincred://"+target+"/"+username)
	if err != nil {
		t.Fatalf("Resolve target+user: %v", err)
	}
	if s2.Reveal() != password {
		t.Fatalf("Reveal: got %q want %q", s2.Reveal(), password)
	}
	s2.Zero()

	// Username mismatch is a miss.
	_, err = r.Resolve(context.Background(), "wincred://"+target+"/wrong-user")
	if err == nil || !strings.Contains(err.Error(), "username") {
		t.Fatalf("expected username mismatch error, got %v", err)
	}
}

func TestWinCredDescribeDoesNotLeakPassword(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("wincred only on Windows")
	}
	target := randomTarget(t)
	const password = "DESCRIBE-CANARY-PASSWORD"
	cred := wincred.NewGenericCredential(target)
	cred.CredentialBlob = []byte(password)
	cred.UserName = "describe-tester"
	if err := cred.Write(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c, err := wincred.GetGenericCredential(target)
		if err == nil {
			_ = c.Delete()
		}
	})

	r := NewWinCredResolver()
	md, err := r.Describe(context.Background(), "wincred://"+target)
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if !md.Exists {
		t.Fatalf("Describe: Exists false: %+v", md)
	}
	// The metadata struct, when stringified, must NOT contain the password.
	for _, field := range []string{md.Ref, md.Vault, md.Item, md.Field, md.Error} {
		if strings.Contains(field, password) {
			t.Fatalf("Describe leaked password into %q", field)
		}
	}
}

func TestWinCredMissingTarget(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("wincred only on Windows")
	}
	SetAuditDir(t.TempDir())
	t.Cleanup(func() { SetAuditDir("") })

	r := NewWinCredResolver()
	_, err := r.Resolve(context.Background(), "wincred://"+randomTarget(t))
	if err == nil {
		t.Fatal("expected error for missing target")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("error message: %v", err)
	}
}

func TestParseWinCredRef(t *testing.T) {
	cases := []struct {
		in           string
		target, user string
		wantErr      bool
	}{
		{"wincred://Target", "Target", "", false},
		{"wincred://Target/User", "Target", "User", false},
		{"wincred://Some/Long/Path", "Some", "Long/Path", false},
		{"wincred://", "", "", true},
		{"op://x", "", "", true},
	}
	for _, tc := range cases {
		target, user, err := parseWinCredRef(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("parseWinCredRef(%q) err=%v wantErr=%v", tc.in, err, tc.wantErr)
		}
		if !tc.wantErr && (target != tc.target || user != tc.user) {
			t.Errorf("parseWinCredRef(%q) = (%q,%q), want (%q,%q)", tc.in, target, user, tc.target, tc.user)
		}
	}
}
