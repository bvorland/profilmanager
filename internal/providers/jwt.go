package providers

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
)

// jwtClaims is the subset of JWT payload fields we care about for
// identity attribution. The full token has dozens of claims; we read
// only what `pm whoami` needs.
type jwtClaims struct {
	UPN        string `json:"upn,omitempty"`
	UniqueName string `json:"unique_name,omitempty"`
	Email      string `json:"email,omitempty"`
	PreferredU string `json:"preferred_username,omitempty"`
	Name       string `json:"name,omitempty"`
	OID        string `json:"oid,omitempty"`
	TID        string `json:"tid,omitempty"`
}

// decodeJWT decodes the payload segment of a JWT (or anything in the
// same dot-separated base64url-payload shape) and returns the parsed
// claims. We never verify the signature — the token came from a CLI we
// already trust, and we only use it to attribute identity.
func decodeJWT(token string) (jwtClaims, error) {
	var c jwtClaims
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return c, errors.New("token is not a JWT (need three dot-separated segments)")
	}
	payload := parts[1]
	// base64url -> base64 + padding (same fix the sample's PowerShell does).
	payload = strings.ReplaceAll(payload, "-", "+")
	payload = strings.ReplaceAll(payload, "_", "/")
	if pad := len(payload) % 4; pad != 0 {
		payload += strings.Repeat("=", 4-pad)
	}
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return c, err
	}
	return c, nil
}

// pickAccount returns the most user-meaningful identifier in c, mirroring
// the priority chain from the sample's whoami block:
// upn → unique_name → email → preferred_username → name.
func (c jwtClaims) pickAccount() string {
	for _, v := range []string{c.UPN, c.UniqueName, c.Email, c.PreferredU, c.Name} {
		if v != "" {
			return v
		}
	}
	return ""
}
