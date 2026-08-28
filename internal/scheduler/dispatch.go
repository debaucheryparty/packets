package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/waris4ly/packets/internal/provider"
	"github.com/waris4ly/packets/internal/storage"
	"github.com/waris4ly/packets/pkg/apitypes"
)

type Dispatcher struct {
	logger       *slog.Logger
	store        *storage.JobStore
	stateMachine *StateMachine
	queue        *storage.NATSQueue
	providers    map[apitypes.ProviderName]provider.BuildProvider
}

func NewDispatcher(logger *slog.Logger, store *storage.JobStore, queue *storage.NATSQueue, providers map[apitypes.ProviderName]provider.BuildProvider) *Dispatcher {
	return &Dispatcher{
		logger:       logger,
		store:        store,
		stateMachine: NewStateMachine(),
		queue:        queue,
		providers:    providers,
	}
}

func (d *Dispatcher) Submit(ctx context.Context, req apitypes.BuildRequest, cacheKey string) (apitypes.JobID, bool, error) {
	// check cache first
	if ref, hit, err := d.store.Lookup(ctx, cacheKey); err == nil && hit {
		d.logger.InfoContext(ctx, "cache hit", slog.String("cache_key", cacheKey), slog.String("ref", string(ref)))
		JobsSubmittedTotal.WithLabelValues(string(req.Toolchain), "true").Inc()
		return apitypes.JobID(fmt.Sprintf("hit_%s", cacheKey)), true, nil
	} else if err != nil {
		d.logger.WarnContext(ctx, "cache lookup failed, proceeding to build", slog.String("err", err.Error()))
	}

	jobID := apitypes.JobID(fmt.Sprintf("j_%s", uuid.New().String()[:8]))

	providerName := apitypes.ProviderDockerWorker
	if req.Toolchain == apitypes.ToolchainSwift || req.Toolchain == apitypes.ToolchainObjC || req.Toolchain == apitypes.ToolchainFlutter {
		providerName = apitypes.ProviderGitHubActions
	}

	job := apitypes.Job{
		ID:          jobID,
		Toolchain:   req.Toolchain,
		CacheKey:    cacheKey,
		State:       apitypes.JobStatePending,
		Provider:    providerName,
		SubmittedAt: time.Now().UTC(),
	}

	if err := d.store.CreateJob(ctx, job); err != nil {
		return "", false, fmt.Errorf("Dispatcher.Submit create: %w", err)
	}

	JobsSubmittedTotal.WithLabelValues(string(req.Toolchain), "false").Inc()

	payload, _ := json.Marshal(JobPayload{Job: job, Req: req})
	if d.queue != nil {
		if err := d.queue.Publish(ctx, "jobs.pending", payload); err != nil {
			d.logger.ErrorContext(ctx, "failed to publish job to queue, falling back to synchronous execution", slog.String("error", err.Error()))
			go d.dispatchAsync(context.Background(), job, req)
		}
	} else {
		go d.dispatchAsync(context.Background(), job, req)
	}

	return jobID, false, nil
}

func (d *Dispatcher) dispatchAsync(ctx context.Context, job apitypes.Job, req apitypes.BuildRequest) {
	p, ok := d.providers[job.Provider]
	if !ok {
		d.logger.ErrorContext(ctx, "provider not found", slog.String("provider", string(job.Provider)))
		_ = d.updateState(ctx, job.ID, apitypes.JobStateFailed, apitypes.JobStatePending)
		JobsFailedTotal.WithLabelValues(string(job.Toolchain), string(job.Provider)).Inc()
		return
	}

	_ = d.updateState(ctx, job.ID, apitypes.JobStateDispatched, apitypes.JobStatePending)

	// dispatch to CI provider or docker worker
	_, err := p.Dispatch(ctx, job)
	if err != nil {
		d.logger.ErrorContext(ctx, "dispatch failed",
			slog.String("job_id", string(job.ID)),
			slog.String("provider", string(job.Provider)),
			slog.String("error", err.Error()),
		)
		JobsFailedTotal.WithLabelValues(string(job.Toolchain), string(job.Provider)).Inc()

		if job.Provider == apitypes.ProviderGitHubActions && errorsIs(err, provider.ErrProviderExhausted) {
			d.logger.WarnContext(ctx, "primary provider exhausted, failing over to circleci", slog.String("job_id", string(job.ID)))

			fallbackJob := job
			fallbackJob.Provider = apitypes.ProviderCircleCI
			if fallbackProvider, fallbackOk := d.providers[apitypes.ProviderCircleCI]; fallbackOk {
				if _, fallbackErr := fallbackProvider.Dispatch(ctx, fallbackJob); fallbackErr == nil {
					return
				} else {
					d.logger.ErrorContext(ctx, "fallback provider also failed", slog.String("error", fallbackErr.Error()))
				}
			}
		}

		_ = d.updateState(ctx, job.ID, apitypes.JobStateFallbackLocal, apitypes.JobStateDispatched)
		return
	}

	_ = d.updateState(ctx, job.ID, apitypes.JobStateRunning, apitypes.JobStateDispatched)
}

func (d *Dispatcher) updateState(ctx context.Context, id apitypes.JobID, to, from apitypes.JobState) error {
	if err := d.stateMachine.ValidateTransition(from, to); err != nil {
		d.logger.ErrorContext(ctx, "invalid state transition", slog.String("error", err.Error()))
		return err
	}
	return d.store.UpdateJobState(ctx, id, to)
}

func errorsIs(err, target error) bool {
	return err.Error() == target.Error()
}
