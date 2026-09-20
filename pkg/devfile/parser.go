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

func Scaffold(name string, componentTypes []string) *Devfile {
	if name == "" {
		name = "app"
	}
	df := &Devfile{
		Name:    name,
		Version: "1.0",
		Build:   CommandSpec{Command: "make build"},
		Test:    CommandSpec{Command: "make test"},
	}

	for _, ct := range componentTypes {
		switch strings.ToLower(ct) {
		case "go":
			df.Environment.Go = "1.24"
			df.Build.Command = "go build ./..."
			df.Test.Command = "go test ./..."
		case "rust":
			df.Environment.Rust = "stable"
			df.Build.Command = "cargo build"
			df.Test.Command = "cargo test"
		case "android":
			df.Environment.Java = "21"
			df.Environment.Android = AndroidSpec{
				SDK:        "35",
				BuildTools: "35.0.0",
			}
			df.Build.Command = "./gradlew assembleDebug"
			df.Test.Command = "./gradlew test"
		case "python":
			df.Environment.Python = "3.12"
			df.Build.Command = "pip install -e ."
			df.Test.Command = "pytest"
		case "node":
			df.Environment.Node = "22"
			df.Build.Command = "npm run build"
			df.Test.Command = "npm test"
			df.Ports = []int{3000}
		case "java":
			df.Environment.Java = "21"
			df.Build.Command = "mvn package"
			df.Test.Command = "mvn test"
		}
	}

	return df
}

func Write(path string, df *Devfile) error {
	data, err := yaml.Marshal(df)
	if err != nil {
		return fmt.Errorf("marshal devfile: %w", err)
	}
	return os.WriteFile(path, data, 0o644)
}
