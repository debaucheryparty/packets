package shim

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"

	"github.com/waris4ly/packets/pkg/apitypes"
)

func TestFallbackRunner(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	runner := NewFallbackRunner(logger)

	var remoteCalled bool
	remoteCall := func(ctx context.Context) error {
		remoteCalled = true
		return errors.New("remote failed")
	}

	def := apitypes.ToolchainDef{
		Name:         "test",
		LocalCommand: "true", // standard unix true
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

	def := apitypes.ToolchainDef{
		Name:         "test",
		LocalCommand: "true",
	}

	err := runner.ExecuteWithFallback(context.Background(), def, ".", nil, remoteCall)
	if err != nil {
		t.Errorf("expected local fallback to succeed after timeout, got %v", err)
	}
	if !remoteCalled {
		t.Error("expected remote strategy to be called")
	}
}
