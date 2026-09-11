package tests

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/debaucheryparty/packets/internal/project"
)

func TestResolveProjectID_ProjectJSON(t *testing.T) {
	tempDir := t.TempDir()

	dotPackets := filepath.Join(tempDir, ".packets")
	if err := os.MkdirAll(dotPackets, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	cfgContent := `{"project_id": "my-custom-project"}`
	if err := os.WriteFile(filepath.Join(dotPackets, "project.json"), []byte(cfgContent), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	id := project.ResolveProjectID(tempDir)
	if id != "my-custom-project" {
		t.Errorf("expected my-custom-project, got %q", id)
	}
}

func TestResolveProjectID_FallbackDeterministic(t *testing.T) {
	tempDir := t.TempDir()

	id1 := project.ResolveProjectID(tempDir)
	id2 := project.ResolveProjectID(tempDir)

	if id1 == "" {
		t.Errorf("expected non-empty project ID")
	}
	if id1 != id2 {
		t.Errorf("expected deterministic project ID, got %q and %q", id1, id2)
	}
}

func TestConfig_SaveAndLoad(t *testing.T) {
	tempDir := t.TempDir()

	cfg := &project.Config{
		ProjectID: "test-app",
		Components: []project.ComponentConfig{
			{
				Type: "android",
				Path: "mobile",
			},
			{
				Type: "rust",
				Path: "core",
			},
		},
	}

	if err := project.SaveConfig(tempDir, cfg); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	loaded, err := project.LoadConfig(tempDir)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if loaded.ProjectID != "test-app" {
		t.Errorf("expected test-app, got %s", loaded.ProjectID)
	}
	if len(loaded.Components) != 2 {
		t.Fatalf("expected 2 components, got %d", len(loaded.Components))
	}
	if loaded.Components[0].Type != "android" || loaded.Components[1].Type != "rust" {
		t.Errorf("unexpected components: %+v", loaded.Components)
	}
}

func TestBuildGraph_TopologicalOrderingAndCycles(t *testing.T) {
	cfg := &project.Config{
		ProjectID: "hybrid-app",
		Components: []project.ComponentConfig{
			{
				Name:         "android-client",
				Type:         "android",
				Path:         "app",
				DependsOn:    []string{"rust-core"},
				BuildCommand: "./gradlew assembleDebug",
			},
			{
				Name:         "rust-core",
				Type:         "rust",
				Path:         "native",
				BuildCommand: "cargo build --release",
			},
			{
				Name:         "docs",
				Type:         "generic",
				Path:         "docs",
				BuildCommand: "mdbook build",
			},
		},
	}

	graph, err := project.NewBuildGraph(cfg)
	if err != nil {
		t.Fatalf("NewBuildGraph failed: %v", err)
	}

	order, err := graph.BuildOrder()
	if err != nil {
		t.Fatalf("BuildOrder failed: %v", err)
	}

	if len(order) != 3 {
		t.Fatalf("expected 3 components in build order, got %d", len(order))
	}

	rustIdx := -1
	androidIdx := -1
	for idx, c := range order {
		if c.Name == "rust-core" {
			rustIdx = idx
		}
		if c.Name == "android-client" {
			androidIdx = idx
		}
	}
	if rustIdx == -1 || androidIdx == -1 || rustIdx > androidIdx {
		t.Errorf("expected rust-core before android-client, got rust=%d android=%d", rustIdx, androidIdx)
	}

	partialOrder, err := graph.BuildOrderFor("android-client")
	if err != nil {
		t.Fatalf("BuildOrderFor failed: %v", err)
	}
	if len(partialOrder) != 2 {
		t.Fatalf("expected 2 components for android-client partial build, got %d", len(partialOrder))
	}
	if partialOrder[0].Name != "rust-core" || partialOrder[1].Name != "android-client" {
		t.Errorf("unexpected partial build order: %+v", partialOrder)
	}

	cyclicCfg := &project.Config{
		ProjectID: "cycle-app",
		Components: []project.ComponentConfig{
			{
				Name:      "comp-a",
				Type:      "go",
				DependsOn: []string{"comp-b"},
			},
			{
				Name:      "comp-b",
				Type:      "rust",
				DependsOn: []string{"comp-a"},
			},
		},
	}
	_, err = project.NewBuildGraph(cyclicCfg)
	if err == nil {
		t.Fatalf("expected cycle detection error for cyclic graph, got nil")
	}

	unknownDepCfg := &project.Config{
		ProjectID: "unknown-dep-app",
		Components: []project.ComponentConfig{
			{
				Name:      "client",
				Type:      "node",
				DependsOn: []string{"non-existent"},
			},
		},
	}
	_, err = project.NewBuildGraph(unknownDepCfg)
	if err == nil {
		t.Fatalf("expected error for unknown dependency, got nil")
	}
}

func TestBuildGraph_ArtifactRouting(t *testing.T) {
	root := t.TempDir()

	rustOutDir := filepath.Join(root, "native", "target", "release")
	_ = os.MkdirAll(rustOutDir, 0o755)
	soPath := filepath.Join(rustOutDir, "libcore.so")
	_ = os.WriteFile(soPath, []byte("\x7fELFfake-shared-object"), 0o755)

	comp := project.ComponentConfig{
		Name: "rust-core",
		Type: "rust",
		Path: "native",
		ArtifactRouting: map[string]string{
			"target/release/libcore.so": "app/src/main/jniLibs/arm64-v8a/libcore.so",
		},
	}

	cfg := &project.Config{
		ProjectID:  "hybrid-app",
		Components: []project.ComponentConfig{comp},
	}

	graph, err := project.NewBuildGraph(cfg)
	if err != nil {
		t.Fatalf("NewBuildGraph failed: %v", err)
	}

	if err := graph.RouteArtifacts(root, comp); err != nil {
		t.Fatalf("RouteArtifacts failed: %v", err)
	}

	routedFile := filepath.Join(root, "app", "src", "main", "jniLibs", "arm64-v8a", "libcore.so")
	content, err := os.ReadFile(routedFile)
	if err != nil {
		t.Fatalf("reading routed artifact: %v", err)
	}
	if string(content) != "\x7fELFfake-shared-object" {
		t.Errorf("routed content mismatch: got %q", string(content))
	}
}

