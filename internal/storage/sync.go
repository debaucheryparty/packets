package storage

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
)

type MutagenSync struct {
	logger    *slog.Logger
	localPath string
	remoteURI string
}

func NewMutagenSync(logger *slog.Logger, localPath, remoteURI string) *MutagenSync {
	return &MutagenSync{
		logger:    logger,
		localPath: localPath,
		remoteURI: remoteURI,
	}
}

func (m *MutagenSync) Start(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "mutagen", "sync", "create",
		"--name", "packets-sync",
		m.localPath,
		m.remoteURI,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("MutagenSync.Start: %w\noutput: %s", err, string(output))
	}

	m.logger.InfoContext(ctx, "mutagen sync started",
		slog.String("local", m.localPath),
		slog.String("remote", m.remoteURI),
	)
	return nil
}

func (m *MutagenSync) Stop(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "mutagen", "sync", "terminate", "packets-sync")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("MutagenSync.Stop: %w\noutput: %s", err, string(output))
	}
	return nil
}

func (m *MutagenSync) Status(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "mutagen", "sync", "list", "--name", "packets-sync")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("MutagenSync.Status: %w", err)
	}
	return string(output), nil
}
