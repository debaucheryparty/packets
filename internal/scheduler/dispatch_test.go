package scheduler

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/debaucheryparty/packets/internal/storage"
	"github.com/debaucheryparty/packets/pkg/apitypes"
	pb "github.com/debaucheryparty/packets/proto/v1"
)

func TestDispatcher_Cancel(t *testing.T) {
	ctx := context.Background()
	store, err := storage.NewJobStore(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	dispatcher := NewDispatcher(slog.Default(), store, nil, nil, nil, NewLogBroker())

	jobID := apitypes.JobID("job-cancel-test")
	job := apitypes.Job{
		ID:          jobID,
		Toolchain:   apitypes.ToolchainGo,
		State:       apitypes.JobStateRunning,
		SubmittedAt: time.Now().UTC(),
		Owner:       "alice",
	}
	if err := store.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}

	cancelled, err := dispatcher.Cancel(ctx, jobID)
	if err != nil {
		t.Fatalf("unexpected cancel error: %v", err)
	}
	if !cancelled {
		t.Fatalf("expected cancelled to be true")
	}

	updated, err := store.GetJob(ctx, jobID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.State != apitypes.JobStateFailed {
		t.Errorf("expected state failed, got %v", updated.State)
	}

	cancelledAgain, err := dispatcher.Cancel(ctx, jobID)
	if err != nil {
		t.Fatalf("unexpected error on second cancel: %v", err)
	}
	if cancelledAgain {
		t.Errorf("expected second cancel to return false")
	}
}

func TestDispatcher_ReapStaleJobs(t *testing.T) {
	ctx := context.Background()
	store, err := storage.NewJobStore(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	dispatcher := NewDispatcher(slog.Default(), store, nil, nil, nil, NewLogBroker())

	oldJob := apitypes.Job{
		ID:          apitypes.JobID("stale-job"),
		Toolchain:   apitypes.ToolchainRust,
		State:       apitypes.JobStateRunning,
		SubmittedAt: time.Now().UTC().Add(-2 * time.Hour),
		Owner:       "bob",
	}
	freshJob := apitypes.Job{
		ID:          apitypes.JobID("fresh-job"),
		Toolchain:   apitypes.ToolchainRust,
		State:       apitypes.JobStateRunning,
		SubmittedAt: time.Now().UTC(),
		Owner:       "bob",
	}

	if err := store.CreateJob(ctx, oldJob); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateJob(ctx, freshJob); err != nil {
		t.Fatal(err)
	}

	reaped, err := dispatcher.ReapStaleJobs(ctx, 1*time.Hour)
	if err != nil {
		t.Fatalf("ReapStaleJobs failed: %v", err)
	}
	if reaped != 1 {
		t.Fatalf("expected 1 job reaped, got %d", reaped)
	}

	j1, _ := store.GetJob(ctx, oldJob.ID)
	if j1.State != apitypes.JobStateFailed {
		t.Errorf("expected stale job to be failed, got %v", j1.State)
	}

	j2, _ := store.GetJob(ctx, freshJob.ID)
	if j2.State != apitypes.JobStateRunning {
		t.Errorf("expected fresh job to remain running, got %v", j2.State)
	}
}

func TestServer_ListJobsAndCancel(t *testing.T) {
	ctx := context.Background()
	store, err := storage.NewJobStore(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	dispatcher := NewDispatcher(slog.Default(), store, nil, nil, nil, NewLogBroker())
	srv := NewServer(dispatcher, store, NewLogBroker(), nil, nil)

	j := apitypes.Job{
		ID:          apitypes.JobID("list-test-1"),
		Toolchain:   apitypes.ToolchainNode,
		Runner:      apitypes.RunnerDocker,
		State:       apitypes.JobStateRunning,
		SubmittedAt: time.Now().UTC(),
		Owner:       "charlie",
	}
	if err := store.CreateJob(ctx, j); err != nil {
		t.Fatal(err)
	}

	listResp, err := srv.ListJobs(ctx, &pb.ListJobsRequest{Limit: 10})
	if err != nil {
		t.Fatalf("ListJobs failed: %v", err)
	}
	if len(listResp.Jobs) != 1 {
		t.Fatalf("expected 1 job in list, got %d", len(listResp.Jobs))
	}
	if listResp.Jobs[0].JobId != "list-test-1" {
		t.Errorf("expected job_id 'list-test-1', got %q", listResp.Jobs[0].JobId)
	}

	cancelResp, err := srv.CancelJob(ctx, &pb.CancelJobRequest{JobId: "list-test-1"})
	if err != nil {
		t.Fatalf("CancelJob failed: %v", err)
	}
	if !cancelResp.Cancelled {
		t.Errorf("expected cancel response to be true")
	}
}
