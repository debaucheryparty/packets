package android

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

type ADBClient interface {
	Devices(ctx context.Context) ([]Device, error)
	Install(ctx context.Context, serial, apkPath string) error
	Shell(ctx context.Context, serial string, args ...string) (string, error)
	ShellStream(ctx context.Context, serial string, stdout io.Writer, args ...string) error
	Logcat(ctx context.Context, serial string, stdout io.Writer) error
	Forward(ctx context.Context, serial, localSpec, remoteSpec string) error
}

type Device struct {
	Serial string
	State  string
}

type ExecADBClient struct {
	adbPath string
}

func NewExecADBClient() *ExecADBClient {
	return &ExecADBClient{adbPath: "adb"}
}

func (a *ExecADBClient) adb(ctx context.Context, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, a.adbPath, args...)
}

func (a *ExecADBClient) Devices(ctx context.Context) ([]Device, error) {
	out, err := a.adb(ctx, "devices").Output()
	if err != nil {
		return nil, fmt.Errorf("adb devices: %w", err)
	}

	var devices []Device
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "List of devices") || line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		devices = append(devices, Device{Serial: parts[0], State: parts[1]})
	}
	return devices, nil
}

func (a *ExecADBClient) Install(ctx context.Context, serial, apkPath string) error {
	args := []string{}
	if serial != "" {
		args = append(args, "-s", serial)
	}
	args = append(args, "install", "-r", apkPath)

	out, err := a.adb(ctx, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("adb install %s: %w\n%s", apkPath, err, string(out))
	}
	return nil
}

func (a *ExecADBClient) Shell(ctx context.Context, serial string, args ...string) (string, error) {
	cmdArgs := []string{}
	if serial != "" {
		cmdArgs = append(cmdArgs, "-s", serial)
	}
	cmdArgs = append(cmdArgs, "shell")
	cmdArgs = append(cmdArgs, args...)

	out, err := a.adb(ctx, cmdArgs...).CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("adb shell: %w", err)
	}
	return string(out), nil
}

func (a *ExecADBClient) ShellStream(ctx context.Context, serial string, stdout io.Writer, args ...string) error {
	cmdArgs := []string{}
	if serial != "" {
		cmdArgs = append(cmdArgs, "-s", serial)
	}
	cmdArgs = append(cmdArgs, "shell")
	cmdArgs = append(cmdArgs, args...)

	cmd := a.adb(ctx, cmdArgs...)
	cmd.Stdout = stdout
	cmd.Stderr = stdout
	return cmd.Run()
}

func (a *ExecADBClient) Logcat(ctx context.Context, serial string, stdout io.Writer) error {
	args := []string{}
	if serial != "" {
		args = append(args, "-s", serial)
	}
	args = append(args, "logcat")

	cmd := a.adb(ctx, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stdout
	return cmd.Run()
}

func (a *ExecADBClient) Forward(ctx context.Context, serial, localSpec, remoteSpec string) error {
	args := []string{}
	if serial != "" {
		args = append(args, "-s", serial)
	}
	args = append(args, "forward", localSpec, remoteSpec)

	out, err := a.adb(ctx, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("adb forward: %w\n%s", err, string(out))
	}
	return nil
}
