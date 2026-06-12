package providers

import (
	"context"
	"strings"
	"testing"
)

func TestFakeCLIHarness(t *testing.T) {
	dir := fakePathDir(t)
	writeFakeCLI(t, dir, "myfake",
		fakeCase{Match: "ping", Stdout: `{"pong":true}`, Exit: 0},
		fakeCase{Stdout: `default`, Stderr: `hello stderr`, Exit: 7},
	)
	// Confirm LookPath finds it.
	if _, err := lookPath("myfake"); err != nil {
		t.Fatalf("lookPath(myfake): %v", err)
	}
	ctx := context.Background()
	stdout, stderr, err := realRun(ctx, "myfake", "ping")
	if err != nil {
		t.Fatalf("realRun ping: %v (stderr=%s)", err, stderr)
	}
	if !strings.Contains(string(stdout), "pong") {
		t.Errorf("ping stdout: %q", stdout)
	}
	// Default case exits 7.
	_, stderrBytes, err := realRun(ctx, "myfake", "noplace")
	if err == nil {
		t.Fatalf("expected non-zero exit, got nil; stderr=%q", stderrBytes)
	}
	if !strings.Contains(string(stderrBytes), "hello stderr") {
		t.Errorf("default stderr: %q", stderrBytes)
	}
}
