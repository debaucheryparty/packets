package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/debaucheryparty/packets/internal/storage"
	"github.com/debaucheryparty/packets/pkg/apitypes"
)

func setupTestStore(t *testing.T) (*storage.JobStore, string) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	store, err := storage.NewJobStore(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("failed to create job store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, dir
}

func TestManager_Lifecycle(t *testing.T) {
	ctx := context.Background()
	store, _ := setupTestStore(t)
	mgr := NewManager(store, nil)

	subID := "sub-test-1"
	svcName := "redis"

	svc, err := mgr.Start(ctx, subID, apitypes.Service{
		Name:    svcName,
		Image:   "redis:alpine",
		Command: []string{"echo", "redis ready"},
		Driver:  "process",
	})
	if err != nil {
		t.Fatalf("failed to start service: %v", err)
	}

	if svc.Status != apitypes.ServiceRunning {
		t.Errorf("expected status %s, got %s", apitypes.ServiceRunning, svc.Status)
	}
	if svc.ContainerID == "" {
		t.Errorf("expected non-empty container ID")
	}

	list, err := mgr.List(ctx, subID)
	if err != nil {
		t.Fatalf("failed to list services: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 service, got %d", len(list))
	}
	if list[0].Name != svcName {
		t.Errorf("expected service name %s, got %s", svcName, list[0].Name)
	}

	logs, err := mgr.Logs(ctx, subID, svcName, 10)
	if err != nil {
		t.Fatalf("failed to get logs: %v", err)
	}
	if len(logs) == 0 {
		t.Errorf("expected non-empty logs")
	}

	stopped, err := mgr.Stop(ctx, subID, svcName)
	if err != nil {
		t.Fatalf("failed to stop service: %v", err)
	}
	if stopped.Status != apitypes.ServiceStopped {
		t.Errorf("expected status %s, got %s", apitypes.ServiceStopped, stopped.Status)
	}

	restarted, err := mgr.Restart(ctx, subID, svcName)
	if err != nil {
		t.Fatalf("failed to restart service: %v", err)
	}
	if restarted.Status != apitypes.ServiceRunning {
		t.Errorf("expected status %s, got %s", apitypes.ServiceRunning, restarted.Status)
	}

	if err := mgr.Delete(ctx, subID, svcName); err != nil {
		t.Fatalf("failed to delete service: %v", err)
	}

	remaining, err := mgr.List(ctx, subID)
	if err != nil {
		t.Fatalf("failed to list services: %v", err)
	}
	if len(remaining) != 0 {
		t.Errorf("expected 0 services after delete, got %d", len(remaining))
	}
}

func TestManager_Validation(t *testing.T) {
	ctx := context.Background()
	store, _ := setupTestStore(t)
	mgr := NewManager(store, nil)

	_, err := mgr.Start(ctx, "", apitypes.Service{Name: "postgres"})
	if err == nil {
		t.Fatalf("expected error for empty subspace ID")
	}

	_, err = mgr.Start(ctx, "sub-1", apitypes.Service{Name: ""})
	if err == nil {
		t.Fatalf("expected error for empty service name")
	}

	_, err = mgr.Stop(ctx, "", "postgres")
	if err == nil {
		t.Fatalf("expected error for empty subspace ID on stop")
	}

	_, err = mgr.Logs(ctx, "", "postgres", 10)
	if err == nil {
		t.Fatalf("expected error for empty subspace ID on logs")
	}
}
