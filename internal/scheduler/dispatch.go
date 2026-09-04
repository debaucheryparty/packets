package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/debaucheryparty/packets/internal/provider"
	"github.com/debaucheryparty/packets/internal/storage"
	"github.com/debaucheryparty/packets/internal/worker"
	"github.com/debaucheryparty/packets/pkg/apitypes"
	"github.com/google/uuid"
)

type Dispatcher struct {
	logger       *slog.Logger
	store        *storage.JobStore
	stateMachine *StateMachine
	providers    map[apitypes.ProviderName]provider.BuildProvider
	executor     *worker.Executor
	limiter      *QuotaLimiter
	logBroker    *LogBroker
}

func NewDispatcher(logger *slog.Logger, store *storage.JobStore, providers map[apitypes.ProviderName]provider.BuildProvider, executor *worker.Executor, limiter *QuotaLimiter, logBroker *LogBroker) *Dispatcher {
	return &Dispatcher{
		logger:       logger,
		store:        store,
		stateMachine: NewStateMachine(),
		providers:    providers,
		executor:     executor,
		limiter:      limiter,
		logBroker:    logBroker,
	}
}

func (d *Dispatcher) Submit(ctx context.Context, req apitypes.BuildRequest, cacheKey, owner string) (apitypes.JobID, bool, error) {
	if ref, hit, err := d.store.Lookup(ctx, cacheKey); err == nil && hit {
		d.logger.InfoContext(ctx, "cache hit", slog.String("cache_key", cacheKey), slog.String("ref", string(ref)))
		JobsSubmittedTotal.WithLabelValues(string(req.Toolchain), "true").Inc()

		cachedJob := apitypes.Job{
			ID:          apitypes.JobID(fmt.Sprintf("cached_%s", cacheKey[:8])),
			State:       apitypes.JobStateSucceeded,
			Toolchain:   req.Toolchain,
			CacheKey:    cacheKey,
			ArtifactRef: ref,
			Owner:       owner,
			SubmittedAt: time.Now().UTC(),
		}
		_ = d.store.CreateJob(ctx, cachedJob)
		return cachedJob.ID, true, nil
	}

	if d.limiter != nil {
		if err := d.limiter.Acquire(owner); err != nil {
			return "", false, err
		}
	}

	jobID := apitypes.JobID(fmt.Sprintf("j_%s", uuid.New().String()[:8]))

	runner := req.Runner
	if runner == "" {
		runner = apitypes.RunnerDocker
	}

	job := apitypes.Job{
		ID:            jobID,
		Toolchain:     req.Toolchain,
		CacheKey:      cacheKey,
		State:         apitypes.JobStatePending,
		Runner:        runner,
		SourceMode:    req.SourceMode,
		SnapshotRef:   req.SnapshotRef,
		CommandArgs:   req.CommandArgs,
		ArtifactPaths: req.ArtifactPaths,
		Image:         req.DockerImage,
		Owner:         owner,
		SubmittedAt:   time.Now().UTC(),
	}

	if err := d.store.CreateJob(ctx, job); err != nil {
		if d.limiter != nil {
			d.limiter.Release(owner)
		}
		return "", false, fmt.Errorf("Dispatcher.Submit create: %w", err)
	}

	JobsSubmittedTotal.WithLabelValues(string(req.Toolchain), "false").Inc()
	go d.dispatchAsync(context.Background(), job, req)

	return jobID, false, nil
}

func (d *Dispatcher) dispatchAsync(ctx context.Context, job apitypes.Job, req apitypes.BuildRequest) {
	defer func() {
		if d.limiter != nil {
			d.limiter.Release(job.Owner)
		}
		if d.logBroker != nil {
			d.logBroker.CloseJob(job.ID)
		}
	}()

	_ = d.updateState(ctx, job.ID, apitypes.JobStateDispatched, apitypes.JobStatePending)

	switch job.Runner {
	case apitypes.RunnerDocker:
		_ = d.updateState(ctx, job.ID, apitypes.JobStateRunning, apitypes.JobStateDispatched)
		result, err := d.executor.Execute(ctx, job)
		if err != nil || result.ExitCode != 0 {
			errMsg := ""
			if err != nil {
				errMsg = err.Error()
			} else {
				errMsg = fmt.Sprintf("exited %d: %s", result.ExitCode, result.Stderr)
			}
			_ = d.store.FailJob(ctx, job.ID, errMsg)
			JobsFailedTotal.WithLabelValues(string(job.Toolchain), string(job.Runner)).Inc()
			return
		}
		if err := d.store.CompleteJob(ctx, job.ID, result.ArtifactRef, job.CacheKey); err != nil {
			d.logger.ErrorContext(ctx, "CompleteJob failed", slog.String("job_id", string(job.ID)), slog.String("err", err.Error()))
		}

	case apitypes.RunnerGitHub:
		p, ok := d.providers[apitypes.ProviderGitHubActions]
		if !ok {
			_ = d.store.FailJob(ctx, job.ID, "github provider not configured")
			return
		}
		if _, err := p.Dispatch(ctx, job); err != nil {
			_ = d.store.FailJob(ctx, job.ID, err.Error())
			JobsFailedTotal.WithLabelValues(string(job.Toolchain), string(job.Runner)).Inc()
			return
		}
		_ = d.updateState(ctx, job.ID, apitypes.JobStateRunning, apitypes.JobStateDispatched)

	case apitypes.RunnerLocal:
		_ = d.updateState(ctx, job.ID, apitypes.JobStateRunning, apitypes.JobStateDispatched)
		result, err := d.executor.Execute(ctx, job)
		if err != nil || result.ExitCode != 0 {
			errMsg := ""
			if err != nil {
				errMsg = err.Error()
			} else {
				errMsg = fmt.Sprintf("exited %d", result.ExitCode)
			}
			_ = d.store.FailJob(ctx, job.ID, errMsg)
			return
		}
		_ = d.store.CompleteJob(ctx, job.ID, result.ArtifactRef, job.CacheKey)

	default:
		_ = d.store.FailJob(ctx, job.ID, fmt.Sprintf("unknown runner: %s", job.Runner))
	}
}

func (d *Dispatcher) updateState(ctx context.Context, id apitypes.JobID, to, from apitypes.JobState) error {
	if err := d.stateMachine.ValidateTransition(from, to); err != nil {
		d.logger.ErrorContext(ctx, "invalid state transition", slog.String("error", err.Error()))
		return err
	}
	return d.store.UpdateJobState(ctx, id, to)
}

func (d *Dispatcher) RecoverPendingJobs(ctx context.Context) error {
	jobs, err := d.store.ListJobsByState(ctx, apitypes.JobStatePending, apitypes.JobStateDispatched)
	if err != nil {
		return fmt.Errorf("RecoverPendingJobs: %w", err)
	}
	for _, job := range jobs {
		go func(j apitypes.Job) {
			d.dispatchAsync(ctx, j, apitypes.BuildRequest{
				Toolchain:     j.Toolchain,
				Runner:        j.Runner,
				SourceMode:    j.SourceMode,
				SnapshotRef:   j.SnapshotRef,
				CommandArgs:   j.CommandArgs,
				ArtifactPaths: j.ArtifactPaths,
				DockerImage:   j.Image,
			})
		}(job)
	}
	return nil
}
