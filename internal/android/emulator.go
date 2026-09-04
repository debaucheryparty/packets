package android

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"
)

type EmulatorState string

const (
	EmulatorStateStopped  EmulatorState = "stopped"
	EmulatorStateStarting EmulatorState = "starting"
	EmulatorStateBooting  EmulatorState = "booting"
	EmulatorStateReady    EmulatorState = "ready"
	EmulatorStateStopping EmulatorState = "stopping"
)

type EmulatorManager interface {
	Start(ctx context.Context, avd string, opts EmulatorStartOpts) (string, error) // returns serial
	Stop(ctx context.Context, serial string) error
	Status(ctx context.Context, serial string) (EmulatorState, error)
	WaitForBoot(ctx context.Context, serial string, timeout time.Duration) error
	List(ctx context.Context) ([]EmulatorInfo, error)
	SaveSnapshot(ctx context.Context, serial, name string) error
	LoadSnapshot(ctx context.Context, serial, name string) error
	ListSnapshots(ctx context.Context, serial string) ([]string, error)
	DeleteSnapshot(ctx context.Context, serial, name string) error
}

type EmulatorStartOpts struct {
	Cores          int
	RAMMb          int
	GPU            string
	NoAudio        bool
	NoWindow       bool
	Snapshot       string
	NoSnapshotLoad bool
}

type EmulatorInfo struct {
	Serial string
	AVD    string
	State  EmulatorState
	PID    int
}

type AVDEmulatorManager struct {
	logger *slog.Logger
	adb    ADBClient
}

func NewAVDEmulatorManager(logger *slog.Logger, adb ADBClient) *AVDEmulatorManager {
	return &AVDEmulatorManager{logger: logger, adb: adb}
}

func (m *AVDEmulatorManager) Start(ctx context.Context, avd string, opts EmulatorStartOpts) (string, error) {
	args := []string{"-avd", avd, "-no-boot-anim"}

	if opts.NoWindow {
		args = append(args, "-no-window")
	}
	if opts.NoAudio {
		args = append(args, "-no-audio")
	}
	if opts.GPU != "" {
		args = append(args, "-gpu", opts.GPU)
	} else {
		args = append(args, "-gpu", "auto")
	}
	if opts.Cores > 0 {
		args = append(args, "-cores", fmt.Sprintf("%d", opts.Cores))
	}
	if opts.RAMMb > 0 {
		args = append(args, "-memory", fmt.Sprintf("%d", opts.RAMMb))
	}
	if opts.Snapshot != "" {
		args = append(args, "-snapshot", opts.Snapshot)
	}
	if opts.NoSnapshotLoad {
		args = append(args, "-no-snapshot-load")
	}

	cmd := exec.Command("emulator", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("emulator start %s: %w", avd, err)
	}

	m.logger.InfoContext(ctx, "emulator process started", slog.String("avd", avd), slog.Int("pid", cmd.Process.Pid))

	serial, err := m.waitForDevice(ctx, 30*time.Second)
	if err != nil {
		return "", fmt.Errorf("emulator %s: device not found after start: %w", avd, err)
	}

	return serial, nil
}

func (m *AVDEmulatorManager) waitForDevice(ctx context.Context, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		devices, err := m.adb.Devices(ctx)
		if err == nil {
			for _, d := range devices {
				if strings.HasPrefix(d.Serial, "emulator-") && d.State == "device" {
					return d.Serial, nil
				}
			}
		}
		time.Sleep(2 * time.Second)
	}
	return "", fmt.Errorf("timeout waiting for emulator device")
}

func (m *AVDEmulatorManager) WaitForBoot(ctx context.Context, serial string, timeout time.Duration) error {
	m.logger.InfoContext(ctx, "waiting for Android boot", slog.String("serial", serial))
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		out, err := m.adb.Shell(ctx, serial, "getprop", "sys.boot_completed")
		if err == nil && strings.TrimSpace(out) == "1" {
			m.logger.InfoContext(ctx, "Android boot complete", slog.String("serial", serial))
			return nil
		}
		time.Sleep(3 * time.Second)
	}
	return fmt.Errorf("timeout waiting for Android boot on %s", serial)
}

func (m *AVDEmulatorManager) Stop(ctx context.Context, serial string) error {
	out, err := exec.CommandContext(ctx, "adb", "-s", serial, "emu", "kill").CombinedOutput()
	if err != nil {
		return fmt.Errorf("emulator stop %s: %w\n%s", serial, err, string(out))
	}
	return nil
}

func (m *AVDEmulatorManager) Status(ctx context.Context, serial string) (EmulatorState, error) {
	devices, err := m.adb.Devices(ctx)
	if err != nil {
		return EmulatorStateStopped, err
	}
	for _, d := range devices {
		if d.Serial == serial {
			switch d.State {
			case "device":
				out, err := m.adb.Shell(ctx, serial, "getprop", "sys.boot_completed")
				if err == nil && strings.TrimSpace(out) == "1" {
					return EmulatorStateReady, nil
				}
				return EmulatorStateBooting, nil
			case "offline":
				return EmulatorStateStarting, nil
			}
		}
	}
	return EmulatorStateStopped, nil
}

func (m *AVDEmulatorManager) List(ctx context.Context) ([]EmulatorInfo, error) {
	devices, err := m.adb.Devices(ctx)
	if err != nil {
		return nil, err
	}
	var infos []EmulatorInfo
	for _, d := range devices {
		if strings.HasPrefix(d.Serial, "emulator-") {
			state := EmulatorStateStarting
			if d.State == "device" {
				state = EmulatorStateReady
			}
			infos = append(infos, EmulatorInfo{Serial: d.Serial, State: state})
		}
	}
	return infos, nil
}

func (m *AVDEmulatorManager) SaveSnapshot(ctx context.Context, serial, name string) error {
	out, err := exec.CommandContext(ctx, "adb", "-s", serial, "emu", "avd", "snapshot", "save", name).CombinedOutput()
	if err != nil {
		return fmt.Errorf("save snapshot %s: %w\n%s", name, err, string(out))
	}
	return nil
}

func (m *AVDEmulatorManager) LoadSnapshot(ctx context.Context, serial, name string) error {
	out, err := exec.CommandContext(ctx, "adb", "-s", serial, "emu", "avd", "snapshot", "load", name).CombinedOutput()
	if err != nil {
		return fmt.Errorf("load snapshot %s: %w\n%s", name, err, string(out))
	}
	return nil
}

func (m *AVDEmulatorManager) ListSnapshots(ctx context.Context, serial string) ([]string, error) {
	out, err := exec.CommandContext(ctx, "adb", "-s", serial, "emu", "avd", "snapshot", "list").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("list snapshots: %w\n%s", err, string(out))
	}
	var snapshots []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "OK") && !strings.HasPrefix(line, "List of snapshots:") {
			parts := strings.Fields(line)
			if len(parts) > 0 {
				snapshots = append(snapshots, parts[0])
			}
		}
	}
	return snapshots, nil
}

func (m *AVDEmulatorManager) DeleteSnapshot(ctx context.Context, serial, name string) error {
	out, err := exec.CommandContext(ctx, "adb", "-s", serial, "emu", "avd", "snapshot", "delete", name).CombinedOutput()
	if err != nil {
		return fmt.Errorf("delete snapshot %s: %w\n%s", name, err, string(out))
	}
	return nil
}
