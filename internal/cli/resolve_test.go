package cli

import (
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/bvorland/profilmanager/internal/core"
	"github.com/spf13/cobra"
)

var stdinMu sync.Mutex

func withNonTTYStdin(t *testing.T) {
	t.Helper()
	stdinMu.Lock()
	old := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		stdinMu.Unlock()
		t.Fatal(err)
	}
	_ = w.Close()
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = old
		_ = r.Close()
		stdinMu.Unlock()
	})
}

func TestResolveProfileArgNoArgsNonTTY(t *testing.T) {
	withTempDirs(t)
	withNonTTYStdin(t)

	_, err := resolveProfileArg(&cobra.Command{}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "stdin is not a TTY") {
		t.Fatalf("got %v", err)
	}
}

func TestResolveProfileArgExactName(t *testing.T) {
	withTempDirs(t)
	writeProfile(t, &core.Profile{Name: "Contoso.MainDev"})

	got, err := resolveProfileArg(&cobra.Command{}, []string{"Contoso.MainDev"})
	if err != nil {
		t.Fatalf("resolveProfileArg: %v", err)
	}
	if got != "Contoso.MainDev" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveProfileArgPrefix(t *testing.T) {
	withTempDirs(t)
	writeProfile(t, &core.Profile{Name: "Contoso.MainDev"})
	writeProfile(t, &core.Profile{Name: "Contoso.Dev"})

	got, err := resolveProfileArg(&cobra.Command{}, []string{"contoso.m"})
	if err != nil {
		t.Fatalf("resolveProfileArg: %v", err)
	}
	if got != "Contoso.MainDev" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveProfileArgAmbiguousPrefix(t *testing.T) {
	withTempDirs(t)
	writeProfile(t, &core.Profile{Name: "Contoso.MainDev"})
	writeProfile(t, &core.Profile{Name: "Contoso.MainProd"})

	_, err := resolveProfileArg(&cobra.Command{}, []string{"Contoso"})
	if !errors.Is(err, core.ErrAmbiguous) {
		t.Fatalf("got %v, want ErrAmbiguous", err)
	}
}

func TestResolveProfileArgTypoSuggests(t *testing.T) {
	withTempDirs(t)
	writeProfile(t, &core.Profile{Name: "Contoso.MainDev"})

	_, err := resolveProfileArg(&cobra.Command{}, []string{"Cntso.MainDev"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "Did you mean: Contoso.MainDev?") {
		t.Fatalf("got %v", err)
	}
}

func TestResolveProfileArgUnrelatedNoSuggestions(t *testing.T) {
	withTempDirs(t)
	writeProfile(t, &core.Profile{Name: "Contoso.MainDev"})

	_, err := resolveProfileArg(&cobra.Command{}, []string{"totally-unrelated-name"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), `profile "totally-unrelated-name" not found`) {
		t.Fatalf("got %v", err)
	}
	if strings.Contains(err.Error(), "Did you mean") {
		t.Fatalf("unexpected suggestion: %v", err)
	}
}
