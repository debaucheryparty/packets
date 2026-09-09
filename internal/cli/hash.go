package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os/exec"
)

type CacheKeyInputs struct {
	ProjectID   string
	Dir         string
	Toolchain   string
	Runner      string
	SourceMode  string
	SnapshotRef string
	CommandArgs []string
}

func GenerateCacheKey(ctx context.Context, in CacheKeyInputs) (string, error) {
	h := sha256.New()
	h.Write([]byte(in.ProjectID))
	h.Write([]byte("\x00"))
	h.Write([]byte(in.Toolchain))
	h.Write([]byte("\x00"))
	h.Write([]byte(in.Runner))
	h.Write([]byte("\x00"))
	h.Write([]byte(in.SourceMode))
	h.Write([]byte("\x00"))

	for _, arg := range in.CommandArgs {
		h.Write([]byte(arg))
		h.Write([]byte("\x00"))
	}

	if in.SnapshotRef != "" {
		h.Write([]byte(in.SnapshotRef))
		return hex.EncodeToString(h.Sum(nil)), nil
	}

	cmd := exec.CommandContext(ctx, "git", "ls-tree", "-r", "HEAD")
	cmd.Dir = in.Dir

	output, err := cmd.Output()
	if err != nil {
		return fallbackHash(in), nil
	}

	h.Write(output)

	statusCmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	statusCmd.Dir = in.Dir
	if statusOutput, err := statusCmd.Output(); err == nil {
		h.Write(statusOutput)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

func fallbackHash(in CacheKeyInputs) string {
	h := sha256.New()
	h.Write([]byte(in.ProjectID))
	h.Write([]byte("\x00"))
	h.Write([]byte(in.Toolchain))
	h.Write([]byte("\x00"))
	h.Write([]byte(in.Runner))
	h.Write([]byte("\x00"))
	h.Write([]byte(in.SourceMode))
	h.Write([]byte("\x00"))
	h.Write([]byte(in.Dir))
	h.Write([]byte("\x00"))
	for _, arg := range in.CommandArgs {
		h.Write([]byte(arg))
		h.Write([]byte("\x00"))
	}
	return hex.EncodeToString(h.Sum(nil))
}
