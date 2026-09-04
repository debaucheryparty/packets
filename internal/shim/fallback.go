package shim

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"time"

	"github.com/debaucheryparty/packets/pkg/apitypes"
)

var (
	ErrRemoteUnreachable = errors.New("remote backend unreachable")
	ErrRemoteTimeout     = errors.New("remote backend timed out")
)

type FallbackRunner struct {
	logger *slog.Logger
}

func NewFallbackRunner(logger *slog.Logger) *FallbackRunner {
	return &FallbackRunner{logger: logger}
}

func (f *FallbackRunner) ExecuteWithFallback(ctx context.Context, def apitypes.ToolchainDef, dir string, args []string, remoteCall func(context.Context) error) error {
	remoteCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	err := remoteCall(remoteCtx)
	if err == nil {
		return nil
	}

	f.logFallback(ctx, def, err)

	return f.executeLocal(ctx, def, dir, args)
}

func (f *FallbackRunner) logFallback(ctx context.Context, def apitypes.ToolchainDef, err error) {
	reason := err.Error()
	if errors.Is(err, context.DeadlineExceeded) {
		reason = ErrRemoteTimeout.Error()
	}

	f.logger.WarnContext(ctx, "fallback triggered",
		slog.String("job_id", ""),
		slog.String("toolchain", string(def.Name)),
		slog.String("attempted_backend", def.Backend.String()),
		slog.String("reason", reason),
		slog.String("fallback_backend", "local"),
	)
}

func (f *FallbackRunner) executeLocal(ctx context.Context, def apitypes.ToolchainDef, dir string, args []string) error {
	f.logger.InfoContext(ctx, "executing build locally", slog.String("command", def.LocalCommand))

	runArgs := args
	if len(runArgs) == 0 {
		runArgs = def.DefaultArgs
	}

	cmd := exec.CommandContext(ctx, def.LocalCommand, runArgs...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("local fallback execution failed: %w", err)
	}

	return nil
}
