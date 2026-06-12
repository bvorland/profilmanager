package providers

import (
	"strings"
	"testing"
)

func TestDetectDrift(t *testing.T) {
	cases := []struct {
		name     string
		statuses []Status
		wantLen  int
		wantCode string
	}{
		{
			name: "matching",
			statuses: []Status{
				{Provider: "az", LoggedIn: true, Account: "u@x", Subscription: "s1"},
				{Provider: "azd", LoggedIn: true, Account: "u@x", Subscription: "s1"},
			},
			wantLen: 0,
		},
		{
			name: "sub-mismatch",
			statuses: []Status{
				{Provider: "az", LoggedIn: true, Account: "u@x", Subscription: "s1"},
				{Provider: "azd", LoggedIn: true, Account: "u@x", Subscription: "s2"},
			},
			wantLen:  1,
			wantCode: "az-azd-subscription-mismatch",
		},
		{
			name: "account-mismatch",
			statuses: []Status{
				{Provider: "az", LoggedIn: true, Account: "a@x", Subscription: "s1"},
				{Provider: "azd", LoggedIn: true, Account: "b@x", Subscription: "s1"},
			},
			wantLen:  1,
			wantCode: "az-azd-account-mismatch",
		},
		{
			name: "both",
			statuses: []Status{
				{Provider: "az", LoggedIn: true, Account: "a@x", Subscription: "s1"},
				{Provider: "azd", LoggedIn: true, Account: "b@x", Subscription: "s2"},
			},
			wantLen: 2,
		},
		{
			name: "azd-not-logged-in",
			statuses: []Status{
				{Provider: "az", LoggedIn: true, Account: "a@x", Subscription: "s1"},
				{Provider: "azd", LoggedIn: false},
			},
			wantLen: 0,
		},
		{
			name: "az-missing-tool",
			statuses: []Status{
				{Provider: "azd", LoggedIn: true, Account: "u@x", Subscription: "s1"},
			},
			wantLen: 0,
		},
		{
			name: "empty-sub-on-azd",
			statuses: []Status{
				{Provider: "az", LoggedIn: true, Account: "a@x", Subscription: "s1"},
				{Provider: "azd", LoggedIn: true, Account: "a@x", Subscription: ""},
			},
			wantLen: 0,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := DetectDrift(c.statuses)
			if len(got) != c.wantLen {
				t.Fatalf("len = %d want %d; got = %+v", len(got), c.wantLen, got)
			}
			if c.wantCode != "" {
				found := false
				for _, d := range got {
					if d.Code == c.wantCode {
						found = true
					}
				}
				if !found {
					t.Errorf("missing code %q in %+v", c.wantCode, got)
				}
			}
		})
	}
}

func TestDriftSubscriptionMismatchFix(t *testing.T) {
	got := DetectDrift([]Status{
		{Provider: "az", LoggedIn: true, Subscription: "sub-az-id"},
		{Provider: "azd", LoggedIn: true, Subscription: "sub-azd-id"},
	})
	if len(got) != 1 {
		t.Fatalf("expected 1 drift, got %+v", got)
	}
	d := got[0]
	if !strings.Contains(d.Fix, "sub-az-id") || !strings.Contains(d.Fix, "sub-azd-id") {
		t.Errorf("Fix should mention both subscriptions, got %q", d.Fix)
	}
	if d.Severity != "warn" {
		t.Errorf("Severity = %q", d.Severity)
	}
}
