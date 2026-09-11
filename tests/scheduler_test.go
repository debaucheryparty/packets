package tests

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/debaucheryparty/packets/internal/policy"
	"github.com/debaucheryparty/packets/internal/scheduler"
	"github.com/debaucheryparty/packets/internal/storage"
	"github.com/debaucheryparty/packets/internal/toolchain"
	"github.com/debaucheryparty/packets/internal/worker"
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
	t.Cleanup(func() { _ = store.Close() })

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
	t.Cleanup(func() { _ = store.Close() })

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

func TestStateMachine_Comprehensive(t *testing.T) {
	sm := scheduler.NewStateMachine()

	terminalStates := []apitypes.JobState{
		apitypes.JobStateSucceeded,
		apitypes.JobStateFailed,
		apitypes.JobStateFallbackLocal,
	}

	allStates := []apitypes.JobState{
		apitypes.JobStatePending,
		apitypes.JobStateUploading,
		apitypes.JobStateDispatched,
		apitypes.JobStateRunning,
		apitypes.JobStateSucceeded,
		apitypes.JobStateFailed,
		apitypes.JobStateFallbackLocal,
	}

	// Terminal states must reject ANY transition
	for _, term := range terminalStates {
		for _, to := range allStates {
			if err := sm.ValidateTransition(term, to); err == nil {
				t.Errorf("expected terminal state %v to reject transition to %v", term, to)
			}
		}
	}

	// Pending can transition to Uploading, Dispatched, Failed, FallbackLocal
	validFromPending := map[apitypes.JobState]bool{
		apitypes.JobStateUploading:     true,
		apitypes.JobStateDispatched:    true,
		apitypes.JobStateFailed:        true,
		apitypes.JobStateFallbackLocal: true,
	}
	for _, to := range allStates {
		err := sm.ValidateTransition(apitypes.JobStatePending, to)
		if validFromPending[to] && err != nil {
			t.Errorf("expected Pending -> %v to be valid, got: %v", to, err)
		} else if !validFromPending[to] && err == nil {
			t.Errorf("expected Pending -> %v to be invalid, but succeeded", to)
		}
	}

	// Running can transition to Succeeded, Failed, FallbackLocal
	validFromRunning := map[apitypes.JobState]bool{
		apitypes.JobStateSucceeded:     true,
		apitypes.JobStateFailed:        true,
		apitypes.JobStateFallbackLocal: true,
	}
	for _, to := range allStates {
		err := sm.ValidateTransition(apitypes.JobStateRunning, to)
		if validFromRunning[to] && err != nil {
			t.Errorf("expected Running -> %v to be valid, got: %v", to, err)
		} else if !validFromRunning[to] && err == nil {
			t.Errorf("expected Running -> %v to be invalid, but succeeded", to)
		}
	}
}

func TestDispatcher_JobRecoveryOnRestart(t *testing.T) {
	ctx := context.Background()
	store, err := storage.NewJobStore(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	stalePendingJob := apitypes.Job{
		ID:          "job-stale-pending",
		ProjectID:   "proj-stale",
		Toolchain:   "go",
		State:       apitypes.JobStatePending,
		Runner:      apitypes.RunnerHost,
		SourceMode:  apitypes.SourceModeWorkspace,
		CommandArgs: []string{"echo", "recovered"},
		SubmittedAt: time.Now().UTC().Add(-10 * time.Minute),
	}
	if err := store.CreateJob(ctx, stalePendingJob); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	staleDispatchedJob := apitypes.Job{
		ID:          "job-stale-dispatched",
		ProjectID:   "proj-stale",
		Toolchain:   "go",
		State:       apitypes.JobStateDispatched,
		Runner:      apitypes.RunnerHost,
		SourceMode:  apitypes.SourceModeWorkspace,
		CommandArgs: []string{"echo", "recovered-dispatched"},
		SubmittedAt: time.Now().UTC().Add(-5 * time.Minute),
	}
	if err := store.CreateJob(ctx, staleDispatchedJob); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	// Verify both are returned by ListJobsByState
	unrecovered, err := store.ListJobsByState(ctx, apitypes.JobStatePending, apitypes.JobStateDispatched)
	if err != nil {
		t.Fatalf("ListJobsByState: %v", err)
	}
	if len(unrecovered) != 2 {
		t.Fatalf("expected 2 unrecovered jobs, got %d", len(unrecovered))
	}
}

func TestDispatcher_ConcurrentJobSubmissions(t *testing.T) {
	ctx := context.Background()
	dbFile := filepath.Join(t.TempDir(), "concurrent_test.db")
	store, err := storage.NewJobStore(ctx, dbFile)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	dispatcher := scheduler.NewDispatcher(slog.Default(), store, nil, nil, nil, scheduler.NewLogBroker())

	var wg sync.WaitGroup
	numJobs := 20
	jobIDs := make([]apitypes.JobID, numJobs)
	errorsChan := make(chan error, numJobs)

	for i := 0; i < numJobs; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := apitypes.BuildRequest{
				Toolchain:   "go",
				Runner:      apitypes.RunnerHost,
				SourceMode:  apitypes.SourceModeWorkspace,
				CommandArgs: []string{"echo", "test"},
				ProjectID:   "proj-concurrent",
			}
			cacheKey := "unique-key-" + time.Now().String() + string(rune(idx))
			jobID, _, err := dispatcher.Submit(ctx, req, cacheKey, "user-concurrent")
			if err != nil {
				errorsChan <- err
				return
			}
			jobIDs[idx] = jobID
		}(i)
	}

	wg.Wait()
	close(errorsChan)

	for err := range errorsChan {
		t.Errorf("concurrent submit error: %v", err)
	}

	// Verify all jobs exist in store
	seenIDs := make(map[apitypes.JobID]bool)
	for _, id := range jobIDs {
		if id == "" {
			t.Errorf("expected non-empty job ID")
			continue
		}
		if seenIDs[id] {
			t.Errorf("duplicate job ID generated: %s", id)
		}
		seenIDs[id] = true

		job, err := store.GetJob(ctx, id)
		if err != nil {
			t.Errorf("GetJob %s failed: %v", id, err)
		}
		if job.ProjectID != "proj-concurrent" {
			t.Errorf("expected ProjectID 'proj-concurrent', got %q", job.ProjectID)
		}
	}
}

func TestWorkerPool_MultiWorkerExecutionAndFailover(t *testing.T) {
	pool := scheduler.NewWorkerPool(slog.Default(), nil, 10)

	dirA := t.TempDir()
	dirB := t.TempDir()
	dirC := t.TempDir()

	execA := worker.NewExecutor(nil, nil, nil, toolchain.NewRegistry(), nil, dirA)
	execB := worker.NewExecutor(nil, nil, nil, toolchain.NewRegistry(), nil, dirB)
	execC := worker.NewExecutor(nil, nil, nil, toolchain.NewRegistry(), nil, dirC)

	pool.RegisterWorker(&scheduler.WorkerNode{ID: "worker-A", Executor: execA, Healthy: true, MaxJobs: 2})
	pool.RegisterWorker(&scheduler.WorkerNode{ID: "worker-B", Executor: execB, Healthy: true, MaxJobs: 2})
	pool.RegisterWorker(&scheduler.WorkerNode{ID: "worker-C", Executor: execC, Healthy: true, MaxJobs: 2})

	if pool.WorkerCount() != 3 {
		t.Fatalf("expected 3 workers registered, got %d", pool.WorkerCount())
	}

	w1, err := pool.SelectWorker("go")
	if err != nil {
		t.Fatalf("SelectWorker 1: %v", err)
	}
	w2, err := pool.SelectWorker("go")
	if err != nil {
		t.Fatalf("SelectWorker 2: %v", err)
	}
	w3, err := pool.SelectWorker("go")
	if err != nil {
		t.Fatalf("SelectWorker 3: %v", err)
	}

	if w1.ID == w2.ID || w2.ID == w3.ID || w1.ID == w3.ID {
		t.Errorf("expected round-robin / least active distribution across distinct workers, got %s, %s, %s", w1.ID, w2.ID, w3.ID)
	}

	pool.ReleaseWorker(w1.ID)
	pool.ReleaseWorker(w2.ID)
	pool.ReleaseWorker(w3.ID)

	_ = pool.SetWorkerHealth("worker-B", false)
	selectedWorkers := make(map[string]bool)
	for i := 0; i < 4; i++ {
		w, err := pool.SelectWorker("go")
		if err != nil {
			t.Fatalf("SelectWorker during failure: %v", err)
		}
		if w.ID == "worker-B" {
			t.Errorf("unhealthy worker-B was selected")
		}
		selectedWorkers[w.ID] = true
	}

	if selectedWorkers["worker-B"] {
		t.Errorf("worker-B should not have been selected while unhealthy")
	}

	for id := range pool.WorkerCount() {
		_ = id
	}
	pool.ReleaseWorker("worker-A")
	pool.ReleaseWorker("worker-C")

	_ = pool.SetWorkerHealth("worker-B", true)
	wReconnected, err := pool.SelectWorker("go")
	if err != nil {
		t.Fatalf("SelectWorker after reconnect: %v", err)
	}
	if wReconnected.ID != "worker-B" {
		t.Errorf("expected reconnected worker-B to be selected first since it had 0 active jobs, got %s", wReconnected.ID)
	}
	pool.ReleaseWorker(wReconnected.ID)
	pool.ReleaseWorker("worker-A")
	pool.ReleaseWorker("worker-C")

	_ = pool.SetWorkerHealth("worker-A", false)
	_ = pool.SetWorkerHealth("worker-B", false)
	_ = pool.SetWorkerHealth("worker-C", false)

	_, err = pool.SelectWorker("go")
	if !errors.Is(err, scheduler.ErrNoWorkersAvailable) {
		t.Errorf("expected ErrNoWorkersAvailable when all workers unhealthy, got: %v", err)
	}
}


