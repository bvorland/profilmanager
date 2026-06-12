package core

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestValidateName(t *testing.T) {
	t.Parallel()
	good := []string{"foo", "Foo.Bar-1", "a_b.c", "Contoso.MainDev"}
	bad := []string{"", "foo bar", "foo/bar", "..", "foo/../bar", "foo:bar", "foo\u00e9"}
	for _, n := range good {
		if err := ValidateName(n); err != nil {
			t.Errorf("ValidateName(%q) unexpected error: %v", n, err)
		}
	}
	for _, n := range bad {
		if err := ValidateName(n); err == nil {
			t.Errorf("ValidateName(%q) expected error, got nil", n)
		}
	}
}

func TestColorEmoji_AllKnownColors(t *testing.T) {
	t.Parallel()
	for _, color := range []string{
		"Cyan", "DarkCyan", "Blue", "DarkBlue",
		"Green", "DarkGreen", "Yellow", "DarkYellow",
		"Red", "DarkRed", "Magenta", "DarkMagenta",
		"White", "Gray", "DarkGray", "Black",
	} {
		if got := ColorEmoji(color); got == "" {
			t.Fatalf("ColorEmoji(%q) returned empty", color)
		}
	}
}

func TestColorEmoji_UnknownColor(t *testing.T) {
	t.Parallel()
	for _, color := range []string{"", "Purple", "Pink", "cyan"} {
		if got := ColorEmoji(color); got != "" {
			t.Fatalf("ColorEmoji(%q) = %q, want empty", color, got)
		}
	}
}

func TestColorHex_AllKnownColors(t *testing.T) {
	t.Parallel()
	for _, color := range []string{
		"Cyan", "DarkCyan", "Blue", "DarkBlue",
		"Green", "DarkGreen", "Yellow", "DarkYellow",
		"Red", "DarkRed", "Magenta", "DarkMagenta",
		"White", "Gray", "DarkGray", "Black",
	} {
		got := ColorHex(color)
		if got == "" {
			t.Fatalf("ColorHex(%q) returned empty", color)
		}
		if len(got) != 7 || got[0] != '#' {
			t.Fatalf("ColorHex(%q) = %q, want #RRGGBB", color, got)
		}
	}
}

func TestColorHex_UnknownColor(t *testing.T) {
	t.Parallel()
	for _, color := range []string{"", "Purple", "Pink", "cyan"} {
		if got := ColorHex(color); got != "" {
			t.Fatalf("ColorHex(%q) = %q, want empty", color, got)
		}
	}
}

func TestColorHex_StableForSameColor(t *testing.T) {
	t.Parallel()
	if a, b := ColorHex("Cyan"), ColorHex("Cyan"); a != b {
		t.Fatalf("ColorHex not stable: %q vs %q", a, b)
	}
}

func TestApplyColorEmojiPrefix_EmptyLabel(t *testing.T) {
	t.Parallel()
	if got := ApplyColorEmojiPrefix("", "Cyan"); got != "" {
		t.Fatalf("ApplyColorEmojiPrefix empty label = %q", got)
	}
}

func TestApplyColorEmojiPrefix_AddsEmoji(t *testing.T) {
	t.Parallel()
	if got := ApplyColorEmojiPrefix("Contoso demo", "Cyan"); got != "🔵 Contoso demo" {
		t.Fatalf("ApplyColorEmojiPrefix = %q", got)
	}
}

func TestApplyColorEmojiPrefix_AlreadyPrefixed_SameColor(t *testing.T) {
	t.Parallel()
	if got := ApplyColorEmojiPrefix("🔵 Foo", "Cyan"); got != "🔵 Foo" {
		t.Fatalf("ApplyColorEmojiPrefix = %q", got)
	}
}

func TestApplyColorEmojiPrefix_AlreadyPrefixed_DifferentEmoji(t *testing.T) {
	t.Parallel()
	if got := ApplyColorEmojiPrefix("🔴 Foo", "Cyan"); got != "🔴 Foo" {
		t.Fatalf("ApplyColorEmojiPrefix = %q", got)
	}
}

func TestApplyColorEmojiPrefix_UnknownColor(t *testing.T) {
	t.Parallel()
	if got := ApplyColorEmojiPrefix("Foo", "Purple"); got != "Foo" {
		t.Fatalf("ApplyColorEmojiPrefix = %q", got)
	}
}

func TestApplyColorEmojiPrefix_EmptyColor(t *testing.T) {
	t.Parallel()
	if got := ApplyColorEmojiPrefix("Foo", ""); got != "Foo" {
		t.Fatalf("ApplyColorEmojiPrefix = %q", got)
	}
}

func TestReplaceColorEmojiPrefix_SwapsKnownPrefix(t *testing.T) {
	t.Parallel()
	if got := ReplaceColorEmojiPrefix("🔷 Contoso Prod Pilot", "White"); got != "⚪ Contoso Prod Pilot" {
		t.Fatalf("ReplaceColorEmojiPrefix swap = %q", got)
	}
}

func TestReplaceColorEmojiPrefix_AddsPrefixWhenAbsent(t *testing.T) {
	t.Parallel()
	if got := ReplaceColorEmojiPrefix("Contoso Prod Pilot", "Cyan"); got != "🔵 Contoso Prod Pilot" {
		t.Fatalf("ReplaceColorEmojiPrefix add = %q", got)
	}
}

func TestReplaceColorEmojiPrefix_RemovesPrefixForUnknownColor(t *testing.T) {
	t.Parallel()
	if got := ReplaceColorEmojiPrefix("🔷 Foo", "Purple"); got != "Foo" {
		t.Fatalf("ReplaceColorEmojiPrefix strip = %q", got)
	}
}

func TestReplaceColorEmojiPrefix_RemovesPrefixForEmptyColor(t *testing.T) {
	t.Parallel()
	if got := ReplaceColorEmojiPrefix("🔷 Foo", ""); got != "Foo" {
		t.Fatalf("ReplaceColorEmojiPrefix strip on clear = %q", got)
	}
}

func TestReplaceColorEmojiPrefix_EmptyLabel(t *testing.T) {
	t.Parallel()
	if got := ReplaceColorEmojiPrefix("", "Cyan"); got != "" {
		t.Fatalf("ReplaceColorEmojiPrefix empty label = %q", got)
	}
}

func TestReplaceColorEmojiPrefix_BareGlyphLabel(t *testing.T) {
	t.Parallel()
	if got := ReplaceColorEmojiPrefix("🔷", "White"); got != "⚪" {
		t.Fatalf("ReplaceColorEmojiPrefix bare glyph = %q", got)
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadGood(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "good.toml")
	writeFile(t, path, `schema = "1"
name = "Contoso.MainDev"
label = "Contoso Main Dev"
color = "cyan"

[azure]
subscription = "sub-1"
tenant = "tenant-1"
config_dir = "~/.azure-Contoso.MainDev"

[gh]
user = "bvorland"
hosts = ["github.com"]

[[env]]
key = "FOO"
value = "bar"

[[env]]
key = "API_KEY"
ref = "op://Vault/Item/password"
`)
	p, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if p.Name != "Contoso.MainDev" {
		t.Errorf("name: got %q", p.Name)
	}
	if p.Azure == nil || p.Azure.SubscriptionID != "sub-1" {
		t.Errorf("azure: %+v", p.Azure)
	}
	if p.GitHub == nil || p.GitHub.Account != "bvorland" {
		t.Errorf("gh: %+v", p.GitHub)
	}
	if len(p.Env) != 2 || p.Env[0].Key != "FOO" || p.Env[1].Ref != "op://Vault/Item/password" {
		t.Errorf("env: %+v", p.Env)
	}
	if got := p.DisplayLabel(); got != "Contoso Main Dev" {
		t.Errorf("DisplayLabel: %q", got)
	}
}

func TestLoadBadSchema(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.toml")
	writeFile(t, path, `schema = "2"
name = "foo"
`)
	if _, err := Load(path); err == nil {
		t.Fatal("expected error for unsupported schema")
	} else if !strings.Contains(err.Error(), "schema") {
		t.Errorf("expected schema error, got: %v", err)
	}
}

func TestLoadBadName(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.toml")
	writeFile(t, path, `schema = "1"
name = "bad name"
`)
	if _, err := Load(path); err == nil {
		t.Fatal("expected error for invalid name")
	}
}

func TestLoadEnvMutex(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.toml")
	writeFile(t, path, `schema = "1"
name = "ok"

[[env]]
key = "FOO"
value = "bar"
ref = "op://X/Y/z"
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for env with both value and ref")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("expected mutex error, got: %v", err)
	}
}

func TestLoadEnvMissingBoth(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.toml")
	writeFile(t, path, `schema = "1"
name = "ok"

[[env]]
key = "FOO"
`)
	if _, err := Load(path); err == nil {
		t.Fatal("expected error for env missing both value and ref")
	}
}

func TestSaveRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "rt.toml")
	in := &Profile{
		Schema: SchemaVersion,
		Name:   "rt-1",
		Label:  "Round-Trip",
		Color:  "magenta",
		Azure:  &AzureProfile{SubscriptionID: "s", TenantID: "t", ConfigDir: "~/.azure-rt"},
		Azd:    &AzdProfile{ConfigDir: "~/.azd-rt", SubscriptionID: "s"},
		GitHub: &GitHubProfile{Account: "user", Hosts: []string{"github.com", "ghe.example"}},
		Kube:   &KubeProfile{Context: "ctx", Namespace: "ns"},
		Git:    &GitIdentity{UserName: "U", UserEmail: "u@example.com", SigningKey: "K"},
		Env: []EnvEntry{
			{Key: "A", Value: "1"},
			{Key: "B", Ref: "op://V/I/f"},
		},
	}
	if err := in.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	out, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if out.Name != in.Name || out.Label != in.Label || out.Color != in.Color {
		t.Errorf("top mismatch: %+v", out)
	}
	if out.Azure == nil || *out.Azure != *in.Azure {
		t.Errorf("azure mismatch: got %+v want %+v", out.Azure, in.Azure)
	}
	if out.GitHub == nil || out.GitHub.Account != "user" || len(out.GitHub.Hosts) != 2 {
		t.Errorf("gh mismatch: %+v", out.GitHub)
	}
	if len(out.Env) != 2 || out.Env[0].Value != "1" || out.Env[1].Ref != "op://V/I/f" {
		t.Errorf("env mismatch: %+v", out.Env)
	}
}

func TestSaveAtomicNoPartialOnInvalid(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "x.toml")
	bad := &Profile{Schema: "1", Name: "bad name"}
	if err := bad.Save(path); err == nil {
		t.Fatal("expected validation error")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected no file on validation failure, stat err=%v", err)
	}
}

func TestPathsHonorEnv(t *testing.T) {
	tmp := t.TempDir()
	switch runtime.GOOS {
	case "windows":
		t.Setenv("APPDATA", filepath.Join(tmp, "appdata"))
		t.Setenv("LOCALAPPDATA", filepath.Join(tmp, "localappdata"))
	default:
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "xdgcfg"))
		t.Setenv("XDG_STATE_HOME", filepath.Join(tmp, "xdgstate"))
		t.Setenv("HOME", filepath.Join(tmp, "home"))
	}

	pd, err := ProfilesDir()
	if err != nil {
		t.Fatalf("ProfilesDir: %v", err)
	}
	if !strings.HasPrefix(pd, tmp) {
		t.Errorf("ProfilesDir %q not under tmp %q", pd, tmp)
	}
	if !strings.HasSuffix(pd, filepath.Join(appDirName, "profiles")) {
		t.Errorf("ProfilesDir %q missing expected suffix", pd)
	}
	if fi, err := os.Stat(pd); err != nil || !fi.IsDir() {
		t.Errorf("ProfilesDir not created: stat=%v err=%v", fi, err)
	}

	sd, err := StateDir()
	if err != nil {
		t.Fatalf("StateDir: %v", err)
	}
	if !strings.HasPrefix(sd, tmp) {
		t.Errorf("StateDir %q not under tmp %q", sd, tmp)
	}

	pp, err := ProfilePath("Contoso.MainDev")
	if err != nil {
		t.Fatalf("ProfilePath: %v", err)
	}
	if filepath.Dir(pp) != pd {
		t.Errorf("ProfilePath dir mismatch: got %q want under %q", pp, pd)
	}
	if filepath.Base(pp) != "Contoso.MainDev.toml" {
		t.Errorf("ProfilePath base: %q", filepath.Base(pp))
	}

	if _, err := ProfilePath("../escape"); err == nil {
		t.Error("ProfilePath should reject traversal")
	}
}
