package state

import (
	"testing"
)

func TestRenameProfileMarkers_UpdatesActiveAndLast(t *testing.T) {
	stubStateDirs(t)
	t.Setenv("PM_SESSION_ID", "rename-markers-session")

	if err := SetActiveProfile("Contoso.Old"); err != nil {
		t.Fatalf("SetActiveProfile: %v", err)
	}
	if err := SetLastProfile("Contoso.Old"); err != nil {
		t.Fatalf("SetLastProfile: %v", err)
	}

	if err := RenameProfileMarkers("Contoso.Old", "Contoso.New"); err != nil {
		t.Fatalf("RenameProfileMarkers: %v", err)
	}

	active, _, err := GetActiveProfile()
	if err != nil {
		t.Fatalf("GetActiveProfile: %v", err)
	}
	if active != "Contoso.New" {
		t.Fatalf("active marker = %q, want Contoso.New", active)
	}
	last, err := GetLastProfile()
	if err != nil {
		t.Fatalf("GetLastProfile: %v", err)
	}
	if last != "Contoso.New" {
		t.Fatalf("last-profile = %q, want Contoso.New", last)
	}
}

func TestRenameProfileMarkers_LeavesUnrelatedMarkers(t *testing.T) {
	stubStateDirs(t)
	t.Setenv("PM_SESSION_ID", "rename-markers-unrelated")

	if err := SetActiveProfile("Contoso.Active"); err != nil {
		t.Fatalf("SetActiveProfile: %v", err)
	}
	if err := SetLastProfile("Contoso.Last"); err != nil {
		t.Fatalf("SetLastProfile: %v", err)
	}

	if err := RenameProfileMarkers("Contoso.Other", "Contoso.New"); err != nil {
		t.Fatalf("RenameProfileMarkers: %v", err)
	}

	active, _, _ := GetActiveProfile()
	if active != "Contoso.Active" {
		t.Fatalf("active marker changed unexpectedly: %q", active)
	}
	last, _ := GetLastProfile()
	if last != "Contoso.Last" {
		t.Fatalf("last-profile changed unexpectedly: %q", last)
	}
}
