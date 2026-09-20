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

func TestBuildGraph_BuildBatches(t *testing.T) {
	cfg := &Config{
		Components: []ComponentConfig{
			{Name: "core-rust", Type: "rust"},
			{Name: "backend-go", Type: "go"},
			{Name: "api", Type: "go", DependsOn: []string{"backend-go"}},
			{Name: "app", Type: "android", DependsOn: []string{"core-rust", "api"}},
		},
	}

	graph, err := NewBuildGraph(cfg)
	if err != nil {
		t.Fatalf("NewBuildGraph failed: %v", err)
	}

	batches, err := graph.BuildBatches()
	if err != nil {
		t.Fatalf("BuildBatches failed: %v", err)
	}

	if len(batches) != 3 {
		t.Fatalf("expected 3 parallel batches, got %d", len(batches))
	}

	if len(batches[0]) != 2 {
		t.Errorf("expected batch 0 to have 2 parallel components, got %d", len(batches[0]))
	}

	if len(batches[1]) != 1 || batches[1][0].Name != "api" {
		t.Errorf("expected batch 1 to be api, got %+v", batches[1])
	}

	if len(batches[2]) != 1 || batches[2][0].Name != "app" {
		t.Errorf("expected batch 2 to be app, got %+v", batches[2])
	}

	targetBatches, err := graph.BuildBatchesFor("api")
	if err != nil {
		t.Fatalf("BuildBatchesFor failed: %v", err)
	}
	if len(targetBatches) != 2 {
		t.Fatalf("expected 2 batches for api, got %d", len(targetBatches))
	}
	if len(targetBatches[0]) != 1 || targetBatches[0][0].Name != "backend-go" {
		t.Errorf("expected backend-go in batch 0, got %+v", targetBatches[0])
	}
	if len(targetBatches[1]) != 1 || targetBatches[1][0].Name != "api" {
		t.Errorf("expected api in batch 1, got %+v", targetBatches[1])
	}
}
