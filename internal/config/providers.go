package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type ProviderConfig struct {
	Type             string `yaml:"type"`
	Host             string `yaml:"host,omitempty"`
	Port             int    `yaml:"port,omitempty"`
	Tailscale        bool   `yaml:"tailscale,omitempty"`
	Repository       string `yaml:"repository,omitempty"`
	SourceRepository string `yaml:"source_repository,omitempty"`
	GitHubToken      string `yaml:"github_token,omitempty"`
}

type GlobalProvidersConfig struct {
	Providers map[string]ProviderConfig `yaml:"providers"`
}

type ProjectConfig struct {
	Provider string `yaml:"provider,omitempty"`
	Build    struct {
		Image     string   `yaml:"image,omitempty"`
		Command   string   `yaml:"command,omitempty"`
		Artifacts []string `yaml:"artifacts,omitempty"`
	} `yaml:"build,omitempty"`
}

func GlobalConfigPath() string {
	home, _ := os.UserHomeDir()
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".packets", "config.yaml")
}

func LoadGlobalProviders() (*GlobalProvidersConfig, error) {
	path := GlobalConfigPath()
	if path == "" {
		return &GlobalProvidersConfig{Providers: make(map[string]ProviderConfig)}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &GlobalProvidersConfig{Providers: make(map[string]ProviderConfig)}, nil
		}
		return nil, fmt.Errorf("read global providers config: %w", err)
	}

	var cfg GlobalProvidersConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal global providers config: %w", err)
	}

	if cfg.Providers == nil {
		cfg.Providers = make(map[string]ProviderConfig)
	}

	return &cfg, nil
}

func SaveGlobalProviders(cfg *GlobalProvidersConfig) error {
	path := GlobalConfigPath()
	if path == "" {
		return fmt.Errorf("cannot determine user home directory")
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal global providers config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write global providers config: %w", err)
	}

	return nil
}

func LoadProjectConfig(dir string) (*ProjectConfig, error) {
	path := filepath.Join(dir, ".packets.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read project config: %w", err)
	}

	var cfg ProjectConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal project config: %w", err)
	}

	return &cfg, nil
}
