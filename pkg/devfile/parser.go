package devfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

var (
	ErrEmptyDevfile     = errors.New("devfile cannot be empty")
	ErrInvalidPort      = errors.New("invalid port number, must be between 1 and 65535")
	ErrEmptyServiceName = errors.New("service name cannot be empty")
)

var CandidateFilenames = []string{
	"packets.yaml",
	"packets.yml",
	".packets.yaml",
	".packets.yml",
	"devfile.yaml",
	"devfile.yml",
}

func Parse(data []byte) (*Devfile, error) {
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, ErrEmptyDevfile
	}

	var df Devfile
	if err := yaml.Unmarshal(data, &df); err != nil {
		return nil, fmt.Errorf("unmarshal devfile: %w", err)
	}

	if err := Validate(&df); err != nil {
		return nil, err
	}

	return &df, nil
}

func ParseFile(path string) (*Devfile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read devfile: %w", err)
	}
	return Parse(data)
}

func Find(dir string) (string, bool) {
	for _, name := range CandidateFilenames {
		candidate := filepath.Join(dir, name)
		if fi, err := os.Stat(candidate); err == nil && !fi.IsDir() {
			return candidate, true
		}
	}
	return "", false
}

func Load(dir string) (*Devfile, error) {
	path, found := Find(dir)
	if !found {
		return nil, nil
	}
	return ParseFile(path)
}

func Validate(df *Devfile) error {
	if df == nil {
		return ErrEmptyDevfile
	}

	for _, p := range df.Ports {
		if p <= 0 || p > 65535 {
			return fmt.Errorf("%w: %d", ErrInvalidPort, p)
		}
	}

	for _, svc := range df.Services {
		if strings.TrimSpace(svc.Name) == "" {
			return ErrEmptyServiceName
		}
	}

	return nil
}
