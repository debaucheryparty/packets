package worker

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/debaucheryparty/packets/internal/storage"
	"github.com/debaucheryparty/packets/internal/toolchain"
	"github.com/debaucheryparty/packets/internal/workspace"
	"github.com/debaucheryparty/packets/pkg/apitypes"
)

type LogPublisher interface {
	Publish(jobID apitypes.JobID, line string)
}

type Executor struct {
	logger       *slog.Logger
	docker       *DockerClient
	store        storage.ObjectStore
	registry     *toolchain.Registry
	logPublisher LogPublisher
	tempDir      string
}

func NewExecutor(logger *slog.Logger, docker *DockerClient, store storage.ObjectStore, registry *toolchain.Registry, logPublisher LogPublisher, tempDir string) *Executor {
	if tempDir == "" {
		tempDir = os.TempDir()
	}
	return &Executor{
		logger:       logger,
		docker:       docker,
		store:        store,
		registry:     registry,
		logPublisher: logPublisher,
		tempDir:      tempDir,
	}
}

func (e *Executor) Execute(ctx context.Context, job apitypes.Job) (apitypes.ExecutionResult, error) {
	jobDir, err := os.MkdirTemp(e.tempDir, "packets-job-"+string(job.ID)+"-")
	if err != nil {
		return apitypes.ExecutionResult{}, fmt.Errorf("Executor.Execute mkdirtemp: %w", err)
	}
	defer os.RemoveAll(jobDir)

	srcDir := filepath.Join(jobDir, "workspace")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		return apitypes.ExecutionResult{}, fmt.Errorf("Executor.Execute mkdir src: %w", err)
	}

	if job.SnapshotRef != "" {
		if err := workspace.ExtractSnapshot(ctx, e.store, job.Owner, job.SnapshotRef, srcDir); err != nil {
			return apitypes.ExecutionResult{}, fmt.Errorf("Executor.Execute extract: %w", err)
		}
	}

	image := e.resolveImage(job)

	if err := e.docker.PullImage(ctx, image); err != nil {
		return apitypes.ExecutionResult{}, fmt.Errorf("Executor.Execute pull %s: %w", image, err)
	}

	command := job.CommandArgs
	if len(command) == 0 {
		if def, ok := e.registry.Lookup(job.Toolchain); ok {
			command = append([]string{def.LocalCommand}, def.DefaultArgs...)
		}
	}

	var logFn func(string)
	if e.logPublisher != nil {
		logFn = func(line string) {
			e.logPublisher.Publish(job.ID, line)
		}
	}

	result, err := e.docker.Run(ctx, RunOpts{
		Image:     image,
		MountPath: srcDir,
		Command:   command,
		Timeout:   30 * time.Minute,
		LogFunc:   logFn,
	})
	if err != nil {
		return apitypes.ExecutionResult{
			ExitCode: 1,
			Stdout:   result.Stdout,
			Stderr:   result.Stderr,
			Error:    err,
		}, nil
	}

	execResult := apitypes.ExecutionResult{
		ExitCode: result.ExitCode,
		Stdout:   result.Stdout,
		Stderr:   result.Stderr,
	}

	if result.ExitCode == 0 && e.store != nil && len(job.ArtifactPaths) > 0 {
		ref, err := e.collectArtifacts(ctx, srcDir, job.ArtifactPaths, string(job.ID), job.Owner)
		if err != nil {
			e.logger.WarnContext(ctx, "artifact collection failed", slog.String("job_id", string(job.ID)), slog.String("err", err.Error()))
		} else {
			execResult.ArtifactRef = ref
		}
	}

	return execResult, nil
}

func (e *Executor) resolveImage(job apitypes.Job) string {
	if e.registry != nil {
		if def, ok := e.registry.Lookup(job.Toolchain); ok && def.DockerImage != "" {
			return def.DockerImage
		}
	}
	if job.Image != "" {
		return job.Image
	}
	return "ubuntu:24.04"
}

func (e *Executor) collectArtifacts(ctx context.Context, srcDir string, paths []string, jobID, owner string) (apitypes.ArtifactRef, error) {
	pr, pw, err := createTarGzPipe(srcDir, paths)
	if err != nil {
		return "", fmt.Errorf("collectArtifacts tar: %w", err)
	}

	key := fmt.Sprintf("%s/artifacts/%s/output.tar.gz", owner, jobID)
	if err := e.store.Upload(ctx, key, pr, -1); err != nil {
		pw.Close() //nolint:errcheck
		return "", fmt.Errorf("collectArtifacts upload: %w", err)
	}
	pw.Close() //nolint:errcheck

	return apitypes.ArtifactRef(key), nil
}

func (e *Executor) Dispatch(ctx context.Context, job apitypes.Job) (apitypes.JobID, error) {
	result, err := e.Execute(ctx, job)
	if err != nil {
		return "", err
	}
	if result.ExitCode != 0 {
		return "", fmt.Errorf("build exited with code %d: %s", result.ExitCode, result.Stderr)
	}
	return job.ID, nil
}

func (e *Executor) Status(ctx context.Context, id apitypes.JobID) (apitypes.JobState, error) {
	return apitypes.JobStateSucceeded, nil
}
