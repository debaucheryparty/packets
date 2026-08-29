package scheduler

import (
	"context"
	"log/slog"

	"github.com/waris4ly/packets/internal/storage"
)

type WorkerPool struct {
	logger     *slog.Logger
	dispatcher *Dispatcher
	sem        chan struct{}
}

func NewWorkerPool(logger *slog.Logger, dispatcher *Dispatcher, concurrency int) *WorkerPool {
	return &WorkerPool{
		logger:     logger,
		dispatcher: dispatcher,
		sem:        make(chan struct{}, concurrency),
	}
}

func (w *WorkerPool) RecoverPendingJobs(ctx context.Context, store *storage.JobStore) error {
	return w.dispatcher.RecoverPendingJobs(ctx)
}
