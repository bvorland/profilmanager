package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/bvorland/profilmanager/internal/core"
	"github.com/bvorland/profilmanager/internal/state"
)

func TestStatusline_EmptyStdin(t *testing.T) {
	testEnv(t)
	stdout, _, err := runCLIWithStdin(t, "", "statusline", "--no-omp")
	if err != nil {
		t.Fatalf("empty stdin: err=%v", err)
	}
	if stdout == "" {
		t.Fatalf("expected non-empty fallback, got empty")
	}
}

func TestStatusline_MalformedJSON(t *testing.T) {
	testEnv(t)
	stdout, _, err := runCLIWithStdin(t, "not json {[", "statusline", "--no-omp")
	if err != nil {
		t.Fatalf("malformed json: err=%v", err)
	}
	if !strings.Contains(stdout, "host") && !strings.Contains(stdout, "profile") {
		t.Fatalf("expected fallback text, got %q", stdout)
	}
}

func TestStatusline_FullPayload_NoRender(t *testing.T) {
	testEnv(t)
	payload := `{
		"session_name":"v0.9-test",
		"cwd":"C:\\repo",
		"workspace":{"current_dir":"C:\\repo"},
		"model":{"display_name":"claude-opus-4.7-xhigh","id":"claude-opus-4.7-xhigh"},
		"context_window":{
			"used_percentage":42,
			"context_window_size":200000,
			"last_call_input_tokens":1234,
			"total_input_tokens":12345,
			"total_output_tokens":678,
			"total_cache_read_tokens":900,
			"total_cache_write_tokens":50,
			"total_reasoning_tokens":11
		},
		"cost":{
			"total_premium_requests":3,
			"total_api_duration_ms":54321,
			"total_duration_ms":75000,
			"total_lines_added":120,
			"total_lines_removed":45
		}
	}`
	stdout, _, err := runCLIWithStdin(t, payload, "statusline", "--no-omp", "--render=false")
	if err != nil {
		t.Fatalf("full payload: err=%v", err)
	}
	expectKVs := map[string]string{
		"PM_SL_MODEL":              "claude-opus-4.7-xhigh",
		"PM_SL_CONTEXT_PCT":        "42",
		"PM_SL_CONTEXT_SIZE":       "200000",
		"PM_SL_CONTEXT_GAUGE":      "▰▰▱▱▱",
		"PM_SL_CONTEXT_BG":         "#4caf50",
		"PM_SL_TOKENS_IN":          "12345",
		"PM_SL_TOKENS_OUT":         "678",
		"PM_SL_TOKENS_CACHE_READ":  "900",
		"PM_SL_TOKENS_CACHE_WRITE": "50",
		"PM_SL_TOKENS_REASONING":   "11",
		"PM_SL_LAST_CALL_IN":       "1234",
		"PM_SL_PREMIUM_REQ":        "3",
		"PM_SL_API_DURATION_MS":    "54321",
		"PM_SL_DURATION_MS":        "75000",
		"PM_SL_DURATION_HUMAN":     "1m 15s",
		"PM_SL_LINES_ADDED":        "120",
		"PM_SL_LINES_REMOVED":      "45",
		"PM_SL_SESSION_NAME":       "v0.9-test",
		"PM_SL_CWD":                "C:\\repo",
	}
	for k, v := range expectKVs {
		want := k + "=" + v
		if !strings.Contains(stdout, want+"\n") {
			t.Errorf("expected %q in output, missing.\nstdout=%s", want, stdout)
		}
	}
}

func TestStatusline_ContextGauge(t *testing.T) {
	cases := []struct {
		pct  int64
		want string
	}{
		{0, "▱▱▱▱▱"},
		{20, "▰▱▱▱▱"},
		{50, "▰▰▱▱▱"},
		{80, "▰▰▰▰▱"},
		{100, "▰▰▰▰▰"},
		{-5, "▱▱▱▱▱"},
		{999, "▰▰▰▰▰"},
	}
	for _, tc := range cases {
		got := contextGauge(tc.pct)
		if got != tc.want {
			t.Errorf("contextGauge(%d)=%q want %q", tc.pct, got, tc.want)
		}
	}
}

func TestStatusline_DurationHuman(t *testing.T) {
	cases := []struct {
		ms   int64
		want string
	}{
		{0, "0s"},
		{1500, "1s"},
		{59000, "59s"},
		{65000, "1m 5s"},
		{3_661_000, "1h 1m"},
		{7_200_000, "2h 0m"},
		{-100, "0s"},
	}
	for _, tc := range cases {
		got := humanDuration(tc.ms)
		if got != tc.want {
			t.Errorf("humanDuration(%d)=%q want %q", tc.ms, got, tc.want)
		}
	}
}

func TestStatusline_ContextBG(t *testing.T) {
	cases := []struct {
		pct  int64
		want string
	}{
		{0, "#4caf50"},
		{59, "#4caf50"},
		{60, "#fdd835"},
		{84, "#fdd835"},
		{85, "#e53935"},
		{100, "#e53935"},
	}
	for _, tc := range cases {
		got := contextBG(tc.pct)
		if got != tc.want {
			t.Errorf("contextBG(%d)=%q want %q", tc.pct, got, tc.want)
		}
	}
}

func TestStatusline_ActiveProfileLoaded(t *testing.T) {
	testEnv(t)
	if _, _, err := runCLI(t, "profile", "add", "demo", "--label", "Demo", "--color", "Cyan"); err != nil {
		t.Fatalf("profile add: %v", err)
	}
	if err := state.SetActiveProfile("demo"); err != nil {
		t.Fatalf("SetActiveProfile: %v", err)
	}
	stdout, _, err := runCLIWithStdin(t, `{"model":{"display_name":"gpt-5.4"}}`, "statusline", "--no-omp", "--render=false")
	if err != nil {
		t.Fatalf("statusline: %v", err)
	}
	wants := []string{
		"PM_ACTIVE_PROFILE=demo",
		"PM_ACTIVE_PROFILE_BG=" + core.ColorHex("Cyan"),
		"PM_ACTIVE_PROFILE_EMOJI=" + core.ColorEmoji("Cyan"),
		"PM_ACTIVE_PROFILE_COLOR=Cyan",
		"PM_SL_MODEL=gpt-5.4",
	}
	for _, w := range wants {
		if !strings.Contains(stdout, w+"\n") {
			t.Errorf("missing %q in output:\n%s", w, stdout)
		}
	}
}

func TestStatusline_NeverPanics(t *testing.T) {
	testEnv(t)
	// Broken JSON.
	if _, _, err := runCLIWithStdin(t, "}{not json", "statusline", "--no-omp"); err != nil {
		t.Fatalf("broken json: %v", err)
	}
	// Set PM_ACTIVE_PROFILE_BG to nothing and active profile missing on disk.
	if err := state.SetActiveProfile("ghost"); err != nil {
		t.Fatalf("SetActiveProfile: %v", err)
	}
	if _, _, err := runCLIWithStdin(t, "{}", "statusline", "--no-omp"); err != nil {
		t.Fatalf("missing profile: %v", err)
	}
	// Garbage at the start of the payload.
	if _, _, err := runCLIWithStdin(t, "\x00\x01\x02", "statusline", "--no-omp"); err != nil {
		t.Fatalf("binary garbage: %v", err)
	}
}

func TestStatusline_ZeroValuesSkipped(t *testing.T) {
	testEnv(t)
	stdout, _, err := runCLIWithStdin(t, `{"context_window":{"total_input_tokens":0,"total_output_tokens":0},"cost":{"total_premium_requests":0,"total_lines_added":0,"total_lines_removed":0}}`, "statusline", "--no-omp", "--render=false")
	if err != nil {
		t.Fatalf("zero-values: %v", err)
	}
	for _, k := range []string{"PM_SL_TOKENS_IN", "PM_SL_TOKENS_OUT", "PM_SL_PREMIUM_REQ", "PM_SL_LINES_ADDED", "PM_SL_LINES_REMOVED"} {
		if strings.Contains(stdout, k+"=") {
			t.Errorf("expected %s to be omitted at value 0, got:\n%s", k, stdout)
		}
	}
}

func TestStatusline_ProfilesDirIntact(t *testing.T) {
	tmp := testEnv(t)
	// Sanity: statusline should not crash even if the profiles dir is empty.
	if _, _, err := runCLIWithStdin(t, "{}", "statusline", "--no-omp"); err != nil {
		t.Fatalf("empty profiles dir: %v", err)
	}
	if _, err := core.ProfilesDir(); err != nil {
		t.Fatalf("ProfilesDir not created: %v", err)
	}
	dir, _ := core.ProfilesDir()
	if !strings.HasPrefix(dir, tmp) {
		t.Fatalf("ProfilesDir %q escaped tmp %q", dir, tmp)
	}
	_ = filepath.Join
}
