package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os/exec"
)

func GenerateCacheKey(ctx context.Context, dir, toolchain, runner, sourceMode, snapshotRef string) (string, error) {
	h := sha256.New()
	h.Write([]byte(toolchain))
	h.Write([]byte(runner))
	h.Write([]byte(sourceMode))

	if snapshotRef != "" {
		h.Write([]byte(snapshotRef))
		return hex.EncodeToString(h.Sum(nil)), nil
	}

	cmd := exec.CommandContext(ctx, "git", "ls-tree", "-r", "HEAD")
	cmd.Dir = dir

	output, err := cmd.Output()
	if err != nil {
		return fallbackHash(dir, toolchain, runner, sourceMode), nil
	}

	h.Write(output)

	statusCmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	statusCmd.Dir = dir
	if statusOutput, err := statusCmd.Output(); err == nil {
		h.Write(statusOutput)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

func fallbackHash(dir, toolchain, runner, sourceMode string) string {
	h := sha256.New()
	h.Write([]byte(toolchain))
	h.Write([]byte(runner))
	h.Write([]byte(sourceMode))
	h.Write([]byte(dir))
	return hex.EncodeToString(h.Sum(nil))
}
