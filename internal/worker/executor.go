package worker

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/debaucheryparty/packets/internal/cache"
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
	depCache     *cache.DepManager
	tempDir      string
	workspaceDir string
}

func defaultWorkspaceRootDir(tempDir string) string {
	if dir := os.Getenv("PACKETS_WORKSPACE_ROOT_DIR"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		return filepath.Join(home, ".packets", "workspaces")
	}
	return filepath.Join(tempDir, "packets-workspaces")
}

func NewExecutor(logger *slog.Logger, docker *DockerClient, store storage.ObjectStore, registry *toolchain.Registry, logPublisher LogPublisher, tempDir string) *Executor {
	if tempDir == "" {
		tempDir = os.TempDir()
	}
	wsDir := defaultWorkspaceRootDir(tempDir)
	_ = os.MkdirAll(wsDir, 0o755)

	return &Executor{
		logger:       logger,
		docker:       docker,
		store:        store,
		registry:     registry,
		logPublisher: logPublisher,
		depCache:     cache.NewDepManagerFromEnv(),
		tempDir:      tempDir,
		workspaceDir: wsDir,
	}
}

func (e *Executor) SetWorkspaceDir(dir string) {
	e.workspaceDir = dir
	_ = os.MkdirAll(dir, 0o755)
}
func (e *Executor) Execute(ctx context.Context, job apitypes.Job) (apitypes.ExecutionResult, error) {
	var srcDir string
	var cleanup func()

	if job.SourceMode == apitypes.SourceModeWorkspace {
		owner := job.Owner
		if owner == "" {
			owner = "default"
		}
		projID := job.ProjectID
		if projID == "" {
			projID = "default"
		}
		srcDir = filepath.Join(e.workspaceDir, owner, projID)
		if err := os.MkdirAll(srcDir, 0o755); err != nil {
			return apitypes.ExecutionResult{}, fmt.Errorf("Executor.Execute mkdir persistent workspace: %w", err)
		}
		cleanup = func() {} // Persistent workspace survives between commands
	} else {
		jobDir, err := os.MkdirTemp(e.tempDir, "packets-job-"+string(job.ID)+"-")
		if err != nil {
			return apitypes.ExecutionResult{}, fmt.Errorf("Executor.Execute mkdirtemp: %w", err)
		}
		cleanup = func() { _ = os.RemoveAll(jobDir) }
		srcDir = filepath.Join(jobDir, "workspace")
		if err := os.MkdirAll(srcDir, 0o755); err != nil {
			cleanup()
			return apitypes.ExecutionResult{}, fmt.Errorf("Executor.Execute mkdir src: %w", err)
		}
	}
	defer cleanup()

	if job.SnapshotRef != "" && e.store != nil {
		refFile := filepath.Join(srcDir, ".packets_snapshot")
		currentRef, _ := os.ReadFile(refFile)
		if string(currentRef) != job.SnapshotRef {
			if err := workspace.ExtractSnapshot(ctx, e.store, job.Owner, job.SnapshotRef, srcDir); err != nil {
				return apitypes.ExecutionResult{}, fmt.Errorf("Executor.Execute extract: %w", err)
			}
			_ = os.WriteFile(refFile, []byte(job.SnapshotRef), 0o644)
		}
	}

	var execResult apitypes.ExecutionResult
	if job.Runner == apitypes.RunnerHost || job.Runner == apitypes.RunnerLocal {
		res, err := e.executeHost(ctx, job, srcDir)
		if err != nil {
			return res, err
		}
		execResult = res
	} else {
		image := job.Image
		if image == "" {
			image = e.resolveImage(job)
		}
		if image == "" {
			return apitypes.ExecutionResult{
				ExitCode: 1,
				Error:    fmt.Errorf("no runner or docker image available for toolchain %q", job.Toolchain),
			}, nil
		}

		if err := e.docker.PullImage(ctx, image); err != nil {
			return apitypes.ExecutionResult{}, fmt.Errorf("Executor.Execute pull %s: %w", image, err)
		}

		command := job.CommandArgs
		if len(command) == 0 {
			if def, ok := e.registry.Lookup(job.Toolchain); ok {
				command = append([]string{def.LocalCommand}, def.DefaultArgs...)
			}
		}

		logFn := func(line string) {
			if e.logPublisher != nil {
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

		execResult = apitypes.ExecutionResult{
			ExitCode: result.ExitCode,
			Stdout:   result.Stdout,
			Stderr:   result.Stderr,
		}
	}

	if execResult.ExitCode == 0 && e.store != nil && len(job.ArtifactPaths) > 0 {
		ref, err := e.collectArtifacts(ctx, srcDir, job.ArtifactPaths, string(job.ID), job.Owner)
		if err != nil {
			e.logger.WarnContext(ctx, "artifact collection failed", slog.String("job_id", string(job.ID)), slog.String("err", err.Error()))
		} else {
			execResult.ArtifactRef = ref
		}
	}

	return execResult, nil
}

func (e *Executor) executeHost(ctx context.Context, job apitypes.Job, srcDir string) (apitypes.ExecutionResult, error) {
	command := job.CommandArgs
	if len(command) == 0 {
		if def, ok := e.registry.Lookup(job.Toolchain); ok {
			command = append([]string{def.LocalCommand}, def.DefaultArgs...)
		}
	}
	if len(command) == 0 {
		return apitypes.ExecutionResult{ExitCode: 1, Error: fmt.Errorf("no command specified")}, nil
	}

	var cmd *exec.Cmd
	if len(command) == 1 {
		cmdStr := command[0]
		if runtime.GOOS == "windows" {
			cmd = exec.Command("cmd.exe", "/c", cmdStr)
		} else {
			cmd = exec.Command("sh", "-c", cmdStr)
		}
	} else {
		cmd = exec.Command(command[0], command[1:]...)
	}

	cmd.Dir = srcDir
	cmd.Env = os.Environ()

	// Phase 10: Bind persistent dependency caches for this toolchain.
	// This redirects GRADLE_USER_HOME, CARGO_HOME, GOPATH, etc. to stable
	// per-owner directories that survive across sequential builds.
	owner := job.Owner
	if owner == "" {
		owner = "default"
	}
	if e.depCache != nil {
		if binding, err := e.depCache.Bind(ctx, owner, job.Toolchain, srcDir); err == nil {
			// Overlay cache env vars on top of the current environment.  A cache
			// env var always wins over the inherited system value.
			cmd.Env = overrideEnv(cmd.Env, binding.EnvSlice())
			// Materialise workspace-relative cache dirs (e.g. node_modules, .west).
			_ = binding.ApplyBindMounts(srcDir)
		} else {
			e.logger.WarnContext(ctx, "dep cache bind failed",
				slog.String("toolchain", string(job.Toolchain)),
				slog.String("err", err.Error()))
		}
	}

	prepareProcessGroup(cmd)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return apitypes.ExecutionResult{}, fmt.Errorf("executeHost stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return apitypes.ExecutionResult{}, fmt.Errorf("executeHost stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return apitypes.ExecutionResult{
			ExitCode: 1,
			Error:    err,
		}, nil
	}

	// Monitor context cancellation to kill the full process tree
	doneCh := make(chan struct{})
	defer close(doneCh)

	go func() {
		select {
		case <-ctx.Done():
			_ = killProcessTree(cmd)
		case <-doneCh:
		}
	}()

	var stdoutBuf, stderrBuf bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(2)

	publish := func(line string) {
		if e.logPublisher != nil {
			e.logPublisher.Publish(job.ID, line)
		}
	}

	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(io.TeeReader(stdoutPipe, &stdoutBuf))
		for scanner.Scan() {
			publish(scanner.Text())
		}
	}()

	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(io.TeeReader(stderrPipe, &stderrBuf))
		for scanner.Scan() {
			publish(scanner.Text())
		}
	}()

	wg.Wait()
	waitErr := cmd.Wait()

	exitCode := 0
	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	return apitypes.ExecutionResult{
		ExitCode: exitCode,
		Stdout:   stdoutBuf.String(),
		Stderr:   stderrBuf.String(),
		Error:    waitErr,
	}, nil
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

// overrideEnv merges base env slice with overrides.  If a key in overrides
// already exists in base, the base value is replaced; otherwise the override
// is appended.  This lets dep-cache bindings win over inherited system values.
func overrideEnv(base, overrides []string) []string {
	if len(overrides) == 0 {
		return base
	}
	// Build a lookup of override keys.
	keys := make(map[string]string, len(overrides))
	for _, kv := range overrides {
		idx := len(kv)
		for i, c := range kv {
			if c == '=' {
				idx = i
				break
			}
		}
		keys[kv[:idx]] = kv
	}
	// Rebuild base, replacing any key that appears in overrides.
	out := make([]string, 0, len(base)+len(overrides))
	for _, kv := range base {
		idx := len(kv)
		for i, c := range kv {
			if c == '=' {
				idx = i
				break
			}
		}
		key := kv[:idx]
		if replacement, ok := keys[key]; ok {
			out = append(out, replacement)
			delete(keys, key) // mark consumed
		} else {
			out = append(out, kv)
		}
	}
	// Append any overrides that were not already in base.
	for _, kv := range overrides {
		idx := len(kv)
		for i, c := range kv {
			if c == '=' {
				idx = i
				break
			}
		}
		if _, stillPresent := keys[kv[:idx]]; stillPresent {
			out = append(out, kv)
		}
	}
	return out
}
