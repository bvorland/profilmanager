package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"time"

	"github.com/spf13/cobra"

	"github.com/bvorland/profilmanager/internal/core"
	"github.com/bvorland/profilmanager/internal/runner"
)

func runChildInProfile(cmd *cobra.Command, profile *core.Profile, child []string, timeout time.Duration, notFoundMessage string) error {
	ctx, cancel := context.WithCancel(cmd.Context())
	if ctx.Err() != nil {
		ctx = context.Background()
	}
	defer cancel()
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	plan, cleanup, err := runner.Compose(ctx, profile, runner.ComposeOpts{ResolveSecrets: true})
	defer cleanup()
	if err != nil {
		return emitError(cmd, err)
	}
	if len(plan.ProviderErrors) > 0 {
		for _, pe := range plan.ProviderErrors {
			fmt.Fprintln(cmd.ErrOrStderr(), styleWarn.Render("warn:"), pe.Error())
		}
	}

	exe, lookErr := exec.LookPath(child[0])
	if lookErr != nil {
		if notFoundMessage != "" {
			return emitError(cmd, errInvalidUsage("%s", notFoundMessage))
		}
		return emitError(cmd, errInvalidUsage("command %q not found on PATH: %v", child[0], lookErr))
	}

	c := exec.CommandContext(ctx, exe, child[1:]...)
	c.Env = runner.EnvSlice(os.Environ(), plan.Env)
	c.Stdin = cmd.InOrStdin()
	c.Stdout = cmd.OutOrStdout()
	c.Stderr = cmd.ErrOrStderr()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	defer signal.Stop(sigCh)
	go func() {
		select {
		case <-ctx.Done():
		case <-sigCh:
			cancel()
		}
	}()

	if err := c.Start(); err != nil {
		return emitError(cmd, fmt.Errorf("start %s: %w", child[0], err))
	}

	cleanup()
	cleanup = func() {}

	waitErr := c.Wait()

	if ctxErr := ctx.Err(); errors.Is(ctxErr, context.DeadlineExceeded) {
		return emitError(cmd, WithExitCode(ExitError, fmt.Errorf("child %s exceeded --timeout=%s", child[0], timeout)))
	}

	if waitErr == nil {
		return nil
	}
	var ee *exec.ExitError
	if errors.As(waitErr, &ee) {
		code := ee.ExitCode()
		if code < 0 {
			code = ExitError
		}
		return WithExitCode(code, fmt.Errorf("%s exited with code %d", child[0], code))
	}
	return emitError(cmd, fmt.Errorf("wait %s: %w", child[0], waitErr))
}
