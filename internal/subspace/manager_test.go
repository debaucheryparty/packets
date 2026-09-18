package subspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/debaucheryparty/packets/internal/storage"
	"github.com/debaucheryparty/packets/pkg/apitypes"
)

func setupTestManager(t *testing.T) (*Manager, *storage.JobStore, string) {
	t.Helper()
	store, err := storage.NewJobStore(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	tempDir := t.TempDir()
	mgr := NewManager(store, nil, tempDir)
	return mgr, store, tempDir
}

func TestManager_CreateAndGet(t *testing.T) {
	mgr, _, _ := setupTestManager(t)
	ctx := context.Background()

	sub, err := mgr.Create(ctx, CreateOptions{
		ProjectID:     "proj-demo",
		OwnerID:       "alice",
		WorkerID:      "worker-1",
		EnvironmentID: "env-go",
		Metadata: map[string]string{
			"version": "1.22",
		},
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if sub.ID == "" || sub.State != apitypes.SubspaceReady {
		t.Errorf("unexpected subspace state or ID: %+v", sub)
	}
	if sub.OwnerID != "alice" || sub.ProjectID != "proj-demo" {
		t.Errorf("unexpected owner/project: %s/%s", sub.OwnerID, sub.ProjectID)
	}

	wsPath, err := mgr.WorkspacePath(sub)
	if err != nil {
		t.Fatalf("WorkspacePath failed: %v", err)
	}
	if fi, err := os.Stat(wsPath); err != nil || !fi.IsDir() {
		t.Errorf("workspace directory does not exist: %s", wsPath)
	}

	got, err := mgr.Get(ctx, sub.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.ID != sub.ID || got.State != apitypes.SubspaceReady {
		t.Errorf("mismatch on Get: got %+v, want %+v", got, sub)
	}
}

func TestManager_List(t *testing.T) {
	mgr, _, _ := setupTestManager(t)
	ctx := context.Background()

	_, err := mgr.Create(ctx, CreateOptions{ProjectID: "p1", OwnerID: "bob"})
	if err != nil {
		t.Fatalf("Create 1 failed: %v", err)
	}
	_, err = mgr.Create(ctx, CreateOptions{ProjectID: "p2", OwnerID: "bob"})
	if err != nil {
		t.Fatalf("Create 2 failed: %v", err)
	}
	_, err = mgr.Create(ctx, CreateOptions{ProjectID: "p1", OwnerID: "carol"})
	if err != nil {
		t.Fatalf("Create 3 failed: %v", err)
	}

	bobSubs, err := mgr.List(ctx, "bob", "")
	if err != nil || len(bobSubs) != 2 {
		t.Errorf("expected 2 subspaces for bob, got %d (err=%v)", len(bobSubs), err)
	}

	bobP1, err := mgr.List(ctx, "bob", "p1")
	if err != nil || len(bobP1) != 1 {
		t.Errorf("expected 1 subspace for bob/p1, got %d (err=%v)", len(bobP1), err)
	}
}

func TestManager_PathTraversalPrevention(t *testing.T) {
	mgr, _, _ := setupTestManager(t)
	maliciousSub := apitypes.Subspace{
		ID:        "sub-1",
		OwnerID:   "../../etc",
		ProjectID: "passwd",
	}

	_, err := mgr.WorkspacePath(maliciousSub)
	if err == nil {
		t.Errorf("expected ErrPathTraversal for malicious owner/project, got nil")
	}
}

func TestManager_StateTransitionsAndDestroy(t *testing.T) {
	mgr, _, _ := setupTestManager(t)
	ctx := context.Background()

	sub, err := mgr.Create(ctx, CreateOptions{ProjectID: "p-test", OwnerID: "dave"})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	wsPath, err := mgr.WorkspacePath(sub)
	if err != nil {
		t.Fatalf("WorkspacePath failed: %v", err)
	}
	testFile := filepath.Join(wsPath, "sample.txt")
	_ = os.WriteFile(testFile, []byte("data"), 0o644)

	if err := mgr.UpdateState(ctx, sub.ID, apitypes.SubspaceBusy); err != nil {
		t.Fatalf("UpdateState failed: %v", err)
	}
	updated, err := mgr.Get(ctx, sub.ID)
	if err != nil || updated.State != apitypes.SubspaceBusy {
		t.Errorf("expected state busy, got %v (err=%v)", updated.State, err)
	}

	if err := mgr.Destroy(ctx, sub.ID); err != nil {
		t.Fatalf("Destroy failed: %v", err)
	}

	if _, err := mgr.Get(ctx, sub.ID); err == nil {
		t.Errorf("expected error getting destroyed subspace, got nil")
	}

	if _, err := os.Stat(testFile); !os.IsNotExist(err) {
		t.Errorf("expected workspace directory to be cleaned up after destroy")
	}
}
