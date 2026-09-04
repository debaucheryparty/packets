package android

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
)

type DisplayStatus struct {
	Running bool
	Backend string
	PID     int
}
type DisplayQuality string

const (
	QualityLow    DisplayQuality = "low"
	QualityMedium DisplayQuality = "medium"
	QualityHigh   DisplayQuality = "high"
)

func QualityToOpts(q DisplayQuality) (maxSize, bitrateBps, maxFPS int) {
	switch q {
	case QualityLow:
		return 800, 2_000_000, 24
	case QualityMedium:
		return 1280, 4_000_000, 30
	case QualityHigh:
		return 1920, 8_000_000, 60
	default:
		return 1280, 4_000_000, 30
	}
}

type DisplayBackend interface {
	Start(ctx context.Context, opts DisplayOpts) error
	Stop(ctx context.Context) error
	Status(ctx context.Context) (DisplayStatus, error)
}
type DisplayOpts struct {
	ADBHost    string
	ADBPort    int
	Serial     string
	MaxSizePx  int
	BitrateBps int
	MaxFPS     int
	Stderr     io.Writer
}

type ScrcpyDisplayBackend struct {
	logger *slog.Logger
	cmd    *exec.Cmd
}

func NewScrcpyDisplayBackend(logger *slog.Logger) *ScrcpyDisplayBackend {
	return &ScrcpyDisplayBackend{logger: logger}
}

func (s *ScrcpyDisplayBackend) Start(ctx context.Context, opts DisplayOpts) error {
	port := opts.ADBPort
	if port == 0 {
		port = 5037
	}

	args := []string{
		"--serial", opts.Serial,
	}

	if opts.MaxSizePx > 0 {
		args = append(args, "--max-size", fmt.Sprintf("%d", opts.MaxSizePx))
	}
	if opts.BitrateBps > 0 {
		args = append(args, "--video-bit-rate", fmt.Sprintf("%d", opts.BitrateBps))
	}
	if opts.MaxFPS > 0 {
		args = append(args, "--max-fps", fmt.Sprintf("%d", opts.MaxFPS))
	}
	s.cmd = exec.CommandContext(ctx, "scrcpy", args...)
	s.cmd.Env = append(s.cmd.Environ(),
		fmt.Sprintf("ADB_SERVER_SOCKET=tcp:%s:%d", opts.ADBHost, port),
	)
	if opts.Stderr != nil {
		s.cmd.Stderr = opts.Stderr
	}

	s.logger.InfoContext(ctx, "starting scrcpy",
		slog.String("serial", opts.Serial),
		slog.String("host", opts.ADBHost),
		slog.Int("port", port),
	)

	if err := s.cmd.Start(); err != nil {
		return fmt.Errorf("scrcpy start: %w", err)
	}

	return s.cmd.Wait()
}

func (s *ScrcpyDisplayBackend) Stop(_ context.Context) error {
	if s.cmd != nil && s.cmd.Process != nil {
		return s.cmd.Process.Kill()
	}
	return nil
}

func (s *ScrcpyDisplayBackend) Status(_ context.Context) (DisplayStatus, error) {
	if s.cmd != nil && s.cmd.Process != nil && s.cmd.ProcessState == nil {
		return DisplayStatus{
			Running: true,
			Backend: "scrcpy",
			PID:     s.cmd.Process.Pid,
		}, nil
	}
	return DisplayStatus{Running: false, Backend: "scrcpy"}, nil
}

type EmulatorGrpcDisplayBackend struct {
	logger *slog.Logger
}

func NewEmulatorGrpcDisplayBackend(logger *slog.Logger) *EmulatorGrpcDisplayBackend {
	return &EmulatorGrpcDisplayBackend{logger: logger}
}

func (g *EmulatorGrpcDisplayBackend) Start(ctx context.Context, opts DisplayOpts) error {
	return fmt.Errorf("emulator-grpc display backend is experimental; please use scrcpy backend")
}
func (g *EmulatorGrpcDisplayBackend) Stop(_ context.Context) error {
	return nil
}

func (g *EmulatorGrpcDisplayBackend) Status(_ context.Context) (DisplayStatus, error) {
	return DisplayStatus{Running: false, Backend: "emulator-grpc"}, nil
}
