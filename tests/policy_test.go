package tests

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/debaucheryparty/packets/internal/policy"
	"github.com/debaucheryparty/packets/internal/storage"
)

func TestPolicyEngine_ClassifyCommand(t *testing.T) {
	engine := policy.NewPolicyEngine(policy.ApprovalAlways)

	tests := []struct {
		cmd  string
		want policy.CommandCategory
	}{
		{"ls -la", policy.CategoryReadOnly},
		{"git status", policy.CategoryReadOnly},
		{"./gradlew assembleDebug", policy.CategoryBuild},
		{"west build -b halo", policy.CategoryBuild},
		{"cargo test", policy.CategoryTest},
		{"./gradlew test", policy.CategoryTest},
		{"npm install", policy.CategoryDependency},
		{"rm -rf /", policy.CategoryDangerous},
	}

	for _, tt := range tests {
		got := engine.ClassifyCommand(tt.cmd)
		if got != tt.want {
			t.Errorf("ClassifyCommand(%q) = %s, want %s", tt.cmd, got, tt.want)
		}
	}
}

func TestPolicyEngine_ValidatePath(t *testing.T) {
	engine := policy.NewPolicyEngine(policy.ApprovalAlways)

	root := "/var/lib/packets/workspaces/app1"

	if err := engine.ValidatePath("/var/lib/packets/workspaces/app1/src/main.go", root); err != nil {
		t.Errorf("expected valid path inside root, got: %v", err)
	}

	if err := engine.ValidatePath("/etc/passwd", root); err == nil {
		t.Errorf("expected error accessing /etc/passwd")
	}

	if err := engine.ValidatePath("/var/lib/packets/workspaces/app2/secret", root); err == nil {
		t.Errorf("expected error accessing outside workspace")
	}
}

func TestPolicyEngine_ApprovalWorkflowAndBinding(t *testing.T) {
	engine := policy.NewPolicyEngine(policy.ApprovalAlways)

	baseCtx := policy.ExecutionContext{
		User:         "alice",
		ProjectID:    "proj-123",
		WorkspaceID:  "ws-456",
		SnapshotHash: "snap-aaa",
		Command:      "build",
		Args:         []string{"assembleDebug"},
		Action:       "BUILD",
	}

	if !engine.RequiresApprovalFor(baseCtx) {
		t.Fatalf("expected command to require approval")
	}

	req, err := engine.CreatePendingApproval(baseCtx)
	if err != nil {
		t.Fatalf("CreatePendingApproval: %v", err)
	}
	if req.ID == "" {
		t.Fatalf("expected non-empty request ID")
	}

	ticket, err := engine.ApprovePending(req.ID)
	if err != nil {
		t.Fatalf("ApprovePending: %v", err)
	}
	if ticket.ID == "" {
		t.Fatalf("expected ticket ID")
	}

	if err := engine.ValidateAndConsumeTicket(ticket.ID, baseCtx); err != nil {
		t.Fatalf("ValidateAndConsumeTicket with matching context failed: %v", err)
	}

	if err := engine.ValidateAndConsumeTicket(ticket.ID, baseCtx); err == nil {
		t.Errorf("expected already consumed ticket to fail")
	}

	req2, _ := engine.CreatePendingApproval(baseCtx)
	ticket2, _ := engine.ApprovePending(req2.ID)

	tamperedCtx := baseCtx
	tamperedCtx.SnapshotHash = "snap-tampered"
	if err := engine.ValidateAndConsumeTicket(ticket2.ID, tamperedCtx); err == nil {
		t.Errorf("expected ticket bound to snap-aaa to fail for snap-tampered")
	}

	req4, _ := engine.CreatePendingApproval(baseCtx)
	ticket4, _ := engine.ApprovePending(req4.ID)
	tamperedArgsCtx := baseCtx
	tamperedArgsCtx.Args = []string{"assembleDebug", "--evil"}
	if err := engine.ValidateAndConsumeTicket(ticket4.ID, tamperedArgsCtx); err == nil {
		t.Errorf("expected ticket bound to args to fail for tampered args")
	}

	req5, _ := engine.CreatePendingApproval(baseCtx)
	ticket5, _ := engine.ApprovePending(req5.ID)
	tamperedProjCtx := baseCtx
	tamperedProjCtx.ProjectID = "proj-other"
	if err := engine.ValidateAndConsumeTicket(ticket5.ID, tamperedProjCtx); err == nil {
		t.Errorf("expected ticket bound to project to fail for tampered project")
	}

	req6, _ := engine.CreatePendingApproval(baseCtx)
	ticket6, _ := engine.ApprovePending(req6.ID)
	tamperedWsCtx := baseCtx
	tamperedWsCtx.WorkspaceID = "ws-other"
	if err := engine.ValidateAndConsumeTicket(ticket6.ID, tamperedWsCtx); err == nil {
		t.Errorf("expected ticket bound to workspace to fail for tampered workspace")
	}

	if err := engine.ValidateAndConsumeTicket("forged_ticket_12345", baseCtx); err == nil {
		t.Errorf("expected forged ticket to fail validation")
	}
}

func TestPolicyEngine_ApprovalExpiry(t *testing.T) {
	engine := policy.NewPolicyEngine(policy.ApprovalAlways)
	ctx := policy.ExecutionContext{
		User:         "alice",
		ProjectID:    "proj-1",
		WorkspaceID:  "ws-1",
		SnapshotHash: "snap-1",
		Command:      "echo test",
		Args:         []string{"echo", "test"},
		Action:       "EXEC",
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

	if err := engine.ValidateAndConsumeTicket(ticket.ID, ctx); err == nil {
		t.Errorf("expected expired ticket to fail validation")
	}
}

func TestPolicyEngine_PersistentRestartWorkflow(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "approvals.db")
	ctx := context.Background()

	store1, err := storage.NewJobStore(ctx, dbPath)
	if err != nil {
		t.Fatalf("NewJobStore: %v", err)
	}

	engine1 := policy.NewPolicyEngineWithStore(policy.ApprovalAlways, store1)

	execCtx := policy.ExecutionContext{
		User:         "alice",
		ProjectID:    "proj-alpha",
		WorkspaceID:  "ws-beta",
		SnapshotHash: "snap-123",
		Command:      "make release",
		Args:         []string{"make", "release"},
		Action:       "BUILD",
	}

	req, err := engine1.CreatePendingApproval(execCtx)
	if err != nil {
		t.Fatalf("CreatePendingApproval: %v", err)
	}

	ticket, err := engine1.ApprovePending(req.ID)
	if err != nil {
		t.Fatalf("ApprovePending: %v", err)
	}

	_ = store1.Close()

	store2, err := storage.NewJobStore(ctx, dbPath)
	if err != nil {
		t.Fatalf("NewJobStore after restart: %v", err)
	}
	defer func() { _ = store2.Close() }()

	engine2 := policy.NewPolicyEngineWithStore(policy.ApprovalAlways, store2)

	tamperedCmdCtx := execCtx
	tamperedCmdCtx.Command = "make malicious"
	if err := engine2.ValidateAndConsumeTicket(ticket.ID, tamperedCmdCtx); err == nil {
		t.Errorf("expected modified command to fail validation after restart")
	}

	tamperedSnapCtx := execCtx
	tamperedSnapCtx.SnapshotHash = "snap-tampered"
	if err := engine2.ValidateAndConsumeTicket(ticket.ID, tamperedSnapCtx); err == nil {
		t.Errorf("expected modified snapshot to fail validation after restart")
	}

	if err := engine2.ValidateAndConsumeTicket(ticket.ID, execCtx); err != nil {
		t.Fatalf("expected exact approved request to succeed after restart: %v", err)
	}

	if err := engine2.ValidateAndConsumeTicket(ticket.ID, execCtx); err == nil {
		t.Errorf("expected ticket to be single-use and fail on replay")
	}
}

func TestPolicyEngine_PersistentConcurrentConsumption(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "concurrent_approvals.db")
	ctx := context.Background()

	store, err := storage.NewJobStore(ctx, dbPath)
	if err != nil {
		t.Fatalf("NewJobStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	engine := policy.NewPolicyEngineWithStore(policy.ApprovalAlways, store)

	execCtx := policy.ExecutionContext{
		User:         "bob",
		ProjectID:    "proj-concurrent",
		WorkspaceID:  "ws-concurrent",
		SnapshotHash: "snap-456",
		Command:      "cargo build",
		Args:         []string{"cargo", "build"},
		Action:       "BUILD",
	}

	req, err := engine.CreatePendingApproval(execCtx)
	if err != nil {
		t.Fatalf("CreatePendingApproval: %v", err)
	}

	ticket, err := engine.ApprovePending(req.ID)
	if err != nil {
		t.Fatalf("ApprovePending: %v", err)
	}

	concurrency := 10
	var wg sync.WaitGroup
	wg.Add(concurrency)
	var successes int32

	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			if err := engine.ValidateAndConsumeTicket(ticket.ID, execCtx); err == nil {
				atomic.AddInt32(&successes, 1)
			}
		}()
	}

	wg.Wait()

	if successes != 1 {
		t.Errorf("expected exactly 1 successful consumption, got %d", successes)
	}
}
