package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveProjectID_ProjectJSON(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "packets-project-test-*")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dotPackets := filepath.Join(tempDir, ".packets")
	if err := os.MkdirAll(dotPackets, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	cfgContent := `{"project_id": "my-custom-project"}`
	if err := os.WriteFile(filepath.Join(dotPackets, "project.json"), []byte(cfgContent), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	id := ResolveProjectID(tempDir)
	if id != "my-custom-project" {
		t.Errorf("expected my-custom-project, got %q", id)
	}
}

func TestResolveProjectID_FallbackDeterministic(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "packets-project-test-*")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	id1 := ResolveProjectID(tempDir)
	id2 := ResolveProjectID(tempDir)

	if id1 == "" {
		t.Errorf("expected non-empty project ID")
	}
	if id1 != id2 {
		t.Errorf("expected deterministic project ID, got %q and %q", id1, id2)
	}
}

func TestConfig_SaveAndLoad(t *testing.T) {
	tempDir := t.TempDir()

	cfg := &Config{
		ProjectID: "test-app",
		Components: []ComponentConfig{
			{
				Type: "android",
				Path: "mobile",
			},
			{
				Type: "rust",
				Path: "backend",
			},
		},
	}

	if err := SaveConfig(tempDir, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	loaded, err := LoadConfig(tempDir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if loaded.ProjectID != "test-app" {
		t.Errorf("expected test-app, got %q", loaded.ProjectID)
	}
	if len(loaded.Components) != 2 {
		t.Fatalf("expected 2 components, got %d", len(loaded.Components))
	}
	if loaded.Components[0].Type != "android" || loaded.Components[0].Path != "mobile" {
		t.Errorf("unexpected component 0: %+v", loaded.Components[0])
	}
	if loaded.Components[1].Type != "rust" || loaded.Components[1].Path != "backend" {
		t.Errorf("unexpected component 1: %+v", loaded.Components[1])
	}
}
