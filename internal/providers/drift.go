package providers

import (
	"fmt"
	"sort"
)

// Drift is one observed disagreement between two providers' Status. The
// rule set is intentionally small in v1 — only the two the sample
// surfaced (`az` vs `azd`). The shape is extensible: add a DriftRule, no
// change to consumers.
//
// Severity is "warn" for things the operator probably wants to know but
// can ignore in the short term, and "error" for things that will
// silently misroute a destructive operation if left in place.
type Drift struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	Fix      string `json:"fix,omitempty"`
}

// DriftRule examines a status snapshot and reports any drifts it finds.
// Rules are pure functions of the input slice; no state, no I/O.
type DriftRule func(byName map[string]Status) []Drift

// driftRules is the registered rule set. New rules go here; consumers
// just call DetectDrift.
var driftRules = []DriftRule{
	azAzdSubscriptionMismatch,
	azAzdAccountMismatch,
}

// DetectDrift runs every registered rule against statuses and returns
// every drift, sorted deterministically (by severity desc then code).
func DetectDrift(statuses []Status) []Drift {
	byName := make(map[string]Status, len(statuses))
	for _, s := range statuses {
		byName[s.Provider] = s
	}
	var out []Drift
	for _, rule := range driftRules {
		out = append(out, rule(byName)...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Severity != out[j].Severity {
			// "error" before "warn"
			return out[i].Severity == "error"
		}
		return out[i].Code < out[j].Code
	})
	return out
}

// azAzdSubscriptionMismatch is the sample's classic gotcha: az and azd
// disagree on which subscription is the default. Destructive operations
// (`azd provision`, `az group create`) land in different places.
func azAzdSubscriptionMismatch(byName map[string]Status) []Drift {
	az, azOK := byName["az"]
	azd, azdOK := byName["azd"]
	if !azOK || !azdOK || !az.LoggedIn || !azd.LoggedIn {
		return nil
	}
	if az.Subscription == "" || azd.Subscription == "" {
		return nil
	}
	// azd's "subscription" lives in `azd config list` → defaults.
	// `azd auth token` doesn't include it; we expose it via Extra
	// when the azd adapter learns it. For v1, only compare when both
	// are present and known.
	azdSub := azd.Subscription
	if azdSub == "" {
		if v, ok := azd.Extra["default_subscription"]; ok {
			azdSub = v
		}
	}
	if azdSub == "" || az.Subscription == azdSub {
		return nil
	}
	return []Drift{{
		Severity: "warn",
		Code:     "az-azd-subscription-mismatch",
		Message: fmt.Sprintf(
			"az subscription %s ≠ azd subscription %s",
			az.Subscription, azdSub,
		),
		Fix: fmt.Sprintf(
			"To align azd to az: azd config set defaults.subscription %s\nTo align az to azd: az account set -s %s",
			az.Subscription, azdSub,
		),
	}}
}

// azAzdAccountMismatch fires when the two CLIs are logged in as
// different identities. That's almost always operator error and tends
// to produce confusing failures rather than catastrophic ones — but
// it's the cheapest source of "huh, why did that 403?" bugs to
// eliminate.
func azAzdAccountMismatch(byName map[string]Status) []Drift {
	az, azOK := byName["az"]
	azd, azdOK := byName["azd"]
	if !azOK || !azdOK || !az.LoggedIn || !azd.LoggedIn {
		return nil
	}
	if az.Account == "" || azd.Account == "" {
		return nil
	}
	if az.Account == azd.Account {
		return nil
	}
	return []Drift{{
		Severity: "warn",
		Code:     "az-azd-account-mismatch",
		Message: fmt.Sprintf(
			"az account %s ≠ azd account %s",
			az.Account, azd.Account,
		),
		Fix: "Run 'az login' or 'azd auth login' to align identities.",
	}}
}
