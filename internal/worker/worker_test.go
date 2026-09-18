package worker

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHostSecurityPolicy_Validate(t *testing.T) {
	tempDir := t.TempDir()
	policy := DefaultHostSecurityPolicy()

	for _, invalidDir := range []string{"", "/", "\\", "."} {
		if err := policy.Validate(invalidDir, []string{"echo", "hi"}); err == nil {
			t.Errorf("expected error for invalid srcDir %q, got nil", invalidDir)
		}
	}

	for _, blockedCmd := range []string{"rm -rf /", "mkfs.ext4", "shutdown -h now", "reboot", "format C:", "diskpart"} {
		if err := policy.Validate(tempDir, []string{blockedCmd}); err == nil {
			t.Errorf("expected security violation for blocked command %q", blockedCmd)
		}
	}

	if err := policy.Validate(tempDir, []string{"echo", "hello world"}); err != nil {
		t.Fatalf("expected valid command to pass, got error: %v", err)
	}

	policy.AllowedRoots = []string{tempDir}
	if err := policy.Validate(tempDir, []string{"go", "build"}); err != nil {
		t.Fatalf("expected path inside allowed roots to pass: %v", err)
	}

	otherDir := filepath.Dir(tempDir)
	if otherDir != tempDir {
		if err := policy.Validate(otherDir, []string{"go", "build"}); err == nil {
			t.Errorf("expected error for path outside allowed roots %s", otherDir)
		}
	}
}

func TestSanitizeHostEnvironment(t *testing.T) {
	input := []string{
		"PATH=/usr/bin:/bin",
		"HOME=/home/testuser",
		"AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE",
		"AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		"GITHUB_TOKEN=ghp_dummytoken12345",
		"PACKETS_AUTH_TOKEN=supersecrettoken",
		"DATABASE_PASSWORD=secretpassword",
		"API_KEY=my-key-value",
		"APP_ENV=production",
	}

	cleaned := SanitizeHostEnvironment(input)
	envMap := make(map[string]string)
	for _, env := range cleaned {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}

	if envMap["PATH"] != "/usr/bin:/bin" {
		t.Errorf("expected PATH to be preserved")
	}
	if envMap["HOME"] != "/home/testuser" {
		t.Errorf("expected HOME to be preserved")
	}
	if envMap["APP_ENV"] != "production" {
		t.Errorf("expected APP_ENV to be preserved")
	}

	sensitiveKeys := []string{
		"AWS_ACCESS_KEY_ID",
		"AWS_SECRET_ACCESS_KEY",
		"GITHUB_TOKEN",
		"PACKETS_AUTH_TOKEN",
		"DATABASE_PASSWORD",
		"API_KEY",
	}
	for _, key := range sensitiveKeys {
		if _, exists := envMap[key]; exists {
			t.Errorf("expected sensitive key %s to be stripped from environment", key)
		}
	}
}

func TestOverrideEnv(t *testing.T) {
	base := []string{"FOO=1", "BAR=2", "BAZ=3"}
	overrides := []string{"BAR=99", "QUX=4"}

	result := overrideEnv(base, overrides)
	resMap := make(map[string]string)
	for _, env := range result {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) == 2 {
			resMap[parts[0]] = parts[1]
		}
	}

	if resMap["FOO"] != "1" || resMap["BAR"] != "99" || resMap["BAZ"] != "3" || resMap["QUX"] != "4" {
		t.Errorf("unexpected env after override: %v", resMap)
	}
}

func TestTarArchiving(t *testing.T) {
	tempDir := t.TempDir()
	subDir := filepath.Join(tempDir, "pkg")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}

	file1 := filepath.Join(tempDir, "root.txt")
	file2 := filepath.Join(subDir, "nested.txt")
	if err := os.WriteFile(file1, []byte("root content"), 0o644); err != nil {
		t.Fatalf("failed to write file1: %v", err)
	}
	if err := os.WriteFile(file2, []byte("nested content"), 0o644); err != nil {
		t.Fatalf("failed to write file2: %v", err)
	}

	pr, pw := createTarGzPipe(tempDir, []string{"root.txt", "pkg/**"})
	defer func() { _ = pw.Close() }()

	gr, err := gzip.NewReader(pr)
	if err != nil {
		t.Fatalf("failed to create gzip reader: %v", err)
	}
	defer func() { _ = gr.Close() }()

	tr := tar.NewReader(gr)
	extracted := make(map[string]string)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("failed reading tar header: %v", err)
		}

		content, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("failed reading tar body: %v", err)
		}
		extracted[header.Name] = string(content)
	}

	if extracted["root.txt"] != "root content" {
		t.Errorf("expected root.txt content, got %q", extracted["root.txt"])
	}
	if extracted["pkg/nested.txt"] != "nested content" {
		t.Errorf("expected pkg/nested.txt content, got %q", extracted["pkg/nested.txt"])
	}
}
