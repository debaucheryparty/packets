package worker

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
)

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

func (d *DockerClient) RunContainer(ctx context.Context, image, workdir string, args []string) (string, error) {
	cmdArgs := []string{"run", "-d", "--rm", "-v", fmt.Sprintf("%s:/workspace", workdir), "-w", "/workspace", image}
	cmdArgs = append(cmdArgs, args...)

	cmd := exec.CommandContext(ctx, "docker", cmdArgs...)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("DockerClient.RunContainer: %w", err)
	}

	containerID := string(out)
	if len(containerID) > 0 && containerID[len(containerID)-1] == '\n' {
		containerID = containerID[:len(containerID)-1]
	}

	return containerID, nil
}

func (d *DockerClient) WaitContainer(ctx context.Context, containerID string) error {
	cmd := exec.CommandContext(ctx, "docker", "wait", containerID)
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("DockerClient.WaitContainer %q: %w", containerID, err)
	}

	exitCode := string(out)
	if len(exitCode) > 0 && exitCode[len(exitCode)-1] == '\n' {
		exitCode = exitCode[:len(exitCode)-1]
	}
	if exitCode != "0" {
		return fmt.Errorf("DockerClient.WaitContainer %q exited with %s", containerID, exitCode)
	}
	return nil
}

func (d *DockerClient) LogsContainer(ctx context.Context, containerID string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "docker", "logs", containerID)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("DockerClient.LogsContainer %q: %w", containerID, err)
	}
	return out, nil
}

func (d *DockerClient) StreamLogs(ctx context.Context, containerID string, pub func(string)) error {
	cmd := exec.CommandContext(ctx, "docker", "logs", "-f", containerID)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("DockerClient.StreamLogs stdout pipe: %w", err)
	}

	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("DockerClient.StreamLogs start: %w", err)
	}

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		pub(scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		d.logger.WarnContext(ctx, "docker logs scanner error", slog.String("error", err.Error()))
	}

	return cmd.Wait()
}
