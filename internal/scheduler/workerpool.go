package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/debaucheryparty/packets/internal/storage"
	"github.com/debaucheryparty/packets/internal/worker"
	"github.com/debaucheryparty/packets/pkg/apitypes"
)

var (
	ErrNoWorkersAvailable = errors.New("no healthy workers available")
	ErrWorkerNotFound     = errors.New("worker not found")
)

type WorkerNode struct {
	ID            string
	Executor      *worker.Executor
	Healthy       bool
	ActiveJobs    int
	MaxJobs       int
	Labels        map[string]string
	LastHeartbeat time.Time
}

type WorkerPool struct {
	mu         sync.RWMutex
	logger     *slog.Logger
	dispatcher *Dispatcher
	sem        chan struct{}
	workers    map[string]*WorkerNode
}

func NewWorkerPool(logger *slog.Logger, dispatcher *Dispatcher, concurrency int) *WorkerPool {
	if concurrency <= 0 {
		concurrency = 10
	}
	return &WorkerPool{
		logger:     logger,
		dispatcher: dispatcher,
		sem:        make(chan struct{}, concurrency),
		workers:    make(map[string]*WorkerNode),
	}
}

func (w *WorkerPool) RegisterWorker(node *WorkerNode) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if node.MaxJobs <= 0 {
		node.MaxJobs = 5
	}
	node.LastHeartbeat = time.Now().UTC()
	w.workers[node.ID] = node
}

func (w *WorkerPool) UnregisterWorker(id string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.workers, id)
}

func (w *WorkerPool) SetWorkerHealth(id string, healthy bool) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	node, ok := w.workers[id]
	if !ok {
		return ErrWorkerNotFound
	}
	node.Healthy = healthy
	return nil
}

func (w *WorkerPool) SelectWorker(toolchain apitypes.Toolchain) (*WorkerNode, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	var best *WorkerNode
	minActive := int(^uint(0) >> 1)

	for _, node := range w.workers {
		if !node.Healthy {
			continue
		}
		if node.ActiveJobs >= node.MaxJobs {
			continue
		}
		if node.ActiveJobs < minActive {
			minActive = node.ActiveJobs
			best = node
		}
	}

	if best == nil {
		return nil, ErrNoWorkersAvailable
	}
	best.ActiveJobs++
	return best, nil
}

func (w *WorkerPool) ReleaseWorker(id string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if node, ok := w.workers[id]; ok {
		if node.ActiveJobs > 0 {
			node.ActiveJobs--
		}
	}
}

func (w *WorkerPool) ExecuteOnWorker(ctx context.Context, job apitypes.Job) (apitypes.ExecutionResult, string, error) {
	node, err := w.SelectWorker(job.Toolchain)
	if err != nil {
		return apitypes.ExecutionResult{}, "", err
	}
	defer w.ReleaseWorker(node.ID)

	if node.Executor == nil {
		return apitypes.ExecutionResult{}, node.ID, fmt.Errorf("worker %s executor unconfigured", node.ID)
	}

	res, err := node.Executor.Execute(ctx, job)
	return res, node.ID, err
}

func (w *WorkerPool) WorkerCount() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return len(w.workers)
}

func (w *WorkerPool) RecoverPendingJobs(ctx context.Context, store *storage.JobStore) error {
	if w.dispatcher != nil {
		return w.dispatcher.RecoverPendingJobs(ctx)
	}
	return nil
}
