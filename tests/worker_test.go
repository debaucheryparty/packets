package tests

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/debaucheryparty/packets/internal/toolchain"
	"github.com/debaucheryparty/packets/internal/worker"
	"github.com/debaucheryparty/packets/pkg/apitypes"
)

type workerTestPublisher struct {
	lines []string
}

func (p *workerTestPublisher) Publish(jobID apitypes.JobID, line string) {
	p.lines = append(p.lines, line)
}

func TestExecutor_HostExecution(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "packets-worker-test-*")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	wsDir := filepath.Join(tempDir, "workspaces")
	pub := &workerTestPublisher{}
	exec := worker.NewExecutor(nil, nil, nil, toolchain.NewRegistry(), pub, tempDir)
	exec.SetWorkspaceDir(wsDir)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := "echo hello-packets"
	job := apitypes.Job{
		ID:          "job-test-1",
		ProjectID:   "my-app",
		Runner:      apitypes.RunnerHost,
		SourceMode:  apitypes.SourceModeWorkspace,
		Owner:       "test-user",
		CommandArgs: []string{cmd},
	}

	result, err := exec.Execute(ctx, job)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d (stderr: %s)", result.ExitCode, result.Stderr)
	}

	output := strings.TrimSpace(result.Stdout)
	if !strings.Contains(output, "hello-packets") {
		t.Errorf("expected output to contain 'hello-packets', got %q", output)
	}

	expectedWorkspacePath := filepath.Join(wsDir, "test-user", "my-app")
	if fi, err := os.Stat(expectedWorkspacePath); err != nil || !fi.IsDir() {
		t.Errorf("expected persistent workspace directory to exist at %s", expectedWorkspacePath)
	}

	testFileInWs := filepath.Join(expectedWorkspacePath, "persisted_file.txt")
	if err := os.WriteFile(testFileInWs, []byte("stored-data"), 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	readCmd := "cat persisted_file.txt"
	if runtime.GOOS == "windows" {
		readCmd = "type persisted_file.txt"
	}

	job2 := apitypes.Job{
		ID:          "job-test-2",
		ProjectID:   "my-app",
		Runner:      apitypes.RunnerHost,
		SourceMode:  apitypes.SourceModeWorkspace,
		Owner:       "test-user",
		CommandArgs: []string{readCmd},
	}

	result2, err := exec.Execute(ctx, job2)
	if err != nil {
		t.Fatalf("Execute 2 failed: %v", err)
	}
	if result2.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d (stderr: %s)", result2.ExitCode, result2.Stderr)
	}
	if !strings.Contains(result2.Stdout, "stored-data") {
		t.Errorf("expected second execution to find persisted file, got: %q", result2.Stdout)
	}

	jobProjB := apitypes.Job{
		ID:          "job-test-proj-b",
		ProjectID:   "other-project",
		Runner:      apitypes.RunnerHost,
		SourceMode:  apitypes.SourceModeWorkspace,
		Owner:       "test-user",
		CommandArgs: []string{"echo hello-proj-b"},
	}
	_, err = exec.Execute(ctx, jobProjB)
	if err != nil {
		t.Fatalf("Execute proj B failed: %v", err)
	}

	projBPath := filepath.Join(wsDir, "test-user", "other-project")
	if fi, err := os.Stat(projBPath); err != nil || !fi.IsDir() {
		t.Errorf("expected project B workspace to exist at %s", projBPath)
	}
	if _, err := os.Stat(filepath.Join(projBPath, "persisted_file.txt")); !os.IsNotExist(err) {
		t.Errorf("project B should not see files from project A")
	}
}

func TestExecutor_CancellationKillsProcess(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "packets-cancel-test-*")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	wsDir := filepath.Join(tempDir, "workspaces")
	pub := &workerTestPublisher{}
	exec := worker.NewExecutor(nil, nil, nil, toolchain.NewRegistry(), pub, tempDir)
	exec.SetWorkspaceDir(wsDir)

	ctx, cancel := context.WithCancel(context.Background())

	cmd := "sleep 10"
	if runtime.GOOS == "windows" {
		cmd = "powershell -Command Start-Sleep -Seconds 10"
	}

	job := apitypes.Job{
		ID:          "job-cancel-test",
		ProjectID:   "cancel-proj",
		Runner:      apitypes.RunnerHost,
		SourceMode:  apitypes.SourceModeWorkspace,
		Owner:       "test-user",
		CommandArgs: []string{cmd},
	}

	start := time.Now()
	go func() {
		time.Sleep(300 * time.Millisecond)
		cancel()
	}()

	res, _ := exec.Execute(ctx, job)
	duration := time.Since(start)

	if duration > 5*time.Second {
		t.Errorf("process was not cancelled quickly, took %v", duration)
	}
	if res.ExitCode == 0 {
		t.Errorf("expected non-zero exit code on cancellation, got %d", res.ExitCode)
	}
}
