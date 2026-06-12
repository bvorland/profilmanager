package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/bvorland/profilmanager/internal/cli/themes"
	"github.com/bvorland/profilmanager/internal/core"
	"github.com/bvorland/profilmanager/internal/state"
)

// statuslineStdinCap caps stdin reads so a misbehaving Copilot CLI session
// can never starve us. 1MB is many orders of magnitude beyond any payload
// the spec produces today.
const statuslineStdinCap = 1 << 20

// statuslineStdinDeadline bounds how long we'll wait for stdin. Copilot CLI
// always writes the JSON immediately, so anything longer indicates a bug
// in the caller.
const statuslineStdinDeadline = 2 * time.Second

// statuslineOMPDeadline bounds how long we'll wait for oh-my-posh to render.
// 2 seconds is generous; a stuck renderer would otherwise freeze the
// Copilot CLI prompt redraw.
const statuslineOMPDeadline = 2 * time.Second

// statuslinePayload mirrors the JSON Copilot CLI streams on stdin. Every
// field is pointer/omitempty so partial payloads (early in a session) decode
// cleanly.
type statuslinePayload struct {
	SessionName string `json:"session_name,omitempty"`
	CWD         string `json:"cwd,omitempty"`
	Workspace   *struct {
		CurrentDir string `json:"current_dir,omitempty"`
	} `json:"workspace,omitempty"`
	Model *struct {
		DisplayName string `json:"display_name,omitempty"`
		ID          string `json:"id,omitempty"`
	} `json:"model,omitempty"`
	ContextWindow *struct {
		UsedPercentage       json.Number `json:"used_percentage,omitempty"`
		ContextWindowSize    json.Number `json:"context_window_size,omitempty"`
		LastCallInputTokens  json.Number `json:"last_call_input_tokens,omitempty"`
		TotalInputTokens     json.Number `json:"total_input_tokens,omitempty"`
		TotalOutputTokens    json.Number `json:"total_output_tokens,omitempty"`
		TotalCacheReadTokens json.Number `json:"total_cache_read_tokens,omitempty"`
		TotalCacheWriteTokes json.Number `json:"total_cache_write_tokens,omitempty"`
		TotalReasoningTokens json.Number `json:"total_reasoning_tokens,omitempty"`
	} `json:"context_window,omitempty"`
	Cost *struct {
		TotalPremiumRequests json.Number `json:"total_premium_requests,omitempty"`
		TotalAPIDurationMs   json.Number `json:"total_api_duration_ms,omitempty"`
		TotalDurationMs      json.Number `json:"total_duration_ms,omitempty"`
		TotalLinesAdded      json.Number `json:"total_lines_added,omitempty"`
		TotalLinesRemoved    json.Number `json:"total_lines_removed,omitempty"`
	} `json:"cost,omitempty"`
}

func newStatusLineCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "statusline",
		Short: "Render the pm status line for Copilot CLI (reads session JSON on stdin)",
		Long: `Read a Copilot CLI session JSON payload from stdin and print a profile-aware
status line on stdout. Designed to be wired into Copilot CLI's statusLine.command
config (see ` + "`pm prompt install-statusline`" + `).

This command is invoked on every Copilot CLI refresh, so it is intentionally
crash-proof: malformed JSON, missing oh-my-posh, broken profile data — all
degrade to a minimal text fallback and exit 0.`,
		Args: cobra.NoArgs,
		RunE: runStatusLine,
	}
	cmd.Flags().Bool("render", true, "shell out to oh-my-posh to render the line; --render=false prints PM_SL_* env exports instead")
	cmd.Flags().Bool("no-omp", false, "skip the oh-my-posh shellout entirely and use the built-in plain text renderer")
	return cmd
}

func runStatusLine(cmd *cobra.Command, args []string) (returnedErr error) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprint(cmd.OutOrStdout(), statuslineFallback(nil, nil))
		}
		returnedErr = nil
	}()

	payload := readStatuslinePayload(cmd.InOrStdin())

	profile := loadActiveProfileSafe()
	env := buildStatuslineEnv(payload, profile)

	render, _ := cmd.Flags().GetBool("render")
	noOMP, _ := cmd.Flags().GetBool("no-omp")

	if !render {
		writeStatuslineEnvExports(cmd.OutOrStdout(), env)
		return nil
	}

	if noOMP {
		fmt.Fprint(cmd.OutOrStdout(), statuslineFallback(payload, profile))
		return nil
	}

	themePath, err := ensureStatuslineTheme()
	if err != nil {
		fmt.Fprint(cmd.OutOrStdout(), statuslineFallback(payload, profile))
		return nil
	}

	out, err := runOMP(themePath, env)
	if err != nil || len(bytes.TrimSpace(out)) == 0 {
		fmt.Fprint(cmd.OutOrStdout(), statuslineFallback(payload, profile))
		return nil
	}
	_, _ = cmd.OutOrStdout().Write(out)
	return nil
}

// readStatuslinePayload reads up to statuslineStdinCap bytes from r with a
// soft deadline. Empty / malformed input returns a zero-value struct.
func readStatuslinePayload(r io.Reader) *statuslinePayload {
	if r == nil {
		return &statuslinePayload{}
	}
	data, err := readWithDeadline(r, statuslineStdinCap, statuslineStdinDeadline)
	if err != nil || len(bytes.TrimSpace(data)) == 0 {
		return &statuslinePayload{}
	}
	var p statuslinePayload
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&p); err != nil {
		return &statuslinePayload{}
	}
	return &p
}

// readWithDeadline reads up to cap bytes from r, abandoning the read after
// deadline elapses. Returns whatever bytes arrived in time. Errors from the
// reader other than EOF are returned; deadline expiry is not an error.
func readWithDeadline(r io.Reader, cap int, deadline time.Duration) ([]byte, error) {
	type result struct {
		data []byte
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		buf, err := io.ReadAll(io.LimitReader(r, int64(cap)))
		ch <- result{data: buf, err: err}
	}()
	select {
	case res := <-ch:
		if res.err == io.EOF {
			res.err = nil
		}
		return res.data, res.err
	case <-time.After(deadline):
		return nil, nil
	}
}

// loadActiveProfileSafe returns the active profile for the current session,
// or nil on any error. Errors are swallowed by design — the statusline
// must never crash because of a malformed profile TOML.
func loadActiveProfileSafe() *core.Profile {
	name, _, err := state.GetActiveProfile()
	if err != nil || name == "" {
		return nil
	}
	dir, err := core.ProfilesDir()
	if err != nil {
		return nil
	}
	p, err := core.Load(filepath.Join(dir, name+".toml"))
	if err != nil {
		return nil
	}
	return p
}

// buildStatuslineEnv flattens the payload + profile into the PM_SL_* /
// PM_ACTIVE_PROFILE_* env vars consumed by the omp theme.
func buildStatuslineEnv(p *statuslinePayload, profile *core.Profile) map[string]string {
	env := map[string]string{}
	if p == nil {
		p = &statuslinePayload{}
	}

	if p.Model != nil {
		model := p.Model.DisplayName
		if model == "" {
			model = p.Model.ID
		}
		if model != "" {
			env["PM_SL_MODEL"] = model
		}
	}
	if p.SessionName != "" {
		env["PM_SL_SESSION_NAME"] = p.SessionName
	}
	cwd := p.CWD
	if cwd == "" && p.Workspace != nil {
		cwd = p.Workspace.CurrentDir
	}
	if cwd != "" {
		env["PM_SL_CWD"] = cwd
	}

	if cw := p.ContextWindow; cw != nil {
		setIntFromNumber(env, "PM_SL_CONTEXT_PCT", cw.UsedPercentage)
		setIntFromNumber(env, "PM_SL_CONTEXT_SIZE", cw.ContextWindowSize)
		setIntFromNumber(env, "PM_SL_LAST_CALL_IN", cw.LastCallInputTokens)
		setIntFromNumber(env, "PM_SL_TOKENS_IN", cw.TotalInputTokens)
		setIntFromNumber(env, "PM_SL_TOKENS_OUT", cw.TotalOutputTokens)
		setIntFromNumber(env, "PM_SL_TOKENS_CACHE_READ", cw.TotalCacheReadTokens)
		setIntFromNumber(env, "PM_SL_TOKENS_CACHE_WRITE", cw.TotalCacheWriteTokes)
		setIntFromNumber(env, "PM_SL_TOKENS_REASONING", cw.TotalReasoningTokens)
		if pct, ok := intFromNumber(cw.UsedPercentage); ok {
			env["PM_SL_CONTEXT_GAUGE"] = contextGauge(pct)
			env["PM_SL_CONTEXT_BG"] = contextBG(pct)
		}
	}

	if c := p.Cost; c != nil {
		setIntFromNumber(env, "PM_SL_PREMIUM_REQ", c.TotalPremiumRequests)
		setIntFromNumber(env, "PM_SL_API_DURATION_MS", c.TotalAPIDurationMs)
		setIntFromNumber(env, "PM_SL_DURATION_MS", c.TotalDurationMs)
		setIntFromNumber(env, "PM_SL_LINES_ADDED", c.TotalLinesAdded)
		setIntFromNumber(env, "PM_SL_LINES_REMOVED", c.TotalLinesRemoved)
		if ms, ok := intFromNumber(c.TotalDurationMs); ok && ms > 0 {
			env["PM_SL_DURATION_HUMAN"] = humanDuration(ms)
		}
	}

	if profile != nil {
		env["PM_ACTIVE_PROFILE"] = profile.Name
		if e := core.ColorEmoji(profile.Color); e != "" {
			env["PM_ACTIVE_PROFILE_EMOJI"] = e
		}
		if hex := core.ColorHex(profile.Color); hex != "" {
			env["PM_ACTIVE_PROFILE_BG"] = hex
		} else {
			env["PM_ACTIVE_PROFILE_BG"] = "#0078d4"
		}
		env["PM_ACTIVE_PROFILE_COLOR"] = profile.Color
	}

	if sid := strings.TrimSpace(os.Getenv("PM_SESSION_ID")); sid != "" {
		env["PM_SESSION_ID"] = sid
	}
	return env
}

// setIntFromNumber writes key=N to env if num parses to a non-zero integer.
// Zero-valued counters are skipped so the omp theme's `{{ if .Env.X }}`
// guards collapse those chips cleanly.
func setIntFromNumber(env map[string]string, key string, num json.Number) {
	v, ok := intFromNumber(num)
	if !ok || v == 0 {
		return
	}
	env[key] = strconv.FormatInt(v, 10)
}

func intFromNumber(num json.Number) (int64, bool) {
	s := string(num)
	if s == "" {
		return 0, false
	}
	if v, err := num.Int64(); err == nil {
		return v, true
	}
	if f, err := num.Float64(); err == nil {
		return int64(f), true
	}
	return 0, false
}

// contextGauge renders a 5-block bar (▰▰▰▱▱) for a percentage in [0..100].
// Out-of-range values clamp to the nearest end.
func contextGauge(pct int64) string {
	const blocks = 5
	filled := int(pct * blocks / 100)
	if pct > 0 && filled == 0 {
		filled = 1
	}
	if filled < 0 {
		filled = 0
	}
	if filled > blocks {
		filled = blocks
	}
	if pct >= 100 {
		filled = blocks
	}
	return strings.Repeat("▰", filled) + strings.Repeat("▱", blocks-filled)
}

// contextBG picks the segment background color for the context gauge:
// green <60%, yellow <85%, red ≥85%. Matches a "fuel gauge" mental model.
func contextBG(pct int64) string {
	switch {
	case pct >= 85:
		return "#e53935"
	case pct >= 60:
		return "#fdd835"
	default:
		return "#4caf50"
	}
}

// humanDuration formats a millisecond count as "Xh Ym", "Xm Ys", or "Xs".
// Tuned for human-glanceable status line use, not precise reporting.
func humanDuration(ms int64) string {
	if ms < 0 {
		ms = 0
	}
	totalSec := ms / 1000
	if totalSec < 60 {
		return fmt.Sprintf("%ds", totalSec)
	}
	if totalSec < 3600 {
		m := totalSec / 60
		s := totalSec % 60
		return fmt.Sprintf("%dm %ds", m, s)
	}
	h := totalSec / 3600
	m := (totalSec % 3600) / 60
	return fmt.Sprintf("%dh %dm", h, m)
}

// statuslineThemePath returns where pm stores the embedded statusline theme.
// Per-OS so install + runtime always agree.
//
//	Windows: %LOCALAPPDATA%\profilmanager\statusline.omp.json
//	macOS:   ~/Library/Application Support/profilmanager/statusline.omp.json
//	Linux:   ${XDG_DATA_HOME:-~/.local/share}/profilmanager/statusline.omp.json
func statuslineThemePath() (string, error) {
	switch runtime.GOOS {
	case "windows":
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			return "", fmt.Errorf("LOCALAPPDATA not set")
		}
		return filepath.Join(base, "profilmanager", "statusline.omp.json"), nil
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support", "profilmanager", "statusline.omp.json"), nil
	default:
		if v := os.Getenv("XDG_DATA_HOME"); v != "" {
			return filepath.Join(v, "profilmanager", "statusline.omp.json"), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".local", "share", "profilmanager", "statusline.omp.json"), nil
	}
}

// ensureStatuslineTheme returns the on-disk theme path, writing the
// embedded default if the file is missing.
func ensureStatuslineTheme() (string, error) {
	path, err := statuslineThemePath()
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, themes.StatuslineOMP, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// runOMP invokes `oh-my-posh print primary --config <theme> --shell uni`
// with env vars merged from the current process and our PM_SL_* additions.
// Bounded by statuslineOMPDeadline.
func runOMP(themePath string, extraEnv map[string]string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), statuslineOMPDeadline)
	defer cancel()
	cmd := exec.CommandContext(ctx, "oh-my-posh", "print", "primary", "--config", themePath, "--shell", "uni")
	cmd.Env = mergeEnvForOMP(extraEnv)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return out, nil
}

// mergeEnvForOMP overlays extraEnv onto os.Environ(), with extraEnv
// winning on duplicate keys. Returns the resulting KEY=VALUE slice.
func mergeEnvForOMP(extraEnv map[string]string) []string {
	merged := map[string]string{}
	for _, kv := range os.Environ() {
		if i := strings.IndexByte(kv, '='); i >= 0 {
			merged[kv[:i]] = kv[i+1:]
		}
	}
	for k, v := range extraEnv {
		merged[k] = v
	}
	out := make([]string, 0, len(merged))
	for k, v := range merged {
		out = append(out, k+"="+v)
	}
	sort.Strings(out)
	return out
}

// writeStatuslineEnvExports prints the env map as deterministic KEY=value
// lines for --render=false consumers.
func writeStatuslineEnvExports(w io.Writer, env map[string]string) {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(w, "%s=%s\n", k, env[k])
	}
}

// statuslineFallback renders a minimal one-line status when omp is missing
// or fails. ANSI is suppressed when NO_COLOR is set.
func statuslineFallback(p *statuslinePayload, profile *core.Profile) string {
	noColor := os.Getenv("NO_COLOR") != ""
	var sb strings.Builder
	sb.WriteByte(' ')
	switch {
	case profile != nil:
		if e := core.ColorEmoji(profile.Color); e != "" {
			sb.WriteString(e)
			sb.WriteByte(' ')
		}
		if !noColor {
			if hex := core.ColorHex(profile.Color); hex != "" {
				sb.WriteString(ansiHexBG(hex))
				sb.WriteString(" " + profile.Name + " ")
				sb.WriteString("\x1b[0m")
			} else {
				sb.WriteString(profile.Name)
			}
		} else {
			sb.WriteString(profile.Name)
		}
	case strings.TrimSpace(os.Getenv("PM_SESSION_ID")) != "":
		sb.WriteString("⚠️  no profile")
	default:
		sb.WriteString("● host")
	}
	if p != nil && p.Model != nil {
		model := p.Model.DisplayName
		if model == "" {
			model = p.Model.ID
		}
		if model != "" {
			sb.WriteString(" · 🤖 ")
			sb.WriteString(model)
		}
	}
	sb.WriteByte('\n')
	return sb.String()
}

// ansiHexBG returns the "set 24-bit background to #rrggbb + white fg"
// ANSI sequence used by the fallback renderer.
func ansiHexBG(hex string) string {
	r, g, b, ok := parseHex(hex)
	if !ok {
		return ""
	}
	return fmt.Sprintf("\x1b[48;2;%d;%d;%dm\x1b[97m", r, g, b)
}

func parseHex(hex string) (int, int, int, bool) {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return 0, 0, 0, false
	}
	v, err := strconv.ParseInt(hex, 16, 32)
	if err != nil {
		return 0, 0, 0, false
	}
	return int((v >> 16) & 0xff), int((v >> 8) & 0xff), int(v & 0xff), true
}
