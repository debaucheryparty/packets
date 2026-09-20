package environment

import (
	"testing"
)

func TestEnvironment_DeterministicIdentity(t *testing.T) {
	spec1 := EnvironmentSpec{
		Components: []string{"android", "go"},
		Toolchains: []string{"gradle", "go1.23"},
		Target:     "linux-amd64",
		Versions: map[string]string{
			"gradle": "8.5",
			"go":     "1.23.0",
		},
	}

	spec2 := EnvironmentSpec{
		Components: []string{"go", "android"},
		Toolchains: []string{"go1.23", "gradle"},
		Target:     "linux-amd64",
		Versions: map[string]string{
			"go":     "1.23.0",
			"gradle": "8.5",
		},
	}

	id1 := spec1.Identity()
	id2 := spec2.Identity()

	if id1 == "" {
		t.Fatalf("expected non-empty environment ID")
	}

	if id1 != id2 {
		t.Errorf("expected deterministic ID regardless of order: %s vs %s", id1, id2)
	}

	spec3 := EnvironmentSpec{
		Components: []string{"android"},
		Toolchains: []string{"gradle"},
	}
	id3 := spec3.Identity()
	if id3 == id1 {
		t.Errorf("expected different ID for different spec: %s vs %s", id3, id1)
	}
}

func TestEnvironment_TopologyIdentity(t *testing.T) {
	topo := &ProjectTopology{
		RootPath: "/test/app",
		Components: []Component{
			{
				Type: ComponentAndroid,
				Name: "app",
				Requirements: []ToolchainRequirement{
					{Name: "gradle", Version: "8.5"},
				},
			},
		},
	}

	id := topo.Identity()
	if id == "" {
		t.Fatalf("expected non-empty topology identity")
	}

	spec := topo.Spec()
	if len(spec.Components) != 1 || spec.Components[0] != "android" {
		t.Errorf("expected component android, got %v", spec.Components)
	}
}

func TestManager_Caching(t *testing.T) {
	mgr := NewManager()
	envID := EnvironmentID("env-test-123")

	if mgr.IsCached(envID) {
		t.Errorf("expected false before caching")
	}

	mgr.MarkCached(envID)
	if !mgr.IsCached(envID) {
		t.Errorf("expected true after caching")
	}
}
