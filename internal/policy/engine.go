package policy

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	ErrApprovalRequired      = errors.New("human approval required")
	ErrInvalidApprovalTicket = errors.New("invalid approval ticket")
	ErrTicketAlreadyUsed     = errors.New("approval ticket already used")
	ErrTicketExpired         = errors.New("approval ticket expired")
	ErrApprovalMismatch      = errors.New("approval ticket does not match request state (snapshot, command, or arguments changed)")
	ErrPendingNotFound       = errors.New("pending approval not found")
	ErrPendingExpired        = errors.New("pending approval expired")
)

type CommandCategory string

const (
	CategoryReadOnly   CommandCategory = "READ_ONLY"
	CategoryBuild      CommandCategory = "BUILD"
	CategoryTest       CommandCategory = "TEST"
	CategoryDependency CommandCategory = "DEPENDENCY"
	CategoryDangerous  CommandCategory = "DANGEROUS"
	CategoryCustom     CommandCategory = "CUSTOM"
)

type ApprovalMode string

const (
	ApprovalAlways  ApprovalMode = "always"
	ApprovalSession ApprovalMode = "session"
	ApprovalNever   ApprovalMode = "never"
)

type ExecutionContext struct {
	User         string   `json:"user"`
	ProjectID    string   `json:"project_id"`
	WorkspaceID  string   `json:"workspace_id"`
	SnapshotHash string   `json:"snapshot_hash"`
	Command      string   `json:"command"`
	Args         []string `json:"args"`
	Action       string   `json:"action"`
}

func (ec ExecutionContext) Hash() string {
	h := sha256.New()
	h.Write([]byte(ec.User + "\x00"))
	h.Write([]byte(ec.ProjectID + "\x00"))
	h.Write([]byte(ec.WorkspaceID + "\x00"))
	h.Write([]byte(ec.SnapshotHash + "\x00"))
	h.Write([]byte(ec.Command + "\x00"))
	for _, a := range ec.Args {
		h.Write([]byte(a + "\x00"))
	}
	h.Write([]byte(ec.Action + "\x00"))
	return hex.EncodeToString(h.Sum(nil))
}

type PendingApproval struct {
	ID        string           `json:"id"`
	Context   ExecutionContext `json:"context"`
	CreatedAt time.Time        `json:"created_at"`
	ExpiresAt time.Time        `json:"expires_at"`
}

type ApprovalTicket struct {
	ID          string    `json:"id"`
	RequestHash string    `json:"request_hash"`
	CreatedAt   time.Time `json:"created_at"`
	ExpiresAt   time.Time `json:"expires_at"`
	Used        bool      `json:"used"`
}

type ApprovalRequest struct {
	Command     string          `json:"command"`
	Category    CommandCategory `json:"category"`
	Project     string          `json:"project"`
	WorkspaceID string          `json:"workspace_id"`
	TargetNode  string          `json:"target_node"`
}

type PolicyEngine struct {
	mode          ApprovalMode
	sessionTokens map[string]bool
	pending       map[string]*PendingApproval
	tickets       map[string]*ApprovalTicket
	mu            sync.RWMutex
}

func NewPolicyEngine(mode ApprovalMode) *PolicyEngine {
	if mode == "" {
		mode = ApprovalAlways
	}
	return &PolicyEngine{
		mode:          mode,
		sessionTokens: make(map[string]bool),
		pending:       make(map[string]*PendingApproval),
		tickets:       make(map[string]*ApprovalTicket),
	}
}

func (p *PolicyEngine) ClassifyCommand(cmd string) CommandCategory {
	trimmed := strings.TrimSpace(cmd)
	lower := strings.ToLower(trimmed)

	dangerousPatterns := []string{"rm -rf /", "mkfs", "dd if=", ":(){ :|:& };:", "chmod -R 777 /", "> /dev/sd"}
	for _, dp := range dangerousPatterns {
		if strings.Contains(lower, dp) {
			return CategoryDangerous
		}
	}

	if strings.HasPrefix(lower, "ls ") || lower == "ls" ||
		strings.HasPrefix(lower, "cat ") ||
		strings.HasPrefix(lower, "grep ") ||
		strings.HasPrefix(lower, "find ") ||
		strings.HasPrefix(lower, "git status") ||
		strings.HasPrefix(lower, "git log") ||
		strings.HasPrefix(lower, "uname") ||
		strings.HasPrefix(lower, "which ") ||
		strings.HasPrefix(lower, "echo ") {
		return CategoryReadOnly
	}

	if strings.Contains(lower, "assemble") ||
		strings.Contains(lower, "west build") ||
		strings.Contains(lower, "cargo build") ||
		strings.Contains(lower, "go build") ||
		strings.Contains(lower, "make") ||
		strings.Contains(lower, "ninja") {
		return CategoryBuild
	}

	if strings.Contains(lower, "test") ||
		strings.Contains(lower, "check") {
		return CategoryTest
	}

	if strings.Contains(lower, "install") ||
		strings.Contains(lower, "update") ||
		strings.Contains(lower, "fetch") ||
		strings.Contains(lower, "download") {
		return CategoryDependency
	}

	return CategoryCustom
}

func (p *PolicyEngine) ValidatePath(targetPath, workspaceRoot string) error {
	cleanedTarget := filepath.Clean(targetPath)
	cleanedRoot := filepath.Clean(workspaceRoot)

	if !strings.HasPrefix(cleanedTarget, cleanedRoot) && cleanedTarget != cleanedRoot {
		return fmt.Errorf("path %s escapes workspace root %s", targetPath, workspaceRoot)
	}

	forbiddenPrefixes := []string{"/etc", "/root", "/boot", "/dev", "/sys", "/proc"}
	for _, fp := range forbiddenPrefixes {
		if strings.HasPrefix(cleanedTarget, fp) {
			return fmt.Errorf("access to system path %s is forbidden", targetPath)
		}
	}

	return nil
}

func (p *PolicyEngine) RequiresApproval(req ApprovalRequest) bool {
	if p.mode == ApprovalNever {
		return false
	}

	if req.Category == CategoryReadOnly {
		return false
	}

	if p.mode == ApprovalSession {
		p.mu.RLock()
		defer p.mu.RUnlock()
		sessionKey := fmt.Sprintf("%s:%s", req.WorkspaceID, req.Category)
		return !p.sessionTokens[sessionKey]
	}

	return true
}

func (p *PolicyEngine) GrantSessionApproval(req ApprovalRequest) {
	p.mu.Lock()
	defer p.mu.Unlock()
	sessionKey := fmt.Sprintf("%s:%s", req.WorkspaceID, req.Category)
	p.sessionTokens[sessionKey] = true
}

func randomID(prefix string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s_%s", prefix, hex.EncodeToString(b))
}

func (p *PolicyEngine) RequiresApprovalFor(ec ExecutionContext) bool {
	if p.mode == ApprovalNever {
		return false
	}
	cat := p.ClassifyCommand(ec.Command)
	return cat != CategoryReadOnly
}

func (p *PolicyEngine) CreatePendingApproval(ec ExecutionContext) (*PendingApproval, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now().UTC()
	pa := &PendingApproval{
		ID:        randomID("appr_req"),
		Context:   ec,
		CreatedAt: now,
		ExpiresAt: now.Add(5 * time.Minute),
	}
	p.pending[pa.ID] = pa
	return pa, nil
}

func (p *PolicyEngine) GetPendingApproval(pendingID string) (*PendingApproval, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	pa, ok := p.pending[pendingID]
	if !ok {
		return nil, ErrPendingNotFound
	}
	if time.Now().UTC().After(pa.ExpiresAt) {
		return nil, ErrPendingExpired
	}
	return pa, nil
}

func (p *PolicyEngine) ApprovePending(pendingID string) (*ApprovalTicket, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	pa, ok := p.pending[pendingID]
	if !ok {
		return nil, ErrPendingNotFound
	}
	now := time.Now().UTC()
	if now.After(pa.ExpiresAt) {
		delete(p.pending, pendingID)
		return nil, ErrPendingExpired
	}

	delete(p.pending, pendingID)

	ticket := &ApprovalTicket{
		ID:          randomID("ticket"),
		RequestHash: pa.Context.Hash(),
		CreatedAt:   now,
		ExpiresAt:   now.Add(5 * time.Minute),
		Used:        false,
	}
	p.tickets[ticket.ID] = ticket
	return ticket, nil
}

func (p *PolicyEngine) ValidateAndConsumeTicket(ticketID string, ec ExecutionContext) error {
	if ticketID == "" {
		return ErrApprovalRequired
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	ticket, ok := p.tickets[ticketID]
	if !ok {
		return ErrInvalidApprovalTicket
	}

	if ticket.Used {
		return ErrTicketAlreadyUsed
	}

	now := time.Now().UTC()
	if now.After(ticket.ExpiresAt) {
		return ErrTicketExpired
	}

	if ticket.RequestHash != ec.Hash() {
		return ErrApprovalMismatch
	}

	ticket.Used = true
	return nil
}

func (p *PolicyEngine) ExpireTicketForTest(ticketID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if ticket, ok := p.tickets[ticketID]; ok {
		ticket.ExpiresAt = time.Now().UTC().Add(-1 * time.Hour)
	}
}
