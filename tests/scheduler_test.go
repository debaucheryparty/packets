package tests

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/debaucheryparty/packets/internal/policy"
	"github.com/debaucheryparty/packets/internal/scheduler"
	"github.com/debaucheryparty/packets/internal/storage"
	"github.com/debaucheryparty/packets/pkg/apitypes"
	pb "github.com/debaucheryparty/packets/proto/v1"
)

func TestStateMachine(t *testing.T) {
	sm := scheduler.NewStateMachine()

	err := sm.ValidateTransition(apitypes.JobStatePending, apitypes.JobStateDispatched)
	if err != nil {
		t.Errorf("expected transition to succeed, got %v", err)
	}

	err = sm.ValidateTransition(apitypes.JobStatePending, apitypes.JobStateSucceeded)
	if err == nil {
		t.Error("expected transition to fail, got nil")
	}

	err = sm.ValidateTransition(apitypes.JobStateDispatched, apitypes.JobStateFallbackLocal)
	if err != nil {
		t.Errorf("expected transition to succeed, got %v", err)
	}
}

func TestLogBrokerPubSub(t *testing.T) {
	broker := scheduler.NewLogBroker()
	jobID := apitypes.JobID("test-job-123")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	broker.Publish(jobID, "line 1")
	broker.Publish(jobID, "line 2")

	existing, ch, cleanup := broker.Subscribe(ctx, jobID)
	defer cleanup()

	if len(existing) != 2 {
		t.Fatalf("expected 2 existing lines, got %d", len(existing))
	}
	if existing[0] != "line 1" || existing[1] != "line 2" {
		t.Errorf("unexpected existing lines: %v", existing)
	}

	var wg sync.WaitGroup
	wg.Add(1)

	var received string
	go func() {
		defer wg.Done()
		select {
		case line := <-ch:
			received = line
		case <-ctx.Done():
		}
	}()

	broker.Publish(jobID, "line 3")
	wg.Wait()

	if received != "line 3" {
		t.Errorf("expected 'line 3', got %q", received)
	}

	broker.CloseJob(jobID)
	if !broker.IsClosed(jobID) {
		t.Error("expected job to be marked closed")
	}
}

func TestQuotaLimiter(t *testing.T) {
	limiter := scheduler.NewQuotaLimiter(2, 60)
	owner := "alice"

	if err := limiter.Acquire(owner); err != nil {
		t.Fatalf("acquire 1 failed: %v", err)
	}

	if err := limiter.Acquire(owner); err != nil {
		t.Fatalf("acquire 2 failed: %v", err)
	}

	if err := limiter.Acquire(owner); err == nil {
		t.Fatal("expected acquire 3 to fail with quota exceeded")
	}

	limiter.Release(owner)

	if err := limiter.Acquire(owner); err != nil {
		t.Fatalf("expected acquire after release to succeed, got: %v", err)
	}
}

func TestSubmitJob_ProjectIDPropagation(t *testing.T) {
	ctx := context.Background()
	store, err := storage.NewJobStore(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	dispatcher := scheduler.NewDispatcher(slog.Default(), store, nil, nil, nil, scheduler.NewLogBroker())
	srv := scheduler.NewServer(dispatcher, store, scheduler.NewLogBroker(), nil, nil)

	resp, err := srv.SubmitJob(ctx, &pb.SubmitJobRequest{
		CacheKey:    "test-cache-key-1",
		Toolchain:   "go",
		ProjectId:   "proj-alpha-123",
		CommandArgs: []string{"test"},
	})
	if err != nil {
		t.Fatalf("SubmitJob failed: %v", err)
	}

	job, err := store.GetJob(ctx, apitypes.JobID(resp.JobId))
	if err != nil {
		t.Fatalf("GetJob failed: %v", err)
	}

	if job.ProjectID != "proj-alpha-123" {
		t.Errorf("expected ProjectID 'proj-alpha-123', got %q", job.ProjectID)
	}
}

func TestSubmitJob_EnforcesPolicyApproval(t *testing.T) {
	ctx := context.Background()
	store, err := storage.NewJobStore(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	dispatcher := scheduler.NewDispatcher(slog.Default(), store, nil, nil, nil, scheduler.NewLogBroker())
	srv := scheduler.NewServer(dispatcher, store, scheduler.NewLogBroker(), nil, nil)
	pe := policy.NewPolicyEngine(policy.ApprovalAlways)
	srv.SetPolicyEngine(pe)

	_, err = srv.SubmitJob(ctx, &pb.SubmitJobRequest{
		CacheKey:    "test-cache-key-policy",
		Toolchain:   "go",
		ProjectId:   "proj-alpha-123",
		CommandArgs: []string{"test"},
	})
	if err == nil {
		t.Fatal("expected PermissionDenied error when no approval ticket is provided")
	}

	ec := policy.ExecutionContext{
		User:        "default",
		ProjectID:   "proj-alpha-123",
		WorkspaceID: "proj-alpha-123",
		Command:     "test",
		Args:        []string{"test"},
		Action:      "BUILD",
	}
	pending, err := pe.CreatePendingApproval(ec)
	if err != nil {
		t.Fatalf("CreatePendingApproval: %v", err)
	}
	ticket, err := pe.ApprovePending(pending.ID)
	if err != nil {
		t.Fatalf("ApprovePending: %v", err)
	}

	resp, err := srv.SubmitJob(ctx, &pb.SubmitJobRequest{
		CacheKey:       "test-cache-key-policy-2",
		Toolchain:      "go",
		ProjectId:      "proj-alpha-123",
		CommandArgs:    []string{"test"},
		ApprovalTicket: ticket.ID,
	})
	if err != nil {
		t.Fatalf("expected SubmitJob with valid ticket to succeed, got: %v", err)
	}
	if resp.JobId == "" {
		t.Fatal("expected non-empty JobId")
	}

	_, err = srv.SubmitJob(ctx, &pb.SubmitJobRequest{
		CacheKey:       "test-cache-key-policy-3",
		Toolchain:      "go",
		ProjectId:      "proj-alpha-123",
		CommandArgs:    []string{"test"},
		ApprovalTicket: ticket.ID,
	})
	if err == nil {
		t.Fatal("expected PermissionDenied on reused ticket")
	}
}
