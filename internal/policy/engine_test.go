package policy

import (
	"testing"
)

func TestClassifyCommand(t *testing.T) {
	pe := NewPolicyEngine(ApprovalNever)
	tests := []struct {
		cmd  string
		want CommandCategory
	}{
		{"git status", CategoryReadOnly},
		{"cargo build --release", CategoryBuild},
		{"go test ./...", CategoryTest},
		{"npm install", CategoryDependency},
		{"rm -rf /", CategoryDangerous},
		{"some-custom-tool --flag", CategoryCustom},
	}

	for _, tt := range tests {
		got := pe.ClassifyCommand(tt.cmd)
		if got != tt.want {
			t.Errorf("ClassifyCommand(%q) = %v, want %v", tt.cmd, got, tt.want)
		}
	}
}

func TestPolicyEngine_ApprovalModes(t *testing.T) {
	neverEngine := NewPolicyEngine(ApprovalNever)
	ec := ExecutionContext{
		User:    "alice",
		Command: "cargo build",
	}

	if neverEngine.RequiresApprovalFor(ec) {
		t.Errorf("ApprovalNever should not require approval")
	}

	alwaysEngine := NewPolicyEngine(ApprovalAlways)
	if !alwaysEngine.RequiresApprovalFor(ec) {
		t.Errorf("ApprovalAlways should require approval for non-whitelisted command")
	}

	alwaysEngine.WhitelistCommand("cargo build")
	if alwaysEngine.RequiresApprovalFor(ec) {
		t.Errorf("Whitelisted command should not require approval")
	}
}

func TestPolicyEngine_TicketLifecycle(t *testing.T) {
	engine := NewPolicyEngine(ApprovalAlways)
	ec := ExecutionContext{
		User:         "bob",
		ProjectID:    "proj-1",
		SnapshotHash: "hash-123",
		Command:      "make test",
	}

	pa, err := engine.CreatePendingApproval(ec)
	if err != nil {
		t.Fatalf("CreatePendingApproval failed: %v", err)
	}

	ticket, err := engine.ApprovePending(pa.ID)
	if err != nil {
		t.Fatalf("ApprovePending failed: %v", err)
	}

	if err := engine.ValidateAndConsumeTicket(ticket.ID, ec); err != nil {
		t.Fatalf("ValidateAndConsumeTicket failed: %v", err)
	}

	// Replay should fail
	if err := engine.ValidateAndConsumeTicket(ticket.ID, ec); err != ErrTicketAlreadyUsed {
		t.Errorf("expected ErrTicketAlreadyUsed on replay, got %v", err)
	}

	// Tampered request should fail
	pa2, err := engine.CreatePendingApproval(ec)
	if err != nil {
		t.Fatal(err)
	}
	ticket2, err := engine.ApprovePending(pa2.ID)
	if err != nil {
		t.Fatal(err)
	}

	tamperedEC := ec
	tamperedEC.Command = "make evil"
	if err := engine.ValidateAndConsumeTicket(ticket2.ID, tamperedEC); err != ErrApprovalMismatch {
		t.Errorf("expected ErrApprovalMismatch, got %v", err)
	}
}
