package cli

import (
	"context"
	"os"
	"testing"
)

func TestGenerateCacheKey_DebugVsRelease(t *testing.T) {
	ctx := context.Background()
	tempDir, err := os.MkdirTemp("", "packets-hash-test-*")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	keyDebug, err := GenerateCacheKey(ctx, CacheKeyInputs{
		ProjectID:   "proj-123",
		Dir:         tempDir,
		Toolchain:   "android",
		Runner:      "host",
		SourceMode:  "workspace",
		SnapshotRef: "snap-abc",
		CommandArgs: []string{"assembleDebug"},
	})
	if err != nil {
		t.Fatalf("GenerateCacheKey debug: %v", err)
	}

	keyRelease, err := GenerateCacheKey(ctx, CacheKeyInputs{
		ProjectID:   "proj-123",
		Dir:         tempDir,
		Toolchain:   "android",
		Runner:      "host",
		SourceMode:  "workspace",
		SnapshotRef: "snap-abc",
		CommandArgs: []string{"assembleRelease"},
	})
	if err != nil {
		t.Fatalf("GenerateCacheKey release: %v", err)
	}

	if keyDebug == keyRelease {
		t.Errorf("expected assembleDebug and assembleRelease to produce different cache keys, got %q", keyDebug)
	}
}

func TestGenerateCacheKey_ProjectIDSeparation(t *testing.T) {
	ctx := context.Background()
	tempDir, err := os.MkdirTemp("", "packets-hash-test-*")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	keyProjA, err := GenerateCacheKey(ctx, CacheKeyInputs{
		ProjectID:   "proj-A",
		Dir:         tempDir,
		Toolchain:   "rust",
		Runner:      "host",
		SourceMode:  "workspace",
		SnapshotRef: "snap-abc",
		CommandArgs: []string{"cargo", "build"},
	})
	if err != nil {
		t.Fatalf("GenerateCacheKey proj A: %v", err)
	}

	keyProjB, err := GenerateCacheKey(ctx, CacheKeyInputs{
		ProjectID:   "proj-B",
		Dir:         tempDir,
		Toolchain:   "rust",
		Runner:      "host",
		SourceMode:  "workspace",
		SnapshotRef: "snap-abc",
		CommandArgs: []string{"cargo", "build"},
	})
	if err != nil {
		t.Fatalf("GenerateCacheKey proj B: %v", err)
	}

	if keyProjA == keyProjB {
		t.Errorf("expected proj-A and proj-B to produce different cache keys, got %q", keyProjA)
	}
}
