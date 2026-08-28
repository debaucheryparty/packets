package scheduler

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/waris4ly/packets/internal/storage"
	"github.com/waris4ly/packets/pkg/apitypes"
)

type WorkerPool struct {
	logger      *slog.Logger
	queue       *storage.NATSQueue
	dispatcher  *Dispatcher
	concurrency int
	sem         chan struct{}
}

func NewWorkerPool(logger *slog.Logger, queue *storage.NATSQueue, dispatcher *Dispatcher, concurrency int) *WorkerPool {
	return &WorkerPool{
		logger:      logger,
		queue:       queue,
		dispatcher:  dispatcher,
		concurrency: concurrency,
		sem:         make(chan struct{}, concurrency),
	}
}

type JobPayload struct {
	Job apitypes.Job          `json:"job"`
	Req apitypes.BuildRequest `json:"req"`
}

func (w *WorkerPool) Start(ctx context.Context) error {
	w.logger.Info("starting NATS bounded worker pool", slog.Int("concurrency", w.concurrency))

	return w.queue.Subscribe(ctx, "jobs.pending", func(data []byte) {
		var payload JobPayload
		if err := json.Unmarshal(data, &payload); err != nil {
			w.logger.Error("failed to unmarshal job payload", slog.String("error", err.Error()))
			return
		}

		w.sem <- struct{}{}
		go func() {
			defer func() { <-w.sem }()
			w.dispatcher.dispatchAsync(context.Background(), payload.Job, payload.Req)
		}()
	})
}
