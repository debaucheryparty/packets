package tests

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/debaucheryparty/packets/internal/storage"
	"github.com/debaucheryparty/packets/pkg/apitypes"
)

func newTestStore(t *testing.T) *storage.JobStore {
	t.Helper()
	s, err := storage.NewJobStore(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestJobStore_CreateAndGet(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	tests := []struct {
		name string
		job  apitypes.Job
	}{
		{
			name: "go job",
			job: apitypes.Job{
				ID: "j_001", ProjectID: "proj-go-1", Toolchain: apitypes.ToolchainGo,
				CacheKey: "abc123", State: apitypes.JobStatePending,
				Provider: apitypes.ProviderDockerWorker, SubmittedAt: now,
			},
		},
		{
			name: "python job",
			job: apitypes.Job{
				ID: "j_002", ProjectID: "proj-py-2", Toolchain: apitypes.ToolchainPython,
				CacheKey: "def456", State: apitypes.JobStatePending,
				Provider: apitypes.ProviderDockerWorker, SubmittedAt: now,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := s.CreateJob(ctx, tt.job); err != nil {
				t.Fatalf("CreateJob: %v", err)
			}

			got, err := s.GetJob(ctx, tt.job.ID)
			if err != nil {
				t.Fatalf("GetJob: %v", err)
			}
			if got.ID != tt.job.ID {
				t.Errorf("ID = %q, want %q", got.ID, tt.job.ID)
			}
			if got.ProjectID != tt.job.ProjectID {
				t.Errorf("ProjectID = %q, want %q", got.ProjectID, tt.job.ProjectID)
			}
			if got.Toolchain != tt.job.Toolchain {
				t.Errorf("Toolchain = %q, want %q", got.Toolchain, tt.job.Toolchain)
			}
			if got.State != tt.job.State {
				t.Errorf("State = %v, want %v", got.State, tt.job.State)
			}
		})
	}
}

func TestJobStore_GetNotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.GetJob(context.Background(), "nonexistent")
	if !errors.Is(err, storage.ErrJobNotFound) {
		t.Errorf("expected ErrJobNotFound, got %v", err)
	}
}

func TestJobStore_UpdateState(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	job := apitypes.Job{
		ID: "j_state", Toolchain: apitypes.ToolchainRust,
		CacheKey: "key1", State: apitypes.JobStatePending,
		SubmittedAt: time.Now().UTC(),
	}
	if err := s.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	tests := []struct {
		name  string
		state apitypes.JobState
	}{
		{name: "to running", state: apitypes.JobStateRunning},
		{name: "to succeeded", state: apitypes.JobStateSucceeded},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := s.UpdateJobState(ctx, job.ID, tt.state); err != nil {
				t.Fatalf("UpdateJobState: %v", err)
			}
			got, err := s.GetJob(ctx, job.ID)
			if err != nil {
				t.Fatalf("GetJob: %v", err)
			}
			if got.State != tt.state {
				t.Errorf("State = %v, want %v", got.State, tt.state)
			}
		})
	}
}

func TestJobStore_UpdateStateNotFound(t *testing.T) {
	s := newTestStore(t)
	err := s.UpdateJobState(context.Background(), "ghost", apitypes.JobStateRunning)
	if !errors.Is(err, storage.ErrJobNotFound) {
		t.Errorf("expected ErrJobNotFound, got %v", err)
	}
}

func TestJobStore_CacheLookup(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	tests := []struct {
		name     string
		storeKey string
		storeRef apitypes.ArtifactRef
		lookKey  string
		wantHit  bool
	}{
		{
			name: "hit after store", storeKey: "k1",
			storeRef: "artifact_abc", lookKey: "k1", wantHit: true,
		},
		{
			name: "miss on unknown key", storeKey: "k2",
			storeRef: "artifact_xyz", lookKey: "k_unknown", wantHit: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := s.Store(ctx, tt.storeKey, tt.storeRef); err != nil {
				t.Fatalf("Store: %v", err)
			}
			ref, hit, err := s.Lookup(ctx, tt.lookKey)
			if err != nil {
				t.Fatalf("Lookup: %v", err)
			}
			if hit != tt.wantHit {
				t.Errorf("hit = %v, want %v", hit, tt.wantHit)
			}
			if tt.wantHit && ref != tt.storeRef {
				t.Errorf("ref = %q, want %q", ref, tt.storeRef)
			}
		})
	}
}

func TestJobStore_ListRecentJobs(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	for i := 0; i < 5; i++ {
		job := apitypes.Job{
			ID: apitypes.JobID(fmt.Sprintf("j_%d", i)), Toolchain: apitypes.ToolchainNode,
			CacheKey: fmt.Sprintf("key_%d", i), State: apitypes.JobStatePending,
			SubmittedAt: now.Add(time.Duration(i) * time.Second),
		}
		if err := s.CreateJob(ctx, job); err != nil {
			t.Fatalf("CreateJob: %v", err)
		}
	}

	jobs, err := s.ListRecentJobs(ctx, 3)
	if err != nil {
		t.Fatalf("ListRecentJobs: %v", err)
	}
	if len(jobs) != 3 {
		t.Errorf("got %d jobs, want 3", len(jobs))
	}
}

func TestJobStore_FileBackedWAL_CrashRecoveryAndConcurrency(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "file_wal_test.db")
	ctx := context.Background()

	s1, err := storage.NewJobStore(ctx, dbPath)
	if err != nil {
		t.Fatalf("NewJobStore: %v", err)
	}

	job1 := apitypes.Job{
		ID:          "job_crash_1",
		ProjectID:   "proj-crash",
		Toolchain:   apitypes.ToolchainGo,
		CacheKey:    "key_crash_1",
		State:       apitypes.JobStatePending,
		SubmittedAt: time.Now().UTC(),
	}
	if err := s1.CreateJob(ctx, job1); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	_ = s1.Close()

	s2, err := storage.NewJobStore(ctx, dbPath)
	if err != nil {
		t.Fatalf("NewJobStore after crash/close: %v", err)
	}
	defer func() { _ = s2.Close() }()

	recovered, err := s2.GetJob(ctx, job1.ID)
	if err != nil {
		t.Fatalf("GetJob after recovery: %v", err)
	}
	if recovered.ID != job1.ID || recovered.ProjectID != job1.ProjectID {
		t.Errorf("recovered job mismatch: got %+v, want %+v", recovered, job1)
	}

	concurrency := 8
	var wg sync.WaitGroup
	wg.Add(concurrency)

	for i := 0; i < concurrency; i++ {
		workerID := i
		go func() {
			defer wg.Done()
			j := apitypes.Job{
				ID:          apitypes.JobID(fmt.Sprintf("concurrent_job_%d", workerID)),
				ProjectID:   "proj-concurrent",
				Toolchain:   apitypes.ToolchainRust,
				CacheKey:    fmt.Sprintf("cache_key_%d", workerID),
				State:       apitypes.JobStatePending,
				SubmittedAt: time.Now().UTC(),
			}
			if err := s2.CreateJob(ctx, j); err != nil {
				t.Errorf("concurrent CreateJob %d: %v", workerID, err)
			}
			_, _ = s2.GetJob(ctx, j.ID)
		}()
	}

	wg.Wait()
}

func TestJobStore_Subspaces(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	sub := apitypes.Subspace{
		ID:            "sub_123",
		ProjectID:     "proj-android",
		OwnerID:       "user-alice",
		WorkerID:      "worker-node-1",
		WorkspaceID:   "ws_android_01",
		EnvironmentID: "env_jdk21",
		State:         apitypes.SubspaceReady,
		CreatedAt:     now,
		LastUsedAt:    now,
		Metadata: map[string]string{
			"toolchain": "kotlin",
			"variant":   "debug",
		},
	}

	if err := s.CreateSubspace(ctx, sub); err != nil {
		t.Fatalf("CreateSubspace failed: %v", err)
	}

	got, err := s.GetSubspace(ctx, "sub_123")
	if err != nil {
		t.Fatalf("GetSubspace failed: %v", err)
	}
	if got.ID != sub.ID || got.ProjectID != sub.ProjectID || got.OwnerID != sub.OwnerID ||
		got.WorkerID != sub.WorkerID || got.WorkspaceID != sub.WorkspaceID ||
		got.EnvironmentID != sub.EnvironmentID || got.State != apitypes.SubspaceReady {
		t.Errorf("GetSubspace mismatch: got %+v, want %+v", got, sub)
	}
	if got.Metadata["toolchain"] != "kotlin" || got.Metadata["variant"] != "debug" {
		t.Errorf("metadata mismatch: got %+v", got.Metadata)
	}

	if err := s.UpdateSubspaceState(ctx, "sub_123", apitypes.SubspaceBusy); err != nil {
		t.Fatalf("UpdateSubspaceState failed: %v", err)
	}
	got2, err := s.GetSubspace(ctx, "sub_123")
	if err != nil || got2.State != apitypes.SubspaceBusy {
		t.Errorf("expected state busy, got %v (err=%v)", got2.State, err)
	}

	if err := s.TouchSubspace(ctx, "sub_123"); err != nil {
		t.Fatalf("TouchSubspace failed: %v", err)
	}

	list, err := s.ListSubspaces(ctx, "user-alice", "proj-android")
	if err != nil || len(list) != 1 {
		t.Fatalf("ListSubspaces expected 1 item, got %d (err=%v)", len(list), err)
	}

	if err := s.DeleteSubspace(ctx, "sub_123"); err != nil {
		t.Fatalf("DeleteSubspace failed: %v", err)
	}
	_, err = s.GetSubspace(ctx, "sub_123")
	if !errors.Is(err, storage.ErrSubspaceNotFound) {
		t.Errorf("expected ErrSubspaceNotFound after delete, got %v", err)
	}
}
