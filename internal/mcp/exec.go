package mcp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/bvorland/profilmanager/internal/core"
	"github.com/bvorland/profilmanager/internal/providers"
	"github.com/bvorland/profilmanager/internal/secrets"
	"github.com/bvorland/profilmanager/internal/state"
)

// ExecRequest is the typed payload for exec_with_profile.
type ExecRequest struct {
	// Profile is the profile name whose env to apply. Empty falls back
	// to the session's active profile (see [state.GetActiveProfile]).
	Profile string
	// Command is the executable basename. Must pass [IsAllowedCommand].
	Command string
	// Args are the arguments passed to the command. Never interpreted
	// by a shell.
	Args []string
	// TimeoutSeconds, if > 0, overrides [Config.DefaultExecTimeout].
	// Clamped to [Config.MaxExecTimeout].
	TimeoutSeconds int
	// Stdin is optional input fed to the child's stdin. Treated as
	// non-secret bytes (no redaction performed on inbound stdin).
	Stdin []byte
}

// ExecResult is what exec_with_profile returns to the agent.
//
// Stdout/Stderr have been redacted: every occurrence of a resolved
// secret value is replaced with [redactedMarker] before the bytes leave
// the pm process.
type ExecResult struct {
	ExitCode   int    `json:"exit_code"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	DurationMs int64  `json:"duration_ms"`
	// TimedOut is true when the child was killed because the timeout
	// expired. ExitCode is the synthesized -1 in that case.
	TimedOut bool `json:"timed_out,omitempty"`
	// Truncated is true when stdout or stderr exceeded
	// [Config.MaxOutputBytes] and was cut.
	Truncated bool `json:"truncated,omitempty"`
	// Command/Args echoed back so an agent (and the audit log) can
	// confirm what actually ran. Args are NOT redacted here — they
	// were caller-supplied and the agent already saw them.
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
}

// ErrCommandNotAllowed is returned by [Exec] when the requested command
// is not in [Config.AllowedCommands].
var ErrCommandNotAllowed = errors.New("command not allowed by mcp.allowed_commands")

// ErrNoProfile is returned by [Exec] when the request has no profile
// and the session has no active profile.
var ErrNoProfile = errors.New("no profile specified and no active profile for this session")

// Exec runs req.Command with req.Profile's env applied. This is the
// guardrailed path for agents to invoke az / azd / gh / kubectl / git.
//
// Guardrails (architecture memo + iron rule):
//
//  1. Command allowlist enforcement (returns [ErrCommandNotAllowed]).
//  2. No shell — uses [exec.CommandContext] with explicit argv.
//  3. Timeout — default 60s, hard cap [Config.MaxExecTimeout].
//  4. Audit log to <StateDir>/audit/mcp.log (newline-JSON; no values,
//     no full stdout — only a redacted 256-byte preview).
//  5. Output redaction — every resolved secret value is replaced with
//     [redactedMarker] in stdout/stderr before bytes leave pm.
//  6. Resolved secrets are zeroed and dropped before this function
//     returns.
//
// Exec NEVER writes files itself — the child process may, but pm only
// observes exit code + (redacted) bytes.
func Exec(ctx context.Context, req ExecRequest) (*ExecResult, error) {
	start := time.Now()

	// Make sure the secret resolvers (op, wincred, dotenv) are
	// installed. NewServer also does this, but Exec is callable
	// standalone (and is in tests), so we belt-and-brace here.
	// RegisterBuiltins is idempotent.
	secrets.RegisterBuiltins()

	// 1. Resolve profile name.
	profileName := strings.TrimSpace(req.Profile)
	if profileName == "" {
		name, _, err := state.GetActiveProfile()
		if err != nil {
			logExec(execAuditCtx{req: req}, "error", nil, 0, time.Since(start), err.Error())
			return nil, err
		}
		if name == "" {
			logExec(execAuditCtx{req: req}, "error", nil, 0, time.Since(start), ErrNoProfile.Error())
			return nil, ErrNoProfile
		}
		profileName = name
	}

	// 2. Allowlist check — before any process touches disk.
	if !IsAllowedCommand(req.Command) {
		err := fmt.Errorf("%w: %s", ErrCommandNotAllowed, req.Command)
		logExec(execAuditCtx{req: req, profile: profileName, redactor: nil},
			"denied", nil, 0, time.Since(start), err.Error())
		return nil, err
	}

	// 3. Load profile.
	profilePath, err := core.ProfilePath(profileName)
	if err != nil {
		logExec(execAuditCtx{req: req, profile: profileName, redactor: nil},
			"error", nil, 0, time.Since(start), err.Error())
		return nil, err
	}
	prof, err := core.Load(profilePath)
	if err != nil {
		logExec(execAuditCtx{req: req, profile: profileName, redactor: nil},
			"error", nil, 0, time.Since(start), err.Error())
		return nil, err
	}

	// 4. Build env + redactor.
	redactor := NewRedactor()
	defer redactor.Reset()
	envBlock, err := buildProfileEnv(ctx, prof, redactor)
	if err != nil {
		logExec(execAuditCtx{req: req, profile: profileName, redactor: redactor},
			"error", nil, 0, time.Since(start), err.Error())
		return nil, err
	}

	// 5. Decide timeout.
	c := GetConfig()
	timeout := c.DefaultExecTimeout
	if req.TimeoutSeconds > 0 {
		timeout = time.Duration(req.TimeoutSeconds) * time.Second
	}
	if timeout > c.MaxExecTimeout {
		timeout = c.MaxExecTimeout
	}

	// 6. Spawn child with explicit argv. No shell, ever. The allowlist
	// guarantees req.Command is a bare basename (no path separators) —
	// we resolve it via PATH explicitly here so the operator's PATH is
	// the authority and the audit log records the absolute path that
	// actually ran (forensics) rather than the unresolved name.
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resolved, lookErr := exec.LookPath(req.Command)
	if lookErr != nil {
		err := fmt.Errorf("look up %s on PATH: %w", req.Command, lookErr)
		logExec(execAuditCtx{req: req, profile: profileName, redactor: redactor},
			"error", nil, 0, time.Since(start), err.Error())
		return nil, err
	}

	cmd := exec.CommandContext(execCtx, resolved, req.Args...)
	cmd.Env = envBlock
	if len(req.Stdin) > 0 {
		cmd.Stdin = bytes.NewReader(req.Stdin)
	}

	var stdoutBuf, stderrBuf cappedBuffer
	stdoutBuf.cap = c.MaxOutputBytes
	stderrBuf.cap = c.MaxOutputBytes
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	runErr := cmd.Run()
	duration := time.Since(start)

	// 7. Determine exit / timeout / error path.
	exitCode := 0
	timedOut := false
	if runErr != nil {
		if errors.Is(execCtx.Err(), context.DeadlineExceeded) {
			exitCode = -1
			timedOut = true
		} else if ee, ok := runErr.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		} else {
			// Spawn failure (binary not found etc.) — propagate as a
			// hard error AND audit it.
			logExec(execAuditCtx{req: req, profile: profileName, redactor: redactor},
				"error", nil, 0, duration, runErr.Error())
			return nil, fmt.Errorf("run %s: %w", req.Command, runErr)
		}
	}

	// 8. Redact stdout/stderr before they leave the process.
	redactedStdout := redactor.Redact(stdoutBuf.Bytes())
	redactedStderr := redactor.Redact(stderrBuf.Bytes())

	result := &ExecResult{
		ExitCode:   exitCode,
		Stdout:     string(redactedStdout),
		Stderr:     string(redactedStderr),
		DurationMs: duration.Milliseconds(),
		TimedOut:   timedOut,
		Truncated:  stdoutBuf.truncated || stderrBuf.truncated,
		Command:    req.Command,
		Args:       append([]string(nil), req.Args...),
	}

	// 9. Audit — args are redacted (in case an agent passed a literal
	// secret as a flag value), output preview is redacted + capped.
	auditResult := "ok"
	if timedOut {
		auditResult = "error"
	} else if exitCode != 0 {
		// Non-zero exit is still "ok" from an authorization perspective
		// — the command ran, the child decided to fail. Don't poison
		// the audit log with "error" for normal CLI exit codes.
		auditResult = "ok"
	}
	logExec(execAuditCtx{req: req, profile: profileName, redactor: redactor},
		auditResult, redactedStdout, exitCode, duration, errString(runErr, timedOut))

	return result, nil
}

// execAuditCtx bundles the inputs the audit helper needs so we don't
// thread half-a-dozen parameters through every call site.
type execAuditCtx struct {
	req      ExecRequest
	profile  string
	redactor *Redactor
}

// logExec writes a single audit entry for an exec_with_profile call.
// The args slice is redacted before serialization; output preview is
// truncated to the first 256 redacted bytes.
func logExec(ctx execAuditCtx, result string, redactedStdout []byte, exitCode int, dur time.Duration, errMsg string) {
	args := ctx.req.Args
	if ctx.redactor != nil {
		args = ctx.redactor.RedactArgs(args)
	}
	preview := firstNRedacted(ctx.redactor, redactedStdout, 256)
	logEntry(AuditEntry{
		Profile:       ctx.profile,
		Tool:          "exec_with_profile",
		Command:       ctx.req.Command,
		Args:          args,
		Result:        result,
		ExitCode:      exitCode,
		DurationMs:    dur.Milliseconds(),
		Error:         errMsg,
		OutputPreview: preview,
	})
}

// errString flattens the run error / timeout flag into a single audit
// string. Empty for the success path.
func errString(runErr error, timedOut bool) string {
	if timedOut {
		return "timeout"
	}
	if runErr == nil {
		return ""
	}
	if _, ok := runErr.(*exec.ExitError); ok {
		// A non-zero exit isn't really an error worth narrating in the
		// audit log; the ExitCode field already captures it.
		return ""
	}
	return runErr.Error()
}

// buildProfileEnv composes the child process env block: provider Apply
// outputs + profile.Env entries (literals + resolved refs). Every
// resolved secret is added to the redactor so its bytes will be
// scrubbed from stdout/stderr before the result is returned.
//
// Provider env wins over profile.Env on key collision (the profile's
// explicit env is the operator's override). Both win over os.Environ
// because the operator wants the profile to "be" the environment for
// this call.
func buildProfileEnv(ctx context.Context, p *core.Profile, redactor *Redactor) ([]string, error) {
	final := map[string]string{}
	for _, kv := range os.Environ() {
		if i := strings.IndexByte(kv, '='); i > 0 {
			final[kv[:i]] = kv[i+1:]
		}
	}

	// Provider env (az, azd, gh, …). Skip providers that aren't
	// installed; their Apply may key off existence checks.
	for _, prov := range providers.All() {
		if !prov.Available() {
			continue
		}
		env, err := prov.Apply(p)
		if err != nil {
			return nil, fmt.Errorf("apply provider %s: %w", prov.Name(), err)
		}
		for k, v := range env {
			final[k] = v
		}
	}

	// Profile env entries. Refs go through the secret resolver; the
	// resolved value goes (a) into the child env block, (b) into the
	// redactor, (c) is then zeroed.
	for _, e := range p.Env {
		if e.Value != "" {
			final[e.Key] = e.Value
			continue
		}
		if e.Ref == "" {
			continue
		}
		sec, rerr := secrets.ResolveRef(ctx, e.Ref)
		if rerr != nil {
			// A miss on one ref must not poison the whole exec — we
			// still want az/azd to work even if one optional secret is
			// missing. Skip the env entry and continue; the audit log
			// already recorded the resolve attempt (secrets package).
			continue
		}
		// Take a *copy* of the revealed bytes so we can Zero the
		// underlying Secret immediately, but still keep the value
		// alive for env injection and redaction.
		raw := sec.RevealBytes()
		valCopy := make([]byte, len(raw))
		copy(valCopy, raw)
		sec.Zero()

		final[e.Key] = string(valCopy)
		// The redactor takes ownership of valCopy and will Zero it on
		// Reset. We deliberately do NOT keep a separate reference here.
		redactor.Add(valCopy)
	}

	keys := make([]string, 0, len(final))
	for k := range final {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(final))
	for _, k := range keys {
		out = append(out, k+"="+final[k])
	}
	return out, nil
}

// cappedBuffer is a [bytes.Buffer]-shaped sink with a maximum capacity.
// Once cap bytes have been written, further Write calls are accepted
// (so the child doesn't block on EPIPE) but the excess is discarded
// and truncated is set to true.
type cappedBuffer struct {
	buf       bytes.Buffer
	cap       int
	truncated bool
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if c.cap <= 0 {
		return c.buf.Write(p)
	}
	remain := c.cap - c.buf.Len()
	if remain <= 0 {
		c.truncated = true
		return len(p), nil
	}
	if len(p) <= remain {
		return c.buf.Write(p)
	}
	if _, err := c.buf.Write(p[:remain]); err != nil {
		return 0, err
	}
	c.truncated = true
	return len(p), nil
}

func (c *cappedBuffer) Bytes() []byte {
	if !c.truncated {
		return c.buf.Bytes()
	}
	const notice = "\n<TRUNCATED — output exceeded mcp.max_output_bytes>\n"
	out := make([]byte, 0, c.buf.Len()+len(notice))
	out = append(out, c.buf.Bytes()...)
	out = append(out, notice...)
	return out
}

// Compile-time interface satisfaction check so cappedBuffer can be
// passed to exec.Cmd.Stdout/Stderr.
var _ io.Writer = (*cappedBuffer)(nil)
