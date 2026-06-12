package providers

import (
	"testing"
)

func TestRegistryHasAllFive(t *testing.T) {
	want := map[string]bool{
		"az":      false,
		"azd":     false,
		"gh":      false,
		"kubectl": false,
		"git":     false,
	}
	for _, p := range All() {
		if _, ok := want[p.Name()]; ok {
			want[p.Name()] = true
		}
	}
	for k, v := range want {
		if !v {
			t.Errorf("provider %q not registered", k)
		}
	}
}

func TestRegistryGet(t *testing.T) {
	for _, name := range []string{"az", "azd", "gh", "kubectl", "git"} {
		p, ok := Get(name)
		if !ok {
			t.Errorf("Get(%q) not ok", name)
			continue
		}
		if p.Name() != name {
			t.Errorf("Get(%q).Name() = %q", name, p.Name())
		}
	}
	if _, ok := Get("nope"); ok {
		t.Errorf("Get(nope) should be false")
	}
}

func TestRegistryAllSorted(t *testing.T) {
	prev := ""
	for _, p := range All() {
		if prev != "" && p.Name() < prev {
			t.Errorf("All() not sorted: %q after %q", p.Name(), prev)
		}
		prev = p.Name()
	}
}
