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
	tempDir := t.TempDir()

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
	tempDir := t.TempDir()

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

func TestExecutor_PathTraversalPrevention(t *testing.T) {
	tempDir := t.TempDir()
	wsDir := filepath.Join(tempDir, "workspaces")
	exec := worker.NewExecutor(nil, nil, nil, toolchain.NewRegistry(), nil, tempDir)
	exec.SetWorkspaceDir(wsDir)

	ctx := context.Background()
	traversalJob := apitypes.Job{
		ID:          "job-traversal",
		ProjectID:   "../../outside",
		Runner:      apitypes.RunnerHost,
		SourceMode:  apitypes.SourceModeWorkspace,
		Owner:       "test-user",
		CommandArgs: []string{"echo", "escaped"},
	}

	_, err := exec.Execute(ctx, traversalJob)
	if err == nil {
		t.Fatalf("expected path traversal in ProjectID to be rejected, but got nil error")
	}
	if !strings.Contains(err.Error(), "workspace escape detected") {
		t.Errorf("expected workspace escape detected error, got: %v", err)
	}

	traversalOwnerJob := apitypes.Job{
		ID:          "job-traversal-owner",
		ProjectID:   "my-proj",
		Runner:      apitypes.RunnerHost,
		SourceMode:  apitypes.SourceModeWorkspace,
		Owner:       "../../../root",
		CommandArgs: []string{"echo", "escaped"},
	}

	_, err = exec.Execute(ctx, traversalOwnerJob)
	if err == nil {
		t.Fatalf("expected path traversal in Owner to be rejected, but got nil error")
	}
	if !strings.Contains(err.Error(), "workspace escape detected") {
		t.Errorf("expected workspace escape detected error, got: %v", err)
	}
}

func TestDocker_HardeningPolicy(t *testing.T) {
	if err := worker.ValidateDockerMount("/var/run/docker.sock"); err == nil {
		t.Errorf("expected mounting docker socket to be rejected")
	}
	if err := worker.ValidateDockerMount("/"); err == nil {
		t.Errorf("expected mounting root filesystem to be rejected")
	}
	if err := worker.ValidateDockerMount("/etc"); err == nil {
		t.Errorf("expected mounting /etc to be rejected")
	}

	opts := worker.RunOpts{
		Image:     "alpine:latest",
		MountPath: "/tmp/fake_workspace",
		Command:   []string{"echo", "hi"},
	}

	policy := worker.DefaultDockerSecurityPolicy()
	args, err := worker.BuildDockerArgs(opts, policy)
	if err != nil {
		t.Fatalf("BuildDockerArgs failed: %v", err)
	}

	argStr := strings.Join(args, " ")
	if !strings.Contains(argStr, "--cap-drop=ALL") {
		t.Errorf("expected args to contain --cap-drop=ALL, got: %s", argStr)
	}
	if !strings.Contains(argStr, "--security-opt=no-new-privileges:true") {
		t.Errorf("expected args to contain no-new-privileges, got: %s", argStr)
	}
	if !strings.Contains(argStr, "--network=none") {
		t.Errorf("expected args to contain --network=none, got: %s", argStr)
	}
	if !strings.Contains(argStr, "--pids-limit=512") {
		t.Errorf("expected args to contain --pids-limit=512, got: %s", argStr)
	}

	optsPriv := worker.RunOpts{
		Image:     "alpine:latest",
		MountPath: "/tmp/fake_workspace",
		Command:   []string{"--privileged", "sh"},
	}
	if _, err := worker.BuildDockerArgs(optsPriv, policy); err == nil {
		t.Errorf("expected privileged container request to be rejected")
	}
}

func TestHost_EnvironmentSanitization(t *testing.T) {
	dirtyEnv := []string{
		"PATH=/usr/bin:/bin",
		"HOME=/home/user",
		"PACKETS_AUTH_TOKEN=supersecret123",
		"GITHUB_TOKEN=ghp_fake12345",
		"AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		"DB_PASSWORD=secretpassword",
		"SAFE_FLAG=1",
	}

	cleanEnv := worker.SanitizeHostEnvironment(dirtyEnv)
	cleanStr := strings.Join(cleanEnv, "\n")

	if strings.Contains(cleanStr, "supersecret123") {
		t.Errorf("PACKETS_AUTH_TOKEN was not sanitized")
	}
	if strings.Contains(cleanStr, "ghp_fake12345") {
		t.Errorf("GITHUB_TOKEN was not sanitized")
	}
	if strings.Contains(cleanStr, "EXAMPLEKEY") {
		t.Errorf("AWS_SECRET_ACCESS_KEY was not sanitized")
	}
	if strings.Contains(cleanStr, "secretpassword") {
		t.Errorf("DB_PASSWORD was not sanitized")
	}
	if !strings.Contains(cleanStr, "PATH=/usr/bin:/bin") {
		t.Errorf("PATH was improperly removed")
	}
	if !strings.Contains(cleanStr, "SAFE_FLAG=1") {
		t.Errorf("SAFE_FLAG was improperly removed")
	}
}

func TestHost_SecurityPolicy(t *testing.T) {
	policy := worker.DefaultHostSecurityPolicy()

	validDir := t.TempDir()
	if err := policy.Validate(validDir, []string{"echo", "hello"}); err != nil {
		t.Fatalf("expected validDir to pass: %v", err)
	}

	if err := policy.Validate("/", []string{"echo", "test"}); err == nil {
		t.Errorf("expected root directory to be rejected")
	}

	if err := policy.Validate(validDir, []string{"rm", "-rf", "/"}); err == nil {
		t.Errorf("expected rm -rf / to be rejected by host policy")
	}

	if err := policy.Validate(validDir, []string{"mkfs.ext4", "/dev/sda"}); err == nil {
		t.Errorf("expected mkfs to be rejected by host policy")
	}

	policyWithRoot := worker.HostSecurityPolicy{
		AllowedRoots: []string{validDir},
	}
	otherDir := t.TempDir()
	if err := policyWithRoot.Validate(otherDir, []string{"echo", "test"}); err == nil {
		t.Errorf("expected directory outside AllowedRoots to be rejected")
	}

	exec := worker.NewExecutor(nil, nil, nil, toolchain.NewRegistry(), nil, validDir)
	exec.SetHostSecurityPolicy(policy)
	ctx := context.Background()

	res, err := exec.Execute(ctx, apitypes.Job{
		ID:          "job-blocked-cmd",
		ProjectID:   "proj-sec",
		Runner:      apitypes.RunnerHost,
		SourceMode:  apitypes.SourceModeWorkspace,
		CommandArgs: []string{"format", "C:"},
	})
	if err != nil {
		t.Fatalf("unexpected dispatch error: %v", err)
	}
	if res.ExitCode == 0 {
		t.Errorf("expected non-zero exit code for blocked command")
	}
}
