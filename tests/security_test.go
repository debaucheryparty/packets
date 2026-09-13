package tests

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/debaucheryparty/packets/internal/policy"
	"github.com/debaucheryparty/packets/internal/project"
	"github.com/debaucheryparty/packets/internal/scheduler"
	"github.com/debaucheryparty/packets/internal/storage"
	"github.com/debaucheryparty/packets/internal/worker"
	"github.com/debaucheryparty/packets/pkg/apitypes"
	pb "github.com/debaucheryparty/packets/proto/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestSecurity_PathTraversalAndWorkspaceEscape(t *testing.T) {
	engine := policy.NewPolicyEngine(policy.ApprovalAlways)
	root := "/var/lib/packets/workspaces/app1"

	if err := engine.ValidatePath("/var/lib/packets/workspaces/app1/safe.go", root); err != nil {
		t.Errorf("expected safe path to be valid: %v", err)
	}

	escapes := []string{
		"/var/lib/packets/workspaces/app2/secret",
		"/etc/passwd",
		"/root/.ssh/id_rsa",
		"../../etc/shadow",
	}
	for _, p := range escapes {
		if err := engine.ValidatePath(p, root); err == nil {
			t.Errorf("expected path escape %q to be rejected", p)
		}
	}
}

func TestSecurity_ApprovalTamperingAndReplay(t *testing.T) {
	engine := policy.NewPolicyEngine(policy.ApprovalAlways)
	ctx := policy.ExecutionContext{
		User:         "alice",
		ProjectID:    "p1",
		WorkspaceID:  "w1",
		SnapshotHash: "s1",
		Command:      "make",
		Args:         []string{"make"},
		Action:       "BUILD",
	}

	req, err := engine.CreatePendingApproval(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := engine.ApprovePending(req.ID)
	if err != nil {
		t.Fatal(err)
	}

	if err := engine.ValidateAndConsumeTicket("forged_ticket", ctx); !errors.Is(err, policy.ErrInvalidApprovalTicket) {
		t.Errorf("expected ErrInvalidApprovalTicket, got: %v", err)
	}

	ctxChangedCmd := ctx
	ctxChangedCmd.Command = "make evil"
	if err := engine.ValidateAndConsumeTicket(ticket.ID, ctxChangedCmd); !errors.Is(err, policy.ErrApprovalMismatch) {
		t.Errorf("expected ErrApprovalMismatch for changed command, got: %v", err)
	}

	ctxChangedArgs := ctx
	ctxChangedArgs.Args = []string{"make", "evil"}
	if err := engine.ValidateAndConsumeTicket(ticket.ID, ctxChangedArgs); !errors.Is(err, policy.ErrApprovalMismatch) {
		t.Errorf("expected ErrApprovalMismatch for changed args, got: %v", err)
	}

	ctxChangedSnap := ctx
	ctxChangedSnap.SnapshotHash = "s2"
	if err := engine.ValidateAndConsumeTicket(ticket.ID, ctxChangedSnap); !errors.Is(err, policy.ErrApprovalMismatch) {
		t.Errorf("expected ErrApprovalMismatch for changed snapshot, got: %v", err)
	}

	ctxChangedProj := ctx
	ctxChangedProj.ProjectID = "p2"
	if err := engine.ValidateAndConsumeTicket(ticket.ID, ctxChangedProj); !errors.Is(err, policy.ErrApprovalMismatch) {
		t.Errorf("expected ErrApprovalMismatch for changed project, got: %v", err)
	}

	ctxChangedWs := ctx
	ctxChangedWs.WorkspaceID = "w2"
	if err := engine.ValidateAndConsumeTicket(ticket.ID, ctxChangedWs); !errors.Is(err, policy.ErrApprovalMismatch) {
		t.Errorf("expected ErrApprovalMismatch for changed workspace, got: %v", err)
	}

	if err := engine.ValidateAndConsumeTicket(ticket.ID, ctx); err != nil {
		t.Fatalf("expected exact approved context to succeed: %v", err)
	}

	if err := engine.ValidateAndConsumeTicket(ticket.ID, ctx); !errors.Is(err, policy.ErrTicketAlreadyUsed) {
		t.Errorf("expected ErrTicketAlreadyUsed on replay, got: %v", err)
	}
}

func TestSecurity_ApprovalExpiry(t *testing.T) {
	engine := policy.NewPolicyEngine(policy.ApprovalAlways)
	ctx := policy.ExecutionContext{
		User:         "bob",
		ProjectID:    "p1",
		WorkspaceID:  "w1",
		SnapshotHash: "s1",
		Command:      "clean",
		Args:         []string{"clean"},
		Action:       "BUILD",
	}

	req, err := engine.CreatePendingApproval(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := engine.ApprovePending(req.ID)
	if err != nil {
		t.Fatal(err)
	}

	engine.ExpireTicketForTest(ticket.ID)
	if err := engine.ValidateAndConsumeTicket(ticket.ID, ctx); !errors.Is(err, policy.ErrTicketExpired) {
		t.Errorf("expected ErrTicketExpired, got: %v", err)
	}
}

func TestSecurity_DirectGRPCBypassRejection(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}
	defer func() { _ = lis.Close() }()

	tmpDir := t.TempDir()
	store, err := storage.NewJobStore(context.Background(), filepath.Join(tmpDir, "sec.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	logBroker := scheduler.NewLogBroker()
	dispatcher := scheduler.NewDispatcher(nil, store, nil, nil, nil, logBroker)

	grpcServer := grpc.NewServer()
	srv := scheduler.NewServer(dispatcher, store, logBroker, nil, nil)
	srv.SetPolicyEngine(policy.NewPolicyEngine(policy.ApprovalAlways))
	pb.RegisterSchedulerServer(grpcServer, srv)

	go func() {
		_ = grpcServer.Serve(lis)
	}()
	defer grpcServer.Stop()

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()

	client := pb.NewSchedulerClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = client.SubmitJob(ctx, &pb.SubmitJobRequest{
		CacheKey:    "bypass-key",
		Toolchain:   "custom",
		Runner:      string(apitypes.RunnerHost),
		SourceMode:  string(apitypes.SourceModeWorkspace),
		CommandArgs: []string{"rm -rf /"},
		ProjectId:   "proj-sec",
	})
	if err == nil {
		t.Fatalf("expected unapproved dangerous command to be rejected by scheduler policy")
	}
}

func TestSecurity_DockerHardeningRegression(t *testing.T) {
	if err := worker.ValidateDockerMount("/var/run/docker.sock"); err == nil {
		t.Errorf("expected docker socket mount to be forbidden")
	}
	if err := worker.ValidateDockerMount("/"); err == nil {
		t.Errorf("expected root mount to be forbidden")
	}

	opts := worker.RunOpts{
		Image:     "alpine:latest",
		MountPath: "/tmp/safe_mount",
		Command:   []string{"--privileged", "sh"},
	}
	policy := worker.DefaultDockerSecurityPolicy()
	if _, err := worker.BuildDockerArgs(opts, policy); err == nil {
		t.Errorf("expected --privileged injection to be rejected")
	}
}

func TestSecurity_SecretRedactionRegression(t *testing.T) {
	env := []string{
		"PACKETS_AUTH_TOKEN=supersecret",
		"AWS_SECRET_ACCESS_KEY=myawskey",
		"GITHUB_TOKEN=ghp_token123",
		"API_KEY=apikey999",
		"PATH=/bin:/usr/bin",
	}
	sanitized := worker.SanitizeHostEnvironment(env)
	joined := strings.Join(sanitized, " ")

	if strings.Contains(joined, "supersecret") || strings.Contains(joined, "myawskey") || strings.Contains(joined, "ghp_token123") || strings.Contains(joined, "apikey999") {
		t.Errorf("secret leaked in host environment: %s", joined)
	}
	if !strings.Contains(joined, "PATH=/bin:/usr/bin") {
		t.Errorf("PATH was improperly removed")
	}
}

func TestSecurity_BuildGraph_ArtifactPathValidation(t *testing.T) {
	cfg := &project.Config{
		ProjectID: "art-escape",
		Components: []project.ComponentConfig{
			{
				Name: "c1",
				Type: "go",
				Path: "src",
				ArtifactRouting: map[string]string{
					"out.bin": "../../etc/evil.bin",
				},
			},
		},
	}
	g, err := project.NewBuildGraph(cfg)
	if err != nil {
		t.Fatalf("NewBuildGraph failed: %v", err)
	}

	tmpDir := t.TempDir()
	srcDir := filepath.Join(tmpDir, "src")
	_ = os.MkdirAll(srcDir, 0o755)
	_ = os.WriteFile(filepath.Join(srcDir, "out.bin"), []byte("data"), 0o644)

	_ = g.RouteArtifacts(tmpDir, cfg.Components[0])
	escapedFile := filepath.Clean(filepath.Join(tmpDir, "../../etc/evil.bin"))
	_ = os.Remove(escapedFile)
}

func TestSecurity_HostSecurityPolicy_CommandAndPathRestrictions(t *testing.T) {
	pol := worker.DefaultHostSecurityPolicy()
	tmp := t.TempDir()

	dangerous := [][]string{
		{"rm", "-rf", "/"},
		{"mkfs.ext4", "/dev/nvme0n1"},
		{"dd", "if=/dev/zero", "of=/dev/sda"},
		{"shutdown", "-h", "now"},
		{"reboot"},
	}
	for _, cmd := range dangerous {
		if err := pol.Validate(tmp, cmd); err == nil {
			t.Errorf("expected dangerous command %v to be blocked", cmd)
		}
	}

	pol.AllowedRoots = []string{tmp}
	if err := pol.Validate(filepath.Dir(tmp), []string{"echo", "hi"}); err == nil {
		t.Errorf("expected parent directory outside AllowedRoots to be blocked")
	}
}
