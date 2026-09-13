package worker

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type RunnerTrustLevel string

const (
	TrustUntrustedContainer RunnerTrustLevel = "UNTRUSTED_CONTAINER"
	TrustTrustedHost        RunnerTrustLevel = "TRUSTED_HOST"
)

type DockerSecurityPolicy struct {
	AllowNetwork     bool
	DropCapabilities bool
	NoNewPrivileges  bool
	MemoryLimit      string
	CPULimit         string
	PidsLimit        int
}

func DefaultDockerSecurityPolicy() DockerSecurityPolicy {
	return DockerSecurityPolicy{
		AllowNetwork:     false,
		DropCapabilities: true,
		NoNewPrivileges:  true,
		MemoryLimit:      "2g",
		CPULimit:         "2",
		PidsLimit:        512,
	}
}

func ValidateDockerMount(mountPath string) error {
	cleaned := filepath.Clean(mountPath)
	lower := strings.ToLower(cleaned)
	if strings.Contains(lower, "docker.sock") {
		return errors.New("mounting docker socket inside container is forbidden")
	}
	if cleaned == "/" || cleaned == "\\" {
		return errors.New("mounting root filesystem is forbidden")
	}
	forbidden := []string{"/etc", "/boot", "/dev", "/sys", "/proc", "/root", `C:\Windows`, `C:\Program Files`}
	for _, f := range forbidden {
		if strings.HasPrefix(cleaned, filepath.Clean(f)) {
			return fmt.Errorf("mounting system path %s is forbidden", cleaned)
		}
	}
	return nil
}

func BuildDockerArgs(opts RunOpts, policy DockerSecurityPolicy) ([]string, error) {
	if err := ValidateDockerMount(opts.MountPath); err != nil {
		return nil, err
	}

	for _, cmd := range opts.Command {
		if strings.Contains(cmd, "--privileged") {
			return nil, errors.New("privileged mode is forbidden")
		}
	}

	memLimit := opts.MemoryLimit
	if memLimit == "" {
		memLimit = policy.MemoryLimit
	}
	if memLimit == "" {
		memLimit = "2g"
	}

	cpuLimit := opts.CPULimit
	if cpuLimit == "" {
		cpuLimit = policy.CPULimit
	}
	if cpuLimit == "" {
		cpuLimit = "2"
	}

	pidsLimit := policy.PidsLimit
	if pidsLimit <= 0 {
		pidsLimit = 512
	}

	args := []string{
		"run", "--rm",
		"-v", opts.MountPath + ":/workspace",
		"-w", "/workspace",
		"--memory=" + memLimit,
		"--cpus=" + cpuLimit,
		fmt.Sprintf("--pids-limit=%d", pidsLimit),
	}

	if policy.DropCapabilities {
		args = append(args, "--cap-drop=ALL")
	}
	if policy.NoNewPrivileges {
		args = append(args, "--security-opt=no-new-privileges:true")
	}

	if policy.AllowNetwork {
		args = append(args, "--network=bridge")
	} else {
		args = append(args, "--network=none")
	}

	for _, e := range opts.Env {
		args = append(args, "-e", e)
	}
	args = append(args, opts.Image)
	args = append(args, opts.Command...)

	return args, nil
}

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

func (d *DockerClient) CheckAvailability(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "docker", "info")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker daemon unavailable: %w", err)
	}
	return nil
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

	args, err := BuildDockerArgs(opts, DefaultDockerSecurityPolicy())
	if err != nil {
		return RunResult{}, fmt.Errorf("DockerClient.Run security check: %w", err)
	}

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
			_ = stdoutW.Close()
			_ = stderrW.Close()
			return RunResult{}, fmt.Errorf("DockerClient.Run start: %w", err)
		}

		waitErr := cmd.Wait()
		_ = stdoutW.Close()
		_ = stderrW.Close()
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

	err = cmd.Run()
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
	var ee *exec.ExitError
	if errors.As(err, &ee) {
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
