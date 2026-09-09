package policy

import (
	"testing"
	"time"
)

func TestPolicyEngine_ClassifyCommand(t *testing.T) {
	engine := NewPolicyEngine(ApprovalAlways)

	tests := []struct {
		cmd  string
		want CommandCategory
	}{
		{"ls -la", CategoryReadOnly},
		{"git status", CategoryReadOnly},
		{"./gradlew assembleDebug", CategoryBuild},
		{"west build -b halo", CategoryBuild},
		{"cargo test", CategoryTest},
		{"./gradlew test", CategoryTest},
		{"npm install", CategoryDependency},
		{"rm -rf /", CategoryDangerous},
	}

	for _, tt := range tests {
		got := engine.ClassifyCommand(tt.cmd)
		if got != tt.want {
			t.Errorf("ClassifyCommand(%q) = %s, want %s", tt.cmd, got, tt.want)
		}
	}
}

func TestPolicyEngine_ValidatePath(t *testing.T) {
	engine := NewPolicyEngine(ApprovalAlways)

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
	engine := NewPolicyEngine(ApprovalAlways)

	baseCtx := ExecutionContext{
		User:         "alice",
		ProjectID:    "proj-123",
		WorkspaceID:  "ws-456",
		SnapshotHash: "snap-aaa",
		Command:      "build",
		Args:         []string{"assembleDebug"},
		Action:       "BUILD",
	}

	// 1. No approval ticket provided -> reject
	if err := engine.ValidateAndConsumeTicket("", baseCtx); err != ErrApprovalRequired {
		t.Errorf("case 1: expected ErrApprovalRequired, got %v", err)
	}

	// 2. Fake approval ticket provided -> reject
	if err := engine.ValidateAndConsumeTicket("ticket_fake_123", baseCtx); err != ErrInvalidApprovalTicket {
		t.Errorf("case 2: expected ErrInvalidApprovalTicket, got %v", err)
	}

	// 3. Human creates pending approval and approves it
	pending, err := engine.CreatePendingApproval(baseCtx)
	if err != nil {
		t.Fatalf("CreatePendingApproval: %v", err)
	}
	if pending.ID == "" {
		t.Fatal("expected non-empty pending ID")
	}

	ticket, err := engine.ApprovePending(pending.ID)
	if err != nil {
		t.Fatalf("ApprovePending: %v", err)
	}

	// 5. Changed snapshot -> reject
	mutatedSnapshotCtx := baseCtx
	mutatedSnapshotCtx.SnapshotHash = "snap-bbb"
	if err := engine.ValidateAndConsumeTicket(ticket.ID, mutatedSnapshotCtx); err != ErrApprovalMismatch {
		t.Errorf("case 5: expected ErrApprovalMismatch for changed snapshot, got %v", err)
	}

	// 6. Changed command -> reject
	mutatedCmdCtx := baseCtx
	mutatedCmdCtx.Command = "rm -rf"
	if err := engine.ValidateAndConsumeTicket(ticket.ID, mutatedCmdCtx); err != ErrApprovalMismatch {
		t.Errorf("case 6: expected ErrApprovalMismatch for changed command, got %v", err)
	}

	// 7. Changed arguments -> reject (e.g. assembleDebug approved, but assembleRelease requested)
	mutatedArgsCtx := baseCtx
	mutatedArgsCtx.Args = []string{"assembleRelease"}
	if err := engine.ValidateAndConsumeTicket(ticket.ID, mutatedArgsCtx); err != ErrApprovalMismatch {
		t.Errorf("case 7: expected ErrApprovalMismatch for changed args, got %v", err)
	}

	// 4. Valid approval ticket with exact matching state -> execute allowed!
	if err := engine.ValidateAndConsumeTicket(ticket.ID, baseCtx); err != nil {
		t.Errorf("case 4: expected valid approval to execute, got %v", err)
	}

	// 8. Reused ticket -> reject (single-use)
	if err := engine.ValidateAndConsumeTicket(ticket.ID, baseCtx); err != ErrTicketAlreadyUsed {
		t.Errorf("case 8: expected ErrTicketAlreadyUsed, got %v", err)
	}

	// 9. Expired ticket -> reject
	pending2, _ := engine.CreatePendingApproval(baseCtx)
	ticket2, _ := engine.ApprovePending(pending2.ID)
	// Artificially expire ticket2
	engine.mu.Lock()
	engine.tickets[ticket2.ID].ExpiresAt = engine.tickets[ticket2.ID].CreatedAt.Add(-1 * time.Hour)
	engine.mu.Unlock()

	if err := engine.ValidateAndConsumeTicket(ticket2.ID, baseCtx); err != ErrTicketExpired {
		t.Errorf("case 9: expected ErrTicketExpired, got %v", err)
	}
}
