package devfile

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestParse_ValidYaml(t *testing.T) {
	raw := `
name: sample-service
version: "1.0"
environment:
  java: "21"
  android:
    sdk: "35"
    start_emulator: true
  rust: "1.80"
  go: "1.23"
services:
  - name: postgres
    image: postgres:16
    ports:
      - "5432:5432"
build:
  command: ./gradlew assembleDebug
test:
  command: ./gradlew test
ports:
  - 8080
  - 3000
`

	df, err := Parse([]byte(raw))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if df.Name != "sample-service" {
		t.Errorf("expected name sample-service, got %s", df.Name)
	}
	if df.Environment.Java != "21" {
		t.Errorf("expected java 21, got %s", df.Environment.Java)
	}
	if df.Environment.Android.SDK != "35" || !df.Environment.Android.StartEmulator {
		t.Errorf("unexpected android spec: %+v", df.Environment.Android)
	}
	if len(df.Services) != 1 || df.Services[0].Name != "postgres" {
		t.Errorf("unexpected services: %+v", df.Services)
	}
	if df.Build.Command != "./gradlew assembleDebug" {
		t.Errorf("unexpected build command: %s", df.Build.Command)
	}
	if len(df.Ports) != 2 || df.Ports[0] != 8080 || df.Ports[1] != 3000 {
		t.Errorf("unexpected ports: %v", df.Ports)
	}
}

func TestParse_Invalid(t *testing.T) {
	if _, err := Parse([]byte("")); !errors.Is(err, ErrEmptyDevfile) {
		t.Errorf("expected ErrEmptyDevfile, got %v", err)
	}

	badPort := `
name: bad-port
ports:
  - 99999
`
	if _, err := Parse([]byte(badPort)); !errors.Is(err, ErrInvalidPort) {
		t.Errorf("expected ErrInvalidPort, got %v", err)
	}

	badService := `
name: bad-svc
services:
  - image: redis:7
`
	if _, err := Parse([]byte(badService)); !errors.Is(err, ErrEmptyServiceName) {
		t.Errorf("expected ErrEmptyServiceName, got %v", err)
	}
}

func TestFindAndLoad(t *testing.T) {
	tempDir := t.TempDir()

	df, err := Load(tempDir)
	if err != nil || df != nil {
		t.Fatalf("expected nil devfile for empty dir, got %v (err=%v)", df, err)
	}

	filePath := filepath.Join(tempDir, "packets.yaml")
	content := `
name: my-app
build:
  command: make build
`
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write devfile: %v", err)
	}

	foundPath, found := Find(tempDir)
	if !found || foundPath != filePath {
		t.Fatalf("expected to find %s, got %s (found=%v)", filePath, foundPath, found)
	}

	loaded, err := Load(tempDir)
	if err != nil || loaded == nil || loaded.Name != "my-app" {
		t.Fatalf("expected loaded devfile name my-app, got %+v (err=%v)", loaded, err)
	}
}

func TestScaffoldAndWrite(t *testing.T) {
	tempDir := t.TempDir()

	df := Scaffold("test-proj", []string{"go", "rust", "android", "node"})
	if df == nil || df.Name != "test-proj" {
		t.Fatalf("expected scaffolded devfile with name test-proj, got %+v", df)
	}
	if df.Environment.Go != "1.24" {
		t.Errorf("expected go 1.24, got %s", df.Environment.Go)
	}
	if df.Environment.Java != "21" || df.Environment.Android.SDK != "35" {
		t.Errorf("expected android sdk 35 and java 21, got %+v", df.Environment)
	}

	target := filepath.Join(tempDir, "packets.yaml")
	if err := Write(target, df); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	loaded, err := Load(tempDir)
	if err != nil || loaded == nil {
		t.Fatalf("Load scaffolded failed: %v", err)
	}
	if loaded.Name != "test-proj" {
		t.Errorf("expected loaded name test-proj, got %s", loaded.Name)
	}
}
