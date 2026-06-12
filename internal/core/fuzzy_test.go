package core

import (
	"reflect"
	"strings"
	"testing"
)

func TestSuggestNamesTypo(t *testing.T) {
	setupProfilesDir(t)
	writeProfile(t, "Contoso.MainDev")

	got := SuggestNames("Cntso.MainDev", 3)
	want := []string{"Contoso.MainDev"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestSuggestNamesCaseAndEdit(t *testing.T) {
	setupProfilesDir(t)
	writeProfile(t, "Contoso.MainDev")

	got := SuggestNames("contso.maindev", 3)
	want := []string{"Contoso.MainDev"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestSuggestNamesUnrelated(t *testing.T) {
	setupProfilesDir(t)
	writeProfile(t, "Contoso.MainDev")

	if got := SuggestNames("totally-unrelated-name", 3); got != nil {
		t.Fatalf("got %#v, want nil", got)
	}
}

func TestSuggestNamesEmptyProfileDir(t *testing.T) {
	setupProfilesDir(t)

	if got := SuggestNames("Cntso.MainDev", 3); got != nil {
		t.Fatalf("got %#v, want nil", got)
	}
}

func TestSuggestNamesSortsByDistanceAndCapsAtThree(t *testing.T) {
	setupProfilesDir(t)
	writeProfile(t, "alpha")
	writeProfile(t, "alpaca")
	writeProfile(t, "alpine")
	writeProfile(t, "omega")

	got := SuggestNames("alpa", 3)
	want := []string{"alpha", "alpaca", "alpine"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestLevenshtein(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"kitten", "sitting", 3},
		{"Cntso.MainDev", "Contoso.MainDev", 2},
		{"contso.maindev", "contoso.maindev", 1},
	}
	for _, tt := range tests {
		if got := levenshtein(tt.a, tt.b); got != tt.want {
			t.Fatalf("levenshtein(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestSuggestNamesCapsLongInput(t *testing.T) {
	setupProfilesDir(t)
	writeProfile(t, strings.Repeat("a", maxFuzzyInputRunes))

	got := SuggestNames(strings.Repeat("a", maxFuzzyInputRunes+1), 0)
	want := []string{strings.Repeat("a", maxFuzzyInputRunes)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}
