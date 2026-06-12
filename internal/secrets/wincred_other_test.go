//go:build !windows

package secrets

import (
	"context"
	"testing"
)

func TestWinCredStubAlwaysUnavailable(t *testing.T) {
	r := NewWinCredResolver()
	if r.Available() {
		t.Fatal("wincred must be unavailable on non-Windows")
	}
	_, err := r.Resolve(context.Background(), "wincred://X")
	if err == nil {
		t.Fatal("Resolve should fail on non-Windows")
	}
	md, err := r.Describe(context.Background(), "wincred://X")
	if err != nil {
		t.Fatalf("Describe stub: %v", err)
	}
	if md.Exists {
		t.Fatalf("Describe stub: Exists must be false: %+v", md)
	}
}
