package storage_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/waris4ly/packets/internal/storage"
	"github.com/waris4ly/packets/pkg/apitypes"
)

func newTestStore(t *testing.T) *storage.JobStore {
	t.Helper()
	s, err := storage.NewJobStore(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
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
				ID: "j_001", Toolchain: apitypes.ToolchainGo,
				CacheKey: "abc123", State: apitypes.JobStatePending,
				Provider: apitypes.ProviderDockerWorker, SubmittedAt: now,
			},
		},
		{
			name: "python job",
			job: apitypes.Job{
				ID: "j_002", Toolchain: apitypes.ToolchainPython,
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
