package secrets

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// withFakeOpOnPath drops a tiny `op.cmd` (Windows) or `op` (Unix) script
// into a temp dir and prepends that dir to PATH for the duration of the
// test. The script implements the smallest viable `op` surface for our
// resolver: whoami, read, item get.
func withFakeOpOnPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	var (
		name string
		body string
	)
	if runtime.GOOS == "windows" {
		name = "op.cmd"
		body = `@echo off
if "%~1"=="whoami" (
  echo {"user_uuid":"fake","account_uuid":"fake"}
  exit /b 0
)
if "%~1"=="read" (
  if "%~2"=="op://Personal/GitHub Token/credential" (
    echo ghp_FAKE_CANARY_VALUE
    exit /b 0
  )
  echo "isn't an item" 1>&2
  exit /b 1
)
if "%~1"=="item" (
  if "%~2"=="get" (
    if "%~3"=="GitHub Token" (
      echo {"id":"x","title":"GitHub Token","fields":[{"id":"credential","label":"credential"}]}
      exit /b 0
    )
    echo "not found" 1>&2
    exit /b 1
  )
)
echo unknown args: %* 1>&2
exit /b 2
`
	} else {
		name = "op"
		body = `#!/bin/sh
case "$1" in
  whoami)
    echo '{"user_uuid":"fake","account_uuid":"fake"}'
    exit 0
    ;;
  read)
    if [ "$2" = "op://Personal/GitHub Token/credential" ]; then
      printf 'ghp_FAKE_CANARY_VALUE\n'
      exit 0
    fi
    echo "isn't an item" 1>&2
    exit 1
    ;;
  item)
    if [ "$2" = "get" ] && [ "$3" = "GitHub Token" ]; then
      echo '{"id":"x","title":"GitHub Token","fields":[{"id":"credential","label":"credential"}]}'
      exit 0
    fi
    echo "not found" 1>&2
    exit 1
    ;;
esac
echo "unknown args: $*" 1>&2
exit 2
`
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	origPath := os.Getenv("PATH")
	t.Cleanup(func() { _ = os.Setenv("PATH", origPath) })
	_ = os.Setenv("PATH", dir+string(os.PathListSeparator)+origPath)
	return dir
}

// withFailingFakeOp drops an `op` that always exits 1.
func withFailingFakeOp(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	var name, body string
	if runtime.GOOS == "windows" {
		name = "op.cmd"
		body = "@echo off\r\necho not signed in 1>&2\r\nexit /b 1\r\n"
	} else {
		name = "op"
		body = "#!/bin/sh\necho 'not signed in' 1>&2\nexit 1\n"
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	origPath := os.Getenv("PATH")
	t.Cleanup(func() { _ = os.Setenv("PATH", origPath) })
	_ = os.Setenv("PATH", dir+string(os.PathListSeparator)+origPath)
}

func TestOpResolverAvailable(t *testing.T) {
	withFakeOpOnPath(t)
	r := NewOpResolver()
	if !r.Available() {
		t.Fatal("Available should be true when fake op whoami succeeds")
	}
}

func TestOpResolverNotAvailableWhenWhoamiFails(t *testing.T) {
	withFailingFakeOp(t)
	r := NewOpResolver()
	if r.Available() {
		t.Fatal("Available should be false when whoami fails")
	}
}

func TestOpResolverResolveSuccess(t *testing.T) {
	withFakeOpOnPath(t)
	SetAuditDir(t.TempDir())
	t.Cleanup(func() { SetAuditDir("") })

	r := NewOpResolver()
	s, err := r.Resolve(context.Background(), "op://Personal/GitHub Token/credential")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	defer s.Zero()
	if got := s.Reveal(); got != "ghp_FAKE_CANARY_VALUE" {
		t.Fatalf("Reveal: %q", got)
	}
}

func TestOpResolverResolveMiss(t *testing.T) {
	withFakeOpOnPath(t)
	SetAuditDir(t.TempDir())
	t.Cleanup(func() { SetAuditDir("") })

	r := NewOpResolver()
	_, err := r.Resolve(context.Background(), "op://Personal/Nope/field")
	if err == nil {
		t.Fatal("expected miss error")
	}
	// The audit log should have a single "miss" entry, not "error".
	entries := auditEntries(t)
	if len(entries) != 1 {
		t.Fatalf("audit entries: %+v", entries)
	}
	if entries[0].Result != AuditMiss {
		t.Fatalf("expected miss, got %q", entries[0].Result)
	}
}

func TestOpResolverRejectsBadRef(t *testing.T) {
	withFakeOpOnPath(t)
	SetAuditDir(t.TempDir())
	t.Cleanup(func() { SetAuditDir("") })

	r := NewOpResolver()
	_, err := r.Resolve(context.Background(), "not-an-op-ref")
	if err == nil || !strings.Contains(err.Error(), "op://") {
		t.Fatalf("expected scheme error, got %v", err)
	}
}

func TestOpResolverDescribeFound(t *testing.T) {
	withFakeOpOnPath(t)
	r := NewOpResolver()
	md, err := r.Describe(context.Background(), "op://Personal/GitHub Token/credential")
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if !md.Exists {
		t.Fatalf("Describe Exists false: %+v", md)
	}
	if md.Vault != "Personal" || md.Item != "GitHub Token" || md.Field != "credential" {
		t.Fatalf("Describe parsed wrong: %+v", md)
	}
}

func TestOpResolverDescribeMissingField(t *testing.T) {
	withFakeOpOnPath(t)
	r := NewOpResolver()
	md, err := r.Describe(context.Background(), "op://Personal/GitHub Token/nonexistent")
	if err == nil {
		t.Fatal("expected error for missing field")
	}
	if md.Exists {
		t.Fatalf("missing field reported Exists=true: %+v", md)
	}
}

func TestOpResolverDescribeBadRef(t *testing.T) {
	r := NewOpResolver()
	md, err := r.Describe(context.Background(), "op://only-one-segment")
	if err == nil {
		t.Fatal("expected error for malformed ref")
	}
	if md.Error == "" {
		t.Fatalf("Error not populated: %+v", md)
	}
}

func TestParseOpRef(t *testing.T) {
	cases := []struct {
		in                 string
		vault, item, field string
		wantErr            bool
	}{
		{"op://V/I/F", "V", "I", "F", false},
		{"op://Personal/GitHub Token/credential", "Personal", "GitHub Token", "credential", false},
		{"op://V/I", "", "", "", true},
		{"op://", "", "", "", true},
		{"vault://V/I/F", "", "", "", true},
	}
	for _, tc := range cases {
		v, i, f, err := parseOpRef(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("parseOpRef(%q) err=%v wantErr=%v", tc.in, err, tc.wantErr)
			continue
		}
		if !tc.wantErr && (v != tc.vault || i != tc.item || f != tc.field) {
			t.Errorf("parseOpRef(%q) = (%q,%q,%q), want (%q,%q,%q)", tc.in, v, i, f, tc.vault, tc.item, tc.field)
		}
	}
}

func TestOpResolverDescribeUnavailable(t *testing.T) {
	withFailingFakeOp(t)
	r := NewOpResolver()
	_, err := r.Describe(context.Background(), "op://V/I/F")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
}
