package providers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bvorland/profilmanager/internal/core"
)

// buildFakeJWT returns a minimal three-segment JWT-shaped string whose
// middle segment decodes to the given claims map. Header and signature
// are placeholders; nothing here validates the signature.
func buildFakeJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	body, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	payload := base64.RawURLEncoding.EncodeToString(body)
	sig := base64.RawURLEncoding.EncodeToString([]byte("sig"))
	return fmt.Sprintf("%s.%s.%s", header, payload, sig)
}

func TestAzdWhoamiLoggedIn(t *testing.T) {
	dir := fakePathDir(t)
	tok := buildFakeJWT(t, map[string]any{
		"upn": "bob@contoso.com",
		"tid": "tenant-9999",
		"oid": "object-1234",
	})
	body := fmt.Sprintf(`{"token":"%s","expiresOn":"2099-12-31T23:59:59Z"}`, tok)
	writeFakeCLI(t, dir, "azd",
		fakeCase{Stdout: body, Exit: 0},
	)
	st, err := (azdProvider{}).Whoami(context.Background())
	if err != nil {
		t.Fatalf("Whoami: %v", err)
	}
	if !st.LoggedIn {
		t.Fatalf("expected LoggedIn=true, got %+v", st)
	}
	if st.Account != "bob@contoso.com" {
		t.Errorf("Account = %q", st.Account)
	}
	if st.Tenant != "tenant-9999" {
		t.Errorf("Tenant = %q", st.Tenant)
	}
	if st.Extra["oid"] != "object-1234" {
		t.Errorf("oid = %q", st.Extra["oid"])
	}
	if st.Extra["expires_on"] != "2099-12-31T23:59:59Z" {
		t.Errorf("expires_on = %q", st.Extra["expires_on"])
	}
}

func TestAzdWhoamiNotLoggedIn(t *testing.T) {
	dir := fakePathDir(t)
	writeFakeCLI(t, dir, "azd",
		fakeCase{Stderr: "ERROR: not logged in, run 'azd auth login' to login", Exit: 1},
	)
	st, err := (azdProvider{}).Whoami(context.Background())
	if err != nil {
		t.Fatalf("Whoami: %v", err)
	}
	if st.LoggedIn {
		t.Errorf("expected LoggedIn=false")
	}
	if !strings.Contains(st.Error, "azd auth login") && st.Error == "" {
		t.Errorf("expected stderr hint, got %q", st.Error)
	}
}

func TestAzdWhoamiMissing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	st, err := (azdProvider{}).Whoami(context.Background())
	if err != nil {
		t.Fatalf("Whoami: %v", err)
	}
	if st.LoggedIn {
		t.Errorf("expected LoggedIn=false")
	}
	if !strings.Contains(st.Error, "azd CLI not installed") {
		t.Errorf("Error = %q", st.Error)
	}
}

func TestAzdApplyCreatesDir(t *testing.T) {
	tmp := t.TempDir()
	cfgDir := filepath.Join(tmp, "azd-test")
	p := &core.Profile{
		Schema: "1", Name: "test",
		Azd: &core.AzdProfile{ConfigDir: cfgDir},
	}
	env, err := (azdProvider{}).Apply(p)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if env["AZD_CONFIG_DIR"] != cfgDir {
		t.Errorf("AZD_CONFIG_DIR = %q want %q", env["AZD_CONFIG_DIR"], cfgDir)
	}
	if fi, err := os.Stat(cfgDir); err != nil || !fi.IsDir() {
		t.Errorf("dir not created: %v %v", fi, err)
	}
}

func TestAzdApplyWithoutAzdBlock(t *testing.T) {
	p := &core.Profile{Schema: "1", Name: "p"}
	env, err := (azdProvider{}).Apply(p)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if _, ok := env["AZD_CONFIG_DIR"]; ok {
		t.Errorf("AZD_CONFIG_DIR should not be set without Azd config_dir")
	}
}

func TestJWTDecodeFallbacks(t *testing.T) {
	t.Run("prefers upn", func(t *testing.T) {
		c := jwtClaims{UPN: "u@example.com", Email: "e@example.com"}
		if got := c.pickAccount(); got != "u@example.com" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("falls through to email", func(t *testing.T) {
		c := jwtClaims{Email: "e@example.com"}
		if got := c.pickAccount(); got != "e@example.com" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("nothing", func(t *testing.T) {
		c := jwtClaims{}
		if got := c.pickAccount(); got != "" {
			t.Errorf("got %q", got)
		}
	})
}
