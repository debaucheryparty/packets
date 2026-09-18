package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/debaucheryparty/packets/internal/provider"
	"github.com/debaucheryparty/packets/internal/storage"
	"github.com/debaucheryparty/packets/internal/worker"
	"github.com/debaucheryparty/packets/pkg/apitypes"
	"github.com/google/uuid"
)

type Dispatcher struct {
	logger        *slog.Logger
	store         *storage.JobStore
	stateMachine  *StateMachine
	providers     map[apitypes.ProviderName]provider.BuildProvider
	executor      *worker.Executor
	limiter       *QuotaLimiter
	logBroker     *LogBroker
	workerPool    *WorkerPool
	activeMu      sync.Mutex
	activeCancels map[apitypes.JobID]context.CancelFunc
}

func NewDispatcher(logger *slog.Logger, store *storage.JobStore, providers map[apitypes.ProviderName]provider.BuildProvider, executor *worker.Executor, limiter *QuotaLimiter, logBroker *LogBroker) *Dispatcher {
	return &Dispatcher{
		logger:        logger,
		store:         store,
		stateMachine:  NewStateMachine(),
		providers:     providers,
		executor:      executor,
		limiter:       limiter,
		logBroker:     logBroker,
		activeCancels: make(map[apitypes.JobID]context.CancelFunc),
	}
}

func (d *Dispatcher) SetWorkerPool(wp *WorkerPool) {
	d.workerPool = wp
}

func (d *Dispatcher) WorkerPool() *WorkerPool {
	return d.workerPool
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
		ProjectID:     req.ProjectID,
		SubmittedAt:   time.Now().UTC(),
	}

	if err := d.store.CreateJob(ctx, job); err != nil {
		if d.limiter != nil {
			d.limiter.Release(owner)
		}
		return "", false, fmt.Errorf("Dispatcher.Submit create: %w", err)
	}

	JobsSubmittedTotal.WithLabelValues(string(req.Toolchain), "false").Inc()
	go d.dispatchAsync(context.Background(), job)

	return jobID, false, nil
}

func (d *Dispatcher) dispatchAsync(ctx context.Context, job apitypes.Job) {
	jobCtx, cancel := context.WithCancel(ctx)
	d.activeMu.Lock()
	d.activeCancels[job.ID] = cancel
	d.activeMu.Unlock()

	defer func() {
		d.activeMu.Lock()
		delete(d.activeCancels, job.ID)
		d.activeMu.Unlock()
		cancel()

		if d.limiter != nil {
			d.limiter.Release(job.Owner)
		}
		if d.logBroker != nil {
			d.logBroker.CloseJob(job.ID)
		}
	}()

	_ = d.updateState(jobCtx, job.ID, apitypes.JobStateDispatched, apitypes.JobStatePending)

	switch job.Runner {
	case apitypes.RunnerDocker:
		_ = d.updateState(jobCtx, job.ID, apitypes.JobStateRunning, apitypes.JobStateDispatched)
		var result apitypes.ExecutionResult
		var err error
		if d.workerPool != nil && d.workerPool.WorkerCount() > 0 {
			result, _, err = d.workerPool.ExecuteOnWorker(jobCtx, job)
		} else if d.executor != nil {
			result, err = d.executor.Execute(jobCtx, job)
		} else {
			_ = d.store.FailJob(jobCtx, job.ID, "executor not configured")
			return
		}
		if err != nil || result.ExitCode != 0 {
			errMsg := ""
			if err != nil {
				errMsg = err.Error()
			} else {
				errMsg = fmt.Sprintf("exited %d: %s", result.ExitCode, result.Stderr)
			}
			_ = d.store.FailJob(jobCtx, job.ID, errMsg)
			JobsFailedTotal.WithLabelValues(string(job.Toolchain), string(job.Runner)).Inc()
			return
		}
		if err := d.store.CompleteJob(jobCtx, job.ID, result.ArtifactRef, job.CacheKey); err != nil {
			d.logger.ErrorContext(jobCtx, "CompleteJob failed", slog.String("job_id", string(job.ID)), slog.String("err", err.Error()))
		} else {
			d.logger.InfoContext(jobCtx, "job succeeded", slog.String("job_id", string(job.ID)), slog.Duration("duration", time.Since(job.SubmittedAt)), slog.String("runner", string(job.Runner)))
		}

	case apitypes.RunnerGitHub:
		p, ok := d.providers[apitypes.ProviderGitHubActions]
		if !ok {
			_ = d.store.FailJob(jobCtx, job.ID, "github provider not configured")
			return
		}
		if _, err := p.Dispatch(jobCtx, job); err != nil {
			_ = d.store.FailJob(jobCtx, job.ID, err.Error())
			JobsFailedTotal.WithLabelValues(string(job.Toolchain), string(job.Runner)).Inc()
			return
		}
		_ = d.updateState(jobCtx, job.ID, apitypes.JobStateRunning, apitypes.JobStateDispatched)

	case apitypes.RunnerHost, apitypes.RunnerLocal:
		_ = d.updateState(jobCtx, job.ID, apitypes.JobStateRunning, apitypes.JobStateDispatched)
		var result apitypes.ExecutionResult
		var err error
		if d.workerPool != nil && d.workerPool.WorkerCount() > 0 {
			result, _, err = d.workerPool.ExecuteOnWorker(jobCtx, job)
		} else if d.executor != nil {
			result, err = d.executor.Execute(jobCtx, job)
		} else {
			_ = d.store.FailJob(jobCtx, job.ID, "executor not configured")
			return
		}
		if err != nil || result.ExitCode != 0 {
			errMsg := ""
			if err != nil {
				errMsg = err.Error()
			} else if result.Error != nil {
				errMsg = fmt.Sprintf("exited %d: %v", result.ExitCode, result.Error)
			} else if strings.TrimSpace(result.Stderr) != "" {
				errMsg = fmt.Sprintf("exited %d: %s", result.ExitCode, strings.TrimSpace(result.Stderr))
			} else {
				errMsg = fmt.Sprintf("exited %d", result.ExitCode)
			}
			_ = d.store.FailJob(jobCtx, job.ID, errMsg)
			return
		}
		if err := d.store.CompleteJob(jobCtx, job.ID, result.ArtifactRef, job.CacheKey); err != nil {
			d.logger.ErrorContext(jobCtx, "CompleteJob failed", slog.String("job_id", string(job.ID)), slog.String("err", err.Error()))
		} else {
			d.logger.InfoContext(jobCtx, "job succeeded", slog.String("job_id", string(job.ID)), slog.Duration("duration", time.Since(job.SubmittedAt)), slog.String("runner", string(job.Runner)))
		}

	default:
		_ = d.store.FailJob(jobCtx, job.ID, fmt.Sprintf("unknown runner: %s", job.Runner))
	}
}

func (d *Dispatcher) updateState(ctx context.Context, id apitypes.JobID, to, from apitypes.JobState) error {
	if err := d.stateMachine.ValidateTransition(from, to); err != nil {
		d.logger.ErrorContext(ctx, "invalid state transition", slog.String("error", err.Error()))
		return err
	}
	return d.store.UpdateJobState(ctx, id, to)
}

func (d *Dispatcher) Cancel(ctx context.Context, id apitypes.JobID) (bool, error) {
	d.activeMu.Lock()
	cancel, running := d.activeCancels[id]
	if running {
		cancel()
		delete(d.activeCancels, id)
	}
	d.activeMu.Unlock()

	job, err := d.store.GetJob(ctx, id)
	if err != nil {
		return false, err
	}
	if job.State == apitypes.JobStateSucceeded || job.State == apitypes.JobStateFailed {
		return false, nil
	}

	_ = d.store.FailJob(ctx, id, "job cancelled by user")
	d.logger.InfoContext(ctx, "job cancelled", slog.String("job_id", string(id)))
	return true, nil
}

func (d *Dispatcher) ReapStaleJobs(ctx context.Context, timeout time.Duration) (int, error) {
	jobs, err := d.store.ListJobsByState(ctx, apitypes.JobStatePending, apitypes.JobStateDispatched, apitypes.JobStateRunning)
	if err != nil {
		return 0, err
	}
	reaped := 0
	now := time.Now().UTC()
	for _, job := range jobs {
		if now.Sub(job.SubmittedAt) > timeout {
			d.activeMu.Lock()
			if cancel, exists := d.activeCancels[job.ID]; exists {
				cancel()
				delete(d.activeCancels, job.ID)
			}
			d.activeMu.Unlock()

			_ = d.store.FailJob(ctx, job.ID, "job execution timed out")
			d.logger.WarnContext(ctx, "reaped stale job", slog.String("job_id", string(job.ID)), slog.Duration("age", now.Sub(job.SubmittedAt)))
			reaped++
		}
	}
	return reaped, nil
}

func (d *Dispatcher) RecoverPendingJobs(ctx context.Context) error {
	jobs, err := d.store.ListJobsByState(ctx, apitypes.JobStatePending, apitypes.JobStateDispatched)
	if err != nil {
		return fmt.Errorf("RecoverPendingJobs: %w", err)
	}
	for _, job := range jobs {
		go func(j apitypes.Job) {
			d.dispatchAsync(ctx, j)
		}(job)
	}
	return nil
}
