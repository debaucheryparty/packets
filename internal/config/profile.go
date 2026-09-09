package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Profile struct {
	ServerAddr            string `yaml:"server_addr" json:"server_addr"`
	AuthToken             string `yaml:"auth_token" json:"auth_token"`
	DefaultRunner         string `yaml:"default_runner,omitempty" json:"default_runner,omitempty"`
	TLSEnabled            bool   `yaml:"tls_enabled,omitempty" json:"tls_enabled,omitempty"`
	TLSCertFile           string `yaml:"tls_cert_file,omitempty" json:"tls_cert_file,omitempty"`
	TLSKeyFile            string `yaml:"tls_key_file,omitempty" json:"tls_key_file,omitempty"`
	TLSCAFile             string `yaml:"tls_ca_file,omitempty" json:"tls_ca_file,omitempty"`
	TLSInsecureSkipVerify bool   `yaml:"tls_insecure_skip_verify,omitempty" json:"tls_insecure_skip_verify,omitempty"`
}

func GetConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("user home dir: %w", err)
	}
	return filepath.Join(home, ".packets"), nil
}

func GetProfilePath() (string, error) {
	dir, err := GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.yaml"), nil
}

func LoadProfile() (*Profile, error) {
	if data, err := os.ReadFile(filepath.Join(".packets", "config.yaml")); err == nil {
		var p Profile
		if err := yaml.Unmarshal(data, &p); err == nil {
			return &p, nil
		}
	}

	path, err := GetProfilePath()
	if err != nil {
		return &Profile{}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Profile{}, nil
		}
		return nil, fmt.Errorf("read profile: %w", err)
	}

	var p Profile
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse profile YAML: %w", err)
	}

	return &p, nil
}

func SaveProfile(p *Profile) error {
	dir, err := GetConfigDir()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	data, err := yaml.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshal profile: %w", err)
	}

	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write config file: %w", err)
	}

	return nil
}
