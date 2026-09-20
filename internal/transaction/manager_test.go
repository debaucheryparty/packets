package transaction

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/debaucheryparty/packets/internal/storage"
	"github.com/debaucheryparty/packets/pkg/apitypes"
)

func setupTestStore(t *testing.T) *storage.JobStore {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	store, err := storage.NewJobStore(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("failed to create job store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestManager_Lifecycle(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)
	mgr := NewManager(store, nil)

	tx, err := mgr.Create(ctx, "proj-1", "sub-1", "snap-base", "refactor core")
	if err != nil {
		t.Fatalf("failed to create transaction: %v", err)
	}

	if tx.Status != apitypes.TransactionOpen {
		t.Errorf("expected status %s, got %s", apitypes.TransactionOpen, tx.Status)
	}
	if tx.BaseSnapshotRef != "snap-base" {
		t.Errorf("expected base snapshot snap-base, got %s", tx.BaseSnapshotRef)
	}

	fetched, err := mgr.Get(ctx, tx.ID)
	if err != nil {
		t.Fatalf("failed to get transaction: %v", err)
	}
	if fetched.ID != tx.ID {
		t.Errorf("expected id %s, got %s", tx.ID, fetched.ID)
	}

	committed, err := mgr.Commit(ctx, tx.ID, "snap-working-1")
	if err != nil {
		t.Fatalf("failed to commit transaction: %v", err)
	}
	if committed.Status != apitypes.TransactionCommitted {
		t.Errorf("expected status %s, got %s", apitypes.TransactionCommitted, committed.Status)
	}
	if committed.WorkingSnapshotRef != "snap-working-1" {
		t.Errorf("expected working snapshot snap-working-1, got %s", committed.WorkingSnapshotRef)
	}

	tx2, err := mgr.Create(ctx, "proj-1", "", "snap-base-2", "failed attempt")
	if err != nil {
		t.Fatalf("failed to create second transaction: %v", err)
	}

	rolledBack, err := mgr.Rollback(ctx, tx2.ID)
	if err != nil {
		t.Fatalf("failed to rollback transaction: %v", err)
	}
	if rolledBack.Status != apitypes.TransactionRolledBack {
		t.Errorf("expected status %s, got %s", apitypes.TransactionRolledBack, rolledBack.Status)
	}

	list, err := mgr.List(ctx, "proj-1")
	if err != nil {
		t.Fatalf("failed to list transactions: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2 transactions, got %d", len(list))
	}
}

func TestManager_Validation(t *testing.T) {
	ctx := context.Background()
	store := setupTestStore(t)
	mgr := NewManager(store, nil)

	_, err := mgr.Create(ctx, "", "", "snap-1", "no proj")
	if err == nil {
		t.Fatalf("expected error when project ID is empty")
	}

	tx, err := mgr.Create(ctx, "proj-1", "", "snap-1", "desc")
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	_, err = mgr.Commit(ctx, tx.ID, "snap-2")
	if err != nil {
		t.Fatalf("commit failed: %v", err)
	}

	_, err = mgr.Commit(ctx, tx.ID, "snap-3")
	if err == nil {
		t.Fatalf("expected error when committing non-open transaction")
	}

	_, err = mgr.Rollback(ctx, tx.ID)
	if err == nil {
		t.Fatalf("expected error when rolling back non-open transaction")
	}
}
