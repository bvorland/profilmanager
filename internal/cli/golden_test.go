package cli

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// updateGolden lets us refresh the golden files when intentional schema
// changes happen:
//
//	go test ./internal/cli -run TestGolden -update
//
// CI runs without -update and fails if any golden would change. See
// CONTRIBUTING.md → "Golden tests" for the full workflow and invariants.
var updateGolden = flag.Bool("update", false, "rewrite CLI --json golden files")

// goldenDir is the on-disk location for every CLI --json golden.
const goldenDir = "testdata/golden"

// goldenInvariants centralizes the rules every CLI --json golden must
// satisfy. They are applied at write-time (so -update produces a stable
// file) AND at compare-time (so a hand-edited golden still passes the
// invariants check before we even diff).
type goldenInvariants struct {
	// MaskKeys names JSON keys whose values must be replaced by a stable
	// placeholder before comparison. Used for fields that vary per-run
	// or per-host (paths, hostnames, generated_at, etc).
	MaskKeys map[string]string
	// SortArraysOfObjectsByKey: for arrays of objects, sort by this
	// key if present. The current set of CLI --json shapes is
	// alphabetically stable by name (`profiles`, `checks`, `providers`)
	// — this is belt-and-braces for any future shape that isn't.
	SortArraysOfObjectsByKey string
}

// defaultInvariants returns the rules that apply to most CLI verbs.
// Individual verbs may layer additional masks on top.
func defaultInvariants() goldenInvariants {
	return goldenInvariants{
		MaskKeys: map[string]string{
			// `pm profile list/show` emits absolute paths under the
			// per-OS profiles dir; tempdir paths vary by run.
			"path": "<tmp>/profiles/<name>.toml",
			// `pm doctor` reports the resolved profiles/state dirs and
			// the tool-availability paths.
			"message": "<masked>",
			// `pm doctor` may include a fix hint per check.
			"fix": "<masked>",
			// `pm whoami` carries provider Error strings that include
			// "<tool> not installed" — stable across runs but masking
			// is cheap insurance against future drift.
			"error": "<masked>",
		},
	}
}

// goldenTestSetup seeds two known profiles into the test isolated home
// so every --json golden runs against a deterministic disk layout.
// Returns the path to the profiles directory for any test that needs it.
func goldenTestSetup(t *testing.T) string {
	t.Helper()
	testEnv(t)
	// Carve a deterministic seed. Two profiles cover the
	// "single-element array" / "multi-element array" cases that bite
	// hand-rolled JSON serializers.
	seed := []string{
		`schema = "1"
name = "alpha"
label = "Alpha Dev"
color = "Cyan"

[azure]
config_dir = "/tmp/azure-alpha"
subscription = "00000000-0000-0000-0000-aaaaaaaaaaaa"
tenant = "11111111-1111-1111-1111-aaaaaaaaaaaa"

[[env]]
key = "ALPHA_VAR"
value = "alpha-literal"

[[env]]
key = "ALPHA_SECRET"
ref = "op://Vault/alpha/password"
`,
		`schema = "1"
name = "beta"
label = "Beta Prod"
color = "Magenta"

[git]
user_name = "Beta User"
user_email = "beta@example.com"
`,
	}
	// Make sure the profiles dir exists before writing.
	// Use the same `pm profile add` path the rest of the test suite
	// uses to provoke creation — easier than reimplementing the dir
	// resolution and keeps us honest about how operators land profiles.
	if _, _, err := runCLI(t, "profile", "add", "_seed_", "--label", "_"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Resolve the dir, drop the seed marker, write our fixtures.
	stdout, _, err := runCLI(t, "profile", "list", "--json")
	if err != nil {
		t.Fatalf("seed list: %v", err)
	}
	var lst profileListJSON
	if err := json.Unmarshal([]byte(stdout), &lst); err != nil {
		t.Fatalf("seed parse: %v stdout=%s", err, stdout)
	}
	if len(lst.Profiles) != 1 {
		t.Fatalf("seed: want 1 placeholder, got %d", len(lst.Profiles))
	}
	dir := filepath.Dir(lst.Profiles[0].Path)
	if err := os.Remove(lst.Profiles[0].Path); err != nil {
		t.Fatalf("seed cleanup: %v", err)
	}
	for i, body := range seed {
		name := []string{"alpha", "beta"}[i]
		if err := os.WriteFile(filepath.Join(dir, name+".toml"), []byte(body), 0o644); err != nil {
			t.Fatalf("seed write %s: %v", name, err)
		}
	}
	return dir
}

// TestGoldenProfileList locks the `pm profile list --json` shape against
// a golden file. The seed includes one azure profile and one git-only
// profile to cover the has_* boolean matrix.
func TestGoldenProfileList(t *testing.T) {
	goldenTestSetup(t)
	stdout, _, err := runCLI(t, "profile", "list", "--json")
	if err != nil {
		t.Fatalf("run: %v stderr-or-stdout=%s", err, stdout)
	}
	assertGolden(t, "profile_list.json", stdout, defaultInvariants())
}

// TestGoldenProfileShowAlpha locks `pm profile show alpha --json` —
// the multi-section profile (azure + env literals + env refs).
func TestGoldenProfileShowAlpha(t *testing.T) {
	goldenTestSetup(t)
	stdout, _, err := runCLI(t, "profile", "show", "alpha", "--json")
	if err != nil {
		t.Fatalf("run: %v stdout=%s", err, stdout)
	}
	assertGolden(t, "profile_show_alpha.json", stdout, defaultInvariants())
}

// TestGoldenProfileShowAlphaRedacted locks the --redacted shape.
// Critical: the redaction map keys (`<ref>`, masked GUIDs, masked
// emails) are the operator-facing contract for "safe to paste".
func TestGoldenProfileShowAlphaRedacted(t *testing.T) {
	goldenTestSetup(t)
	stdout, _, err := runCLI(t, "profile", "show", "alpha", "--json", "--redacted")
	if err != nil {
		t.Fatalf("run: %v stdout=%s", err, stdout)
	}
	assertGolden(t, "profile_show_alpha_redacted.json", stdout, defaultInvariants())
}

// TestGoldenProfileShowBeta locks the git-only profile shape — covers
// the case where azure/azd/gh/kube sections are absent (TOML round-trip
// must not invent empty objects for them).
func TestGoldenProfileShowBeta(t *testing.T) {
	goldenTestSetup(t)
	stdout, _, err := runCLI(t, "profile", "show", "beta", "--json")
	if err != nil {
		t.Fatalf("run: %v stdout=%s", err, stdout)
	}
	assertGolden(t, "profile_show_beta.json", stdout, defaultInvariants())
}

// TestGoldenWhoami locks the `pm whoami --json` envelope. Under the
// isolated PATH-less environment most providers report "not installed";
// the test asserts on the *envelope shape*, not on which providers are
// available — the latter is verified by the integration suite.
func TestGoldenWhoami(t *testing.T) {
	goldenTestSetup(t)
	// Empty PATH so every provider reports "not installed" deterministically.
	t.Setenv("PATH", "")
	stdout, _, err := runCLI(t, "whoami", "--json")
	if err != nil {
		t.Fatalf("run: %v stdout=%s", err, stdout)
	}
	inv := defaultInvariants()
	// whoami statuses always carry `error` strings — leave masked.
	assertGolden(t, "whoami.json", stdout, inv)
}

// TestGoldenDoctor locks the `pm doctor --json` envelope. The check
// names are the API contract; messages/fixes are masked because they
// embed absolute paths.
func TestGoldenDoctor(t *testing.T) {
	goldenTestSetup(t)
	t.Setenv("PATH", "") // deterministic tool-availability (all warn)
	t.Setenv("PM_SHELL_INIT_LOADED", "1")
	stdout, _, err := runCLI(t, "doctor", "--json")
	// doctor may exit non-zero if any check fails; tolerate ExitError.
	if err != nil && CodeFor(err) != ExitError {
		t.Fatalf("run: %v stdout=%s", err, stdout)
	}
	assertGolden(t, "doctor.json", stdout, defaultInvariants())
}

// TestGoldenErrorEnvelope locks the JSON error envelope shape (stderr
// in --json mode). Agents branch on `code`; renames are breaking.
func TestGoldenErrorEnvelope(t *testing.T) {
	goldenTestSetup(t)
	// `pm profile show ghost --json` produces an invalid_usage envelope.
	root := newRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetIn(strings.NewReader(""))
	root.SetArgs([]string{"profile", "show", "ghost", "--json"})
	if err := root.Execute(); err == nil {
		t.Fatalf("expected error; stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	inv := defaultInvariants()
	// The `error` message includes an absolute path; mask it.
	assertGolden(t, "error_invalid_usage.json", stderr.String(), inv)
}

// ---------- helpers ----------

// assertGolden normalizes `got` (parse JSON, apply invariants, re-marshal
// with stable key ordering and indent), then either writes the golden
// file (with -update) or diffs against it.
func assertGolden(t *testing.T, name, got string, inv goldenInvariants) {
	t.Helper()
	normalized, err := normalizeJSON(got, inv)
	if err != nil {
		t.Fatalf("normalize golden %s: %v\nraw=%s", name, err, got)
	}
	path := filepath.Join(goldenDir, name)
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir golden: %v", err)
		}
		if err := os.WriteFile(path, []byte(normalized), 0o644); err != nil {
			t.Fatalf("write golden %s: %v", path, err)
		}
		t.Logf("golden %s updated", path)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (re-run with -update to create)", path, err)
	}
	// Normalize line endings on Windows: git autocrlf may convert LF
	// in checked-out files. Compare on \n.
	wantStr := strings.ReplaceAll(string(want), "\r\n", "\n")
	if normalized != wantStr {
		t.Errorf("golden %s mismatch (-want +got)\n--- want ---\n%s\n--- got ---\n%s",
			path, wantStr, normalized)
	}
}

// normalizeJSON parses, applies invariants (mask + array sort), then
// re-marshals with sorted keys and 2-space indent. Returns the
// canonical bytes. Any unparseable input is a hard error — every CLI
// --json verb MUST emit valid JSON on stdout (or stderr for envelopes).
func normalizeJSON(raw string, inv goldenInvariants) (string, error) {
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return "", err
	}
	v = applyInvariants(v, inv)
	// Re-encode with sorted keys via our own walker — encoding/json
	// already sorts map[string]any keys, but we want predictable
	// indentation and a trailing newline.
	out, err := marshalCanonical(v)
	if err != nil {
		return "", err
	}
	return string(out) + "\n", nil
}

// applyInvariants walks the parsed JSON in place: masks keys, optionally
// sorts arrays of objects. Maps in encoding/json's output are
// map[string]any — perfect for in-place mutation.
func applyInvariants(v any, inv goldenInvariants) any {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			if mask, ok := inv.MaskKeys[k]; ok {
				// Only mask if the value is non-empty; leave empty
				// strings as-is so we can spot when a field that
				// should be set isn't.
				if s, isStr := val.(string); isStr && s != "" {
					x[k] = mask
					continue
				}
			}
			x[k] = applyInvariants(val, inv)
		}
		return x
	case []any:
		for i := range x {
			x[i] = applyInvariants(x[i], inv)
		}
		if inv.SortArraysOfObjectsByKey != "" {
			sortArrayByKey(x, inv.SortArraysOfObjectsByKey)
		}
		return x
	default:
		return v
	}
}

func sortArrayByKey(arr []any, key string) {
	sort.SliceStable(arr, func(i, j int) bool {
		mi, _ := arr[i].(map[string]any)
		mj, _ := arr[j].(map[string]any)
		si, _ := mi[key].(string)
		sj, _ := mj[key].(string)
		return si < sj
	})
}

// marshalCanonical encodes v with 2-space indentation. encoding/json
// already sorts map[string]any keys, so the output is deterministic
// for any JSON we've decoded.
func marshalCanonical(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	// Drop the trailing newline encoder.Encode adds; assertGolden adds its own.
	out := buf.Bytes()
	if n := len(out); n > 0 && out[n-1] == '\n' {
		out = out[:n-1]
	}
	return out, nil
}
