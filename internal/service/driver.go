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

type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *safeBuffer) Write(p []byte) (n int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

func (s *safeBuffer) logf(format string, args ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fmt.Fprintf(&s.buf, format, args...)
}

type ProcessDriver struct {
	mu        sync.RWMutex
	processes map[string]*processInstance
}

type processInstance struct {
	cmd    *exec.Cmd
	logs   *safeBuffer
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
	buf := new(safeBuffer)

	var cmdArgs []string
	if len(svc.Command) > 0 {
		cmdArgs = svc.Command
	} else if svc.Image != "" {
		cmdArgs = strings.Fields(svc.Image)
	}

	if len(cmdArgs) == 0 {
		buf.logf("[%s] Service %s started\n", time.Now().UTC().Format(time.RFC3339), svc.Name)
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
		buf.logf("[%s] Start error: %v\n", time.Now().UTC().Format(time.RFC3339), err)
		d.processes[instanceID] = &processInstance{
			logs: buf,
		}
		return instanceID, nil
	}

	buf.logf("[%s] Process started with PID %d\n", time.Now().UTC().Format(time.RFC3339), cmd.Process.Pid)

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

	if proc.logs != nil {
		proc.logs.logf("[%s] Process stopped\n", time.Now().UTC().Format(time.RFC3339))
	}

	return nil
}

func (d *ProcessDriver) Logs(ctx context.Context, containerID string, lines int) (string, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	proc, ok := d.processes[containerID]
	if !ok {
		return "", nil
	}

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
