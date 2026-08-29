package shim

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"runtime"
	"testing"

	"github.com/waris4ly/packets/pkg/apitypes"
)

func noopCommand() (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/c", "exit", "/b", "0"}
	}
	return "true", nil
}

func TestFallbackRunner(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	runner := NewFallbackRunner(logger)

	var remoteCalled bool
	remoteCall := func(ctx context.Context) error {
		remoteCalled = true
		return errors.New("remote failed")
	}

	cmd, args := noopCommand()
	def := apitypes.ToolchainDef{
		Name:         "test",
		LocalCommand: cmd,
		DefaultArgs:  args,
	}

	err := runner.ExecuteWithFallback(context.Background(), def, ".", nil, remoteCall)
	if err != nil {
		t.Errorf("expected local fallback to succeed, got %v", err)
	}
	if !remoteCalled {
		t.Error("expected remote strategy to be called")
	}
}

func TestFallbackRunner_Timeout(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	runner := NewFallbackRunner(logger)

	var remoteCalled bool
	remoteCall := func(ctx context.Context) error {
		remoteCalled = true
		return context.DeadlineExceeded
	}

	cmd, args := noopCommand()
	def := apitypes.ToolchainDef{
		Name:         "test",
		LocalCommand: cmd,
		DefaultArgs:  args,
	}

	err := runner.ExecuteWithFallback(context.Background(), def, ".", nil, remoteCall)
	if err != nil {
		t.Errorf("expected local fallback to succeed after timeout, got %v", err)
	}
	if !remoteCalled {
		t.Error("expected remote strategy to be called")
	}
}
