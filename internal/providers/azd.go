package providers

import (
	"context"
	"encoding/json"
	"os"

	"github.com/bvorland/profilmanager/internal/core"
)

// azdProvider integrates the Azure Developer CLI (azd).
//
// azd carries its own auth state under AZD_CONFIG_DIR. We don't try to
// disable WAM here (azd doesn't expose the same broker knob) — we just
// pin the config dir per profile and rely on `azd auth token` returning
// the correct identity for whoever azd considers logged in.
type azdProvider struct{}

func AzdProvider() Provider { return azdProvider{} }

func (azdProvider) Name() string { return "azd" }

func (azdProvider) Available() bool {
	_, err := lookPath("azd")
	return err == nil
}

func (azdProvider) Apply(p *core.Profile) (map[string]string, error) {
	env := map[string]string{}
	if p == nil || p.Azd == nil || p.Azd.ConfigDir == "" {
		return env, nil
	}
	dir := expandHome(p.Azd.ConfigDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, errf("azd", "create AZD_CONFIG_DIR", err)
	}
	env["AZD_CONFIG_DIR"] = dir
	return env, nil
}

func (azdProvider) Whoami(ctx context.Context) (Status, error) {
	st := Status{Provider: "azd"}
	if _, err := lookPath("azd"); err != nil {
		st.Error = "azd CLI not installed"
		return st, nil
	}
	ctx, cancel := withDefaultTimeout(ctx)
	defer cancel()
	stdout, stderr, err := runCmd(ctx, "azd", "auth", "token", "--output", "json")
	if err != nil {
		if isExitErr(err) {
			st.Error = "not logged in"
			if hint := truncStderr(stderr); hint != "" {
				st.Error = hint
			}
			return st, nil
		}
		return st, errf("azd", "auth token", err)
	}
	var tok azdToken
	if err := json.Unmarshal(stdout, &tok); err != nil {
		return st, errf("azd", "parse auth token", err)
	}
	if tok.Token == "" {
		st.Error = "azd returned empty token"
		return st, nil
	}
	st.LoggedIn = true
	st.Extra = map[string]string{}
	if tok.ExpiresOn != "" {
		st.Extra["expires_on"] = tok.ExpiresOn
	}
	if claims, err := decodeJWT(tok.Token); err == nil {
		st.Account = claims.pickAccount()
		st.Tenant = claims.TID
		if claims.OID != "" {
			st.Extra["oid"] = claims.OID
		}
	}
	return st, nil
}

// azdToken is the shape `azd auth token --output json` emits.
type azdToken struct {
	Token     string `json:"token"`
	ExpiresOn string `json:"expiresOn"`
}
