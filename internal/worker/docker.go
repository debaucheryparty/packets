package worker

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

type RunOpts struct {
	Image       string
	MountPath   string
	Command     []string
	Env         []string
	Timeout     time.Duration
	MemoryLimit string
	CPULimit    string
	LogFunc     func(string)
}

type RunResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

type DockerClient struct {
	logger *slog.Logger
}

func NewDockerClient(logger *slog.Logger) *DockerClient {
	return &DockerClient{logger: logger}
}

func (d *DockerClient) PullImage(ctx context.Context, image string) error {
	cmd := exec.CommandContext(ctx, "docker", "pull", image)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("DockerClient.PullImage %q: %w", image, err)
	}
	return nil
}

func (d *DockerClient) Run(ctx context.Context, opts RunOpts) (RunResult, error) {
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 30 * time.Minute
	}
	memLimit := opts.MemoryLimit
	if memLimit == "" {
		memLimit = "2g"
	}
	cpuLimit := opts.CPULimit
	if cpuLimit == "" {
		cpuLimit = "2"
	}

	args := []string{
		"run", "--rm",
		"-v", opts.MountPath + ":/workspace",
		"-w", "/workspace",
		"--memory=" + memLimit,
		"--cpus=" + cpuLimit,
		"--pids-limit=512",
		"--network=none",
	}
	for _, e := range opts.Env {
		args = append(args, "-e", e)
	}
	args = append(args, opts.Image)
	args = append(args, opts.Command...)

	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(tctx, "docker", args...)

	var stdoutBuf, stderrBuf bytes.Buffer

	if opts.LogFunc != nil {
		stdoutR, stdoutW := io.Pipe()
		stderrR, stderrW := io.Pipe()

		cmd.Stdout = stdoutW
		cmd.Stderr = stderrW

		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			scanner := bufio.NewScanner(stdoutR)
			for scanner.Scan() {
				line := scanner.Text()
				stdoutBuf.WriteString(line + "\n")
				opts.LogFunc(line)
			}
		}()

		go func() {
			defer wg.Done()
			scanner := bufio.NewScanner(stderrR)
			for scanner.Scan() {
				line := scanner.Text()
				stderrBuf.WriteString(line + "\n")
				opts.LogFunc(line)
			}
		}()

		err := cmd.Start()
		if err != nil {
			stdoutW.Close() //nolint:errcheck
			stderrW.Close() //nolint:errcheck
			return RunResult{}, fmt.Errorf("DockerClient.Run start: %w", err)
		}

		waitErr := cmd.Wait()
		stdoutW.Close() //nolint:errcheck
		stderrW.Close() //nolint:errcheck
		wg.Wait()

		result := RunResult{
			Stdout: stdoutBuf.String(),
			Stderr: stderrBuf.String(),
		}

		if waitErr != nil {
			var exitErr *exec.ExitError
			if ok := isExitError(waitErr, &exitErr); ok {
				result.ExitCode = exitErr.ExitCode()
				return result, nil
			}
			return result, fmt.Errorf("DockerClient.Run wait: %w", waitErr)
		}
		return result, nil
	}

	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err := cmd.Run()
	result := RunResult{
		Stdout: stdoutBuf.String(),
		Stderr: stderrBuf.String(),
	}

	if err != nil {
		var exitErr *exec.ExitError
		if ok := isExitError(err, &exitErr); ok {
			result.ExitCode = exitErr.ExitCode()
			return result, nil
		}
		return result, fmt.Errorf("DockerClient.Run: %w", err)
	}
	return result, nil
}

func isExitError(err error, target **exec.ExitError) bool {
	if ee, ok := err.(*exec.ExitError); ok { //nolint:errorlint
		*target = ee
		return true
	}
	return false
}

func (d *DockerClient) LogsContainer(ctx context.Context, containerID string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "docker", "logs", containerID)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("DockerClient.LogsContainer %q: %w", containerID, err)
	}
	return out, nil
}

func trimNL(s string) string { //nolint:unused
	return strings.TrimRight(s, "\r\n")
}

func parseExitCode(s string) int { //nolint:unused
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}
