package environment

import (
	"context"
	"testing"
)

func TestGenericResolver_Check(t *testing.T) {
	resolver := NewGenericResolver()
	ctx := context.Background()

	// Rust check
	rustReqs := resolver.Check(ctx, Component{Type: ComponentRust})
	if len(rustReqs) != 2 {
		t.Fatalf("expected 2 reqs for Rust, got %d", len(rustReqs))
	}
	if rustReqs[0].Name != "Rust compiler (rustc)" {
		t.Errorf("expected rustc, got %s", rustReqs[0].Name)
	}

	// Go check
	goReqs := resolver.Check(ctx, Component{Type: ComponentGo})
	if len(goReqs) != 1 {
		t.Fatalf("expected 1 req for Go, got %d", len(goReqs))
	}
	if goReqs[0].Name != "Go compiler" {
		t.Errorf("expected Go compiler, got %s", goReqs[0].Name)
	}

	// Unknown component
	unknownReqs := resolver.Check(ctx, Component{Type: "unknown-component"})
	if unknownReqs != nil {
		t.Errorf("expected nil reqs for unknown component, got %v", unknownReqs)
	}
}
