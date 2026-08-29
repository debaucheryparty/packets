package scheduler

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/waris4ly/packets/pkg/apitypes"
)

func TestLogBrokerPubSub(t *testing.T) {
	broker := NewLogBroker()
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
	limiter := NewQuotaLimiter(2, 60)
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
