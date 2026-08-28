package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os/exec"
)

// GenerateCacheKey generates a cache key for the workspace using git if available
func GenerateCacheKey(ctx context.Context, dir string, toolchain string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "ls-tree", "-r", "HEAD")
	cmd.Dir = dir

	output, err := cmd.Output()
	if err != nil {
		return fallbackHash(dir, toolchain), nil
	}

	h := sha256.New()
	h.Write([]byte(toolchain))
	h.Write(output)

	statusCmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	statusCmd.Dir = dir
	if statusOutput, err := statusCmd.Output(); err == nil {
		h.Write(statusOutput)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

func fallbackHash(dir, toolchain string) string {
	h := sha256.New()
	h.Write([]byte(toolchain))
	h.Write([]byte(dir))
	return hex.EncodeToString(h.Sum(nil))
}
