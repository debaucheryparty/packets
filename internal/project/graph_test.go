package project

import (
	"testing"
)

func TestBuildGraph_Order(t *testing.T) {
	cfg := &Config{
		Components: []ComponentConfig{
			{Name: "app", Type: "go", DependsOn: []string{"core", "util"}},
			{Name: "core", Type: "rust", DependsOn: []string{"util"}},
			{Name: "util", Type: "c"},
		},
	}

	graph, err := NewBuildGraph(cfg)
	if err != nil {
		t.Fatalf("NewBuildGraph failed: %v", err)
	}

	order, err := graph.BuildOrder()
	if err != nil {
		t.Fatalf("BuildOrder failed: %v", err)
	}

	if len(order) != 3 {
		t.Fatalf("expected 3 components, got %d", len(order))
	}

	// util must be built first, then core, then app
	if order[0].Name != "util" {
		t.Errorf("expected util first, got %s", order[0].Name)
	}
	if order[1].Name != "core" {
		t.Errorf("expected core second, got %s", order[1].Name)
	}
	if order[2].Name != "app" {
		t.Errorf("expected app last, got %s", order[2].Name)
	}
}

func TestBuildGraph_CycleDetection(t *testing.T) {
	cfg := &Config{
		Components: []ComponentConfig{
			{Name: "a", Type: "go", DependsOn: []string{"b"}},
			{Name: "b", Type: "go", DependsOn: []string{"a"}},
		},
	}

	_, err := NewBuildGraph(cfg)
	if err == nil {
		t.Fatalf("expected cycle detection error, got nil")
	}
}

func TestBuildGraph_BuildOrderFor(t *testing.T) {
	cfg := &Config{
		Components: []ComponentConfig{
			{Name: "app1", Type: "go", DependsOn: []string{"shared"}},
			{Name: "app2", Type: "rust", DependsOn: []string{"shared"}},
			{Name: "shared", Type: "c"},
		},
	}

	graph, err := NewBuildGraph(cfg)
	if err != nil {
		t.Fatal(err)
	}

	order, err := graph.BuildOrderFor("app1")
	if err != nil {
		t.Fatalf("BuildOrderFor failed: %v", err)
	}

	if len(order) != 2 {
		t.Fatalf("expected 2 components for app1, got %d", len(order))
	}
	if order[0].Name != "shared" || order[1].Name != "app1" {
		t.Errorf("unexpected order: %v", order)
	}
}
