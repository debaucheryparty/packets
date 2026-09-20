package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/debaucheryparty/packets/pkg/apitypes"
)

var (
	ErrDriverNotFound    = errors.New("driver not found")
	ErrServiceNotRunning = errors.New("service process is not running")
)

type Driver interface {
	Start(ctx context.Context, svc apitypes.Service) (string, error)
	Stop(ctx context.Context, containerID string) error
	Logs(ctx context.Context, containerID string, lines int) (string, error)
}

type ProcessDriver struct {
	mu        sync.RWMutex
	processes map[string]*processInstance
}

type processInstance struct {
	cmd    *exec.Cmd
	logs   *bytes.Buffer
	mu     sync.Mutex
	cancel context.CancelFunc
}

func NewProcessDriver() *ProcessDriver {
	return &ProcessDriver{
		processes: make(map[string]*processInstance),
	}
}

func (d *ProcessDriver) Start(ctx context.Context, svc apitypes.Service) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	instanceID := fmt.Sprintf("proc-%s-%d", svc.Name, time.Now().UnixNano())
	buf := new(bytes.Buffer)

	var cmdArgs []string
	if len(svc.Command) > 0 {
		cmdArgs = svc.Command
	} else if svc.Image != "" {
		cmdArgs = strings.Fields(svc.Image)
	}

	if len(cmdArgs) == 0 {
		fmt.Fprintf(buf, "[%s] Service %s started\n", time.Now().UTC().Format(time.RFC3339), svc.Name)
		d.processes[instanceID] = &processInstance{
			logs: buf,
		}
		return instanceID, nil
	}

	execCtx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(execCtx, cmdArgs[0], cmdArgs[1:]...)
	cmd.Stdout = buf
	cmd.Stderr = buf

	if len(svc.Environment) > 0 {
		env := cmd.Environ()
		for k, v := range svc.Environment {
			env = append(env, fmt.Sprintf("%s=%s", k, v))
		}
		cmd.Env = env
	}

	if err := cmd.Start(); err != nil {
		cancel()
		fmt.Fprintf(buf, "[%s] Start error: %v\n", time.Now().UTC().Format(time.RFC3339), err)
		d.processes[instanceID] = &processInstance{
			logs: buf,
		}
		return instanceID, nil
	}

	fmt.Fprintf(buf, "[%s] Process started with PID %d\n", time.Now().UTC().Format(time.RFC3339), cmd.Process.Pid)

	d.processes[instanceID] = &processInstance{
		cmd:    cmd,
		logs:   buf,
		cancel: cancel,
	}

	go func() {
		_ = cmd.Wait()
	}()

	return instanceID, nil
}

func (d *ProcessDriver) Stop(ctx context.Context, containerID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	proc, ok := d.processes[containerID]
	if !ok {
		return nil
	}

	if proc.cancel != nil {
		proc.cancel()
	}
	if proc.cmd != nil && proc.cmd.Process != nil {
		_ = proc.cmd.Process.Kill()
	}

	proc.mu.Lock()
	if proc.logs != nil {
		fmt.Fprintf(proc.logs, "[%s] Process stopped\n", time.Now().UTC().Format(time.RFC3339))
	}
	proc.mu.Unlock()

	return nil
}

func (d *ProcessDriver) Logs(ctx context.Context, containerID string, lines int) (string, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	proc, ok := d.processes[containerID]
	if !ok {
		return "", nil
	}

	proc.mu.Lock()
	defer proc.mu.Unlock()

	if proc.logs == nil {
		return "", nil
	}

	all := proc.logs.String()
	if lines <= 0 {
		return all, nil
	}

	allLines := strings.Split(strings.TrimRight(all, "\n"), "\n")
	if len(allLines) <= lines {
		return all, nil
	}

	return strings.Join(allLines[len(allLines)-lines:], "\n") + "\n", nil
}
