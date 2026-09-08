package project

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type ComponentConfig struct {
	Type     string            `json:"type"`
	Name     string            `json:"name,omitempty"`
	Path     string            `json:"path,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type Config struct {
	ProjectID   string            `json:"project_id"`
	Name        string            `json:"name,omitempty"`
	Description string            `json:"description,omitempty"`
	Components  []ComponentConfig `json:"components,omitempty"`
}

// LoadConfig reads .packets/project.json from the given project root.
func LoadConfig(dir string) (*Config, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		absDir = dir
	}
	cfgFile := filepath.Join(absDir, ".packets", "project.json")
	data, err := os.ReadFile(cfgFile)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// SaveConfig persists the given Config to .packets/project.json.
func SaveConfig(dir string, cfg *Config) error {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		absDir = dir
	}
	packetsDir := filepath.Join(absDir, ".packets")
	if err := os.MkdirAll(packetsDir, 0o755); err != nil {
		return err
	}
	cfgFile := filepath.Join(packetsDir, "project.json")
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cfgFile, append(data, '\n'), 0o644)
}

// ResolveProjectID deterministically identifies the project using:
// 1. Explicit .packets/project.json
// 2. Git remote origin URL
// 3. Normalized repository root absolute path hash
func ResolveProjectID(dir string) string {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		absDir = dir
	}

	// 1. Check .packets/project.json
	cfgFile := filepath.Join(absDir, ".packets", "project.json")
	if data, err := os.ReadFile(cfgFile); err == nil {
		var cfg Config
		if err := json.Unmarshal(data, &cfg); err == nil && cfg.ProjectID != "" {
			return sanitizeID(cfg.ProjectID)
		}
	}

	// 2. Check git remote origin
	cmd := exec.Command("git", "config", "--get", "remote.origin.url")
	cmd.Dir = absDir
	if out, err := cmd.Output(); err == nil {
		remote := strings.TrimSpace(string(out))
		if remote != "" {
			h := sha256.Sum256([]byte(remote))
			return "git-" + hex.EncodeToString(h[:])[:12]
		}
	}

	// 3. Fallback: Hash the clean absolute project root
	h := sha256.Sum256([]byte(filepath.Clean(absDir)))
	base := filepath.Base(absDir)
	return sanitizeID(base) + "-" + hex.EncodeToString(h[:])[:8]
}

func sanitizeID(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	res := strings.Trim(b.String(), "-")
	if res == "" {
		return "project"
	}
	return res
}
