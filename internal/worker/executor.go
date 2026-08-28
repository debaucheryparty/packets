package worker

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/waris4ly/packets/internal/storage"
	"github.com/waris4ly/packets/pkg/apitypes"
)

type Executor struct {
	logger *slog.Logger
	docker *DockerClient
	queue  *storage.NATSQueue
}

func NewExecutor(logger *slog.Logger, docker *DockerClient, queue *storage.NATSQueue) *Executor {
	return &Executor{
		logger: logger,
		docker: docker,
		queue:  queue,
	}
}

func (e *Executor) Dispatch(ctx context.Context, job apitypes.Job) (apitypes.JobID, error) {
	e.logger.InfoContext(ctx, "starting local docker worker execution",
		slog.String("job_id", string(job.ID)),
		slog.String("toolchain", string(job.Toolchain)),
	)

	image := "alpine:latest"
	if err := e.docker.PullImage(ctx, image); err != nil {
		return "", fmt.Errorf("Executor.Dispatch pull image: %w", err)
	}

	containerID, err := e.docker.RunContainer(ctx, image, "/workspace", []string{"echo", "built"})
	if err != nil {
		return "", fmt.Errorf("Executor.Dispatch run container: %w", err)
	}

	e.logger.DebugContext(ctx, "container started", slog.String("container_id", containerID))

	go func() {
		subject := fmt.Sprintf("job.%s.logs", job.ID)
		_ = e.docker.StreamLogs(context.Background(), containerID, func(line string) {
			if e.queue != nil {
				_ = e.queue.Publish(context.Background(), subject, []byte(line))
			}
		})
	}()

	if waitErr := e.docker.WaitContainer(ctx, containerID); waitErr != nil {
		logs, _ := e.docker.LogsContainer(context.Background(), containerID)
		e.logger.ErrorContext(ctx, "container failed",
			slog.String("container_id", containerID),
			slog.String("logs", strings.TrimSpace(string(logs))),
		)
		return "", fmt.Errorf("Executor.Dispatch wait: %w", waitErr)
	}

	return job.ID, nil
}

func (e *Executor) Status(ctx context.Context, id apitypes.JobID) (apitypes.JobState, error) {
	return apitypes.JobStateSucceeded, nil
}

func (e *Executor) FetchArtifact(ctx context.Context, id apitypes.JobID) (io.ReadCloser, error) {
	return nil, fmt.Errorf("DockerWorker.FetchArtifact not implemented")
}
