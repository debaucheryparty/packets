package toolchain

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/debaucheryparty/packets/pkg/apitypes"
)

func TestDetector_DetectGoProject(t *testing.T) {
	tempDir := t.TempDir()
	goModPath := filepath.Join(tempDir, "go.mod")
	if err := os.WriteFile(goModPath, []byte("module example.com/test\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	reg := NewRegistry()
	detector := NewDetector(reg)

	def, err := detector.DetectToolchain(tempDir)
	if err != nil {
		t.Fatalf("DetectToolchain failed: %v", err)
	}
	if def.Name != apitypes.ToolchainGo {
		t.Errorf("expected toolchain go, got %s", def.Name)
	}
}

func TestDetector_DetectEmptyDir(t *testing.T) {
	tempDir := t.TempDir()
	reg := NewRegistry()
	detector := NewDetector(reg)

	_, err := detector.DetectToolchain(tempDir)
	if err == nil {
		t.Errorf("expected error detecting toolchain in empty directory, got nil")
	}
}

func TestRouter_Route(t *testing.T) {
	tempDir := t.TempDir()
	cargoPath := filepath.Join(tempDir, "Cargo.toml")
	if err := os.WriteFile(cargoPath, []byte("[package]\nname = \"foo\"\nversion = \"0.1.0\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	reg := NewRegistry()
	detector := NewDetector(reg)
	router := NewRouter(slog.Default(), detector)

	target, err := router.Route(context.Background(), tempDir)
	if err != nil {
		t.Fatalf("Route failed: %v", err)
	}
	if target.Backend != apitypes.BackendSccacheDist {
		t.Errorf("expected backend sccache_dist, got %v", target.Backend)
	}
}
