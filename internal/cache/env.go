package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

type EnvFingerprint struct {
	Hash    string            `json:"hash"`
	Details map[string]string `json:"details"`
}

func FingerprintEnv(ctx context.Context) (*EnvFingerprint, error) {
	probes := []struct {
		name string
		cmd  []string
	}{
		{"go", []string{"go", "version"}},
		{"rust", []string{"rustc", "--version"}},
		{"cargo", []string{"cargo", "--version"}},
		{"java", []string{"java", "-version"}},
		{"javac", []string{"javac", "-version"}},
		{"node", []string{"node", "--version"}},
		{"npm", []string{"npm", "--version"}},
		{"python3", []string{"python3", "--version"}},
		{"pip3", []string{"pip3", "--version"}},
		{"cmake", []string{"cmake", "--version"}},
		{"ninja", []string{"ninja", "--version"}},
		{"west", []string{"west", "--version"}},
		{"dtc", []string{"dtc", "--version"}},
		{"adb", []string{"adb", "version"}},
		{"docker", []string{"docker", "--version"}},
		{"gradle", []string{"gradle", "--version"}},
	}

	details := make(map[string]string, len(probes))
	for _, p := range probes {
		out, err := exec.CommandContext(ctx, p.cmd[0], p.cmd[1:]...).CombinedOutput()
		if err != nil {
			continue
		}
		line := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
		details[p.name] = strings.TrimSpace(line)
	}

	hash, err := stableHash(details)
	if err != nil {
		return nil, fmt.Errorf("FingerprintEnv hash: %w", err)
	}
	return &EnvFingerprint{Hash: hash, Details: details}, nil
}

func stableHash(m map[string]string) (string, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
