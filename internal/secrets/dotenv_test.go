package secrets

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDotEnvLiteralPassthrough(t *testing.T) {
	t.Parallel()
	SetAuditDir(t.TempDir())
	t.Cleanup(func() { SetAuditDir("") })

	r := NewDotEnvResolver()
	if !r.Available() {
		t.Fatal("dotenv should always be Available()")
	}
	s, err := r.Resolve(context.Background(), "plain-text-value")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	defer s.Zero()
	if got := s.Reveal(); got != "plain-text-value" {
		t.Fatalf("Reveal: %q", got)
	}
}

func TestDotEnvDescribeLiteralOmitsValue(t *testing.T) {
	t.Parallel()
	r := NewDotEnvResolver()
	md, err := r.Describe(context.Background(), "super-secret-literal-value")
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if strings.Contains(md.Ref, "super-secret-literal-value") {
		t.Fatalf("Describe leaked literal into Ref: %+v", md)
	}
	if md.Scheme != "dotenv" || !md.Exists {
		t.Fatalf("Describe metadata: %+v", md)
	}
}

func TestDotEnvFileResolve(t *testing.T) {
	t.Parallel()
	SetAuditDir(t.TempDir())
	t.Cleanup(func() { SetAuditDir("") })

	dir := t.TempDir()
	envPath := filepath.Join(dir, "test.env")
	content := strings.Join([]string{
		"# a comment",
		"",
		"  # indented comment",
		"PLAIN=value1",
		"  WITH_SPACES = value2  ",
		`QUOTED_DOUBLE="hello world"`,
		`QUOTED_SINGLE='hello again'`,
		`QUOTED_PADDED="  pad  "`,
		"export EXPORTED=exp-value",
		"EMBED=op://Vault/Item/field",
	}, "\n")
	if err := os.WriteFile(envPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	r := NewDotEnvResolver()
	cases := map[string]string{
		"PLAIN":         "value1",
		"WITH_SPACES":   "value2",
		"QUOTED_DOUBLE": "hello world",
		"QUOTED_SINGLE": "hello again",
		"QUOTED_PADDED": "  pad  ",
		"EXPORTED":      "exp-value",
		"EMBED":         "op://Vault/Item/field",
	}
	for key, want := range cases {
		t.Run(key, func(t *testing.T) {
			ref := "dotenv://" + filepath.ToSlash(envPath) + "#" + key
			s, err := r.Resolve(context.Background(), ref)
			if err != nil {
				t.Fatalf("Resolve %s: %v", ref, err)
			}
			defer s.Zero()
			if got := s.Reveal(); got != want {
				t.Fatalf("%s: got %q want %q", key, got, want)
			}
		})
	}
}

func TestDotEnvFileMissingKey(t *testing.T) {
	t.Parallel()
	SetAuditDir(t.TempDir())
	t.Cleanup(func() { SetAuditDir("") })

	dir := t.TempDir()
	envPath := filepath.Join(dir, "x.env")
	if err := os.WriteFile(envPath, []byte("ONLY=here\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := NewDotEnvResolver()
	_, err := r.Resolve(context.Background(), "dotenv://"+filepath.ToSlash(envPath)+"#MISSING")
	if err == nil {
		t.Fatal("expected error for missing key")
	}
	if !strings.Contains(err.Error(), "not present") {
		t.Fatalf("error message: %v", err)
	}
}

func TestDotEnvFileMissingFile(t *testing.T) {
	t.Parallel()
	SetAuditDir(t.TempDir())
	t.Cleanup(func() { SetAuditDir("") })

	r := NewDotEnvResolver()
	_, err := r.Resolve(context.Background(), "dotenv://"+filepath.ToSlash(filepath.Join(t.TempDir(), "missing.env"))+"#KEY")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestDotEnvDescribeFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	envPath := filepath.Join(dir, "x.env")
	if err := os.WriteFile(envPath, []byte("KEY=value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := NewDotEnvResolver()

	ref := "dotenv://" + filepath.ToSlash(envPath) + "#KEY"
	md, err := r.Describe(context.Background(), ref)
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if !md.Exists || md.Field != "KEY" {
		t.Fatalf("Describe: %+v", md)
	}
	// Defence: Describe MUST NOT echo the value.
	if strings.Contains(md.Ref, "value") || strings.Contains(md.Error, "value") {
		t.Fatalf("Describe leaked value: %+v", md)
	}

	mdMiss, err := r.Describe(context.Background(), "dotenv://"+filepath.ToSlash(envPath)+"#MISSING")
	if err != nil {
		t.Fatalf("Describe miss: %v", err)
	}
	if mdMiss.Exists {
		t.Fatalf("missing key should report Exists=false: %+v", mdMiss)
	}
}

func TestParseDotEnvRef(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in        string
		path, key string
		wantErr   bool
	}{
		{"dotenv:///tmp/a.env#K", "/tmp/a.env", "K", false},
		{"dotenv://C:/Users/x/.env#GITHUB_TOKEN", "C:/Users/x/.env", "GITHUB_TOKEN", false},
		{"dotenv://#K", "", "", true},
		{"dotenv:///tmp/a.env#", "", "", true},
		{"dotenv:///tmp/a.env", "", "", true},
		{"op:///tmp/a.env#K", "", "", true},
	}
	for _, tc := range cases {
		path, key, err := parseDotEnvRef(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("parseDotEnvRef(%q) err=%v wantErr=%v", tc.in, err, tc.wantErr)
			continue
		}
		if !tc.wantErr {
			if path != tc.path || key != tc.key {
				t.Errorf("parseDotEnvRef(%q) = (%q,%q), want (%q,%q)", tc.in, path, key, tc.path, tc.key)
			}
		}
	}
}

func TestReadDotEnvMalformedLine(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	envPath := filepath.Join(dir, "bad.env")
	if err := os.WriteFile(envPath, []byte("no-equals-sign-here\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readDotEnv(envPath); err == nil {
		t.Fatal("expected error on malformed line")
	}
}
