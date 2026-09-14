package policy

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
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

type ApprovalStore interface {
	SavePending(ctx context.Context, pa *PendingApproval) error
	GetPending(ctx context.Context, id string) (*PendingApproval, error)
	DeletePending(ctx context.Context, id string) error
	SaveTicket(ctx context.Context, ticket *ApprovalTicket) error
	GetTicket(ctx context.Context, id string) (*ApprovalTicket, error)
	ConsumeTicket(ctx context.Context, id string, reqHash string) error
	SaveSessionApproval(ctx context.Context, key string) error
	HasSessionApproval(ctx context.Context, key string) (bool, error)
	ExpireTicketForTest(ctx context.Context, id string) error
}

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

type InteractiveApprover interface {
	RequestApproval(ctx context.Context, ec ExecutionContext) (approved bool, always bool, err error)
}

type PolicyEngine struct {
	mode          ApprovalMode
	store         ApprovalStore
	sessionTokens map[string]bool
	pending       map[string]*PendingApproval
	tickets       map[string]*ApprovalTicket
	whitelisted   map[string]bool
	approver      InteractiveApprover
	mu            sync.RWMutex
}

func NewPolicyEngine(mode ApprovalMode) *PolicyEngine {
	return NewPolicyEngineWithStore(mode, nil)
}

func NewPolicyEngineWithStore(mode ApprovalMode, store ApprovalStore) *PolicyEngine {
	if mode == "" {
		mode = ApprovalAlways
	}
	return &PolicyEngine{
		mode:          mode,
		store:         store,
		sessionTokens: make(map[string]bool),
		pending:       make(map[string]*PendingApproval),
		tickets:       make(map[string]*ApprovalTicket),
		whitelisted:   make(map[string]bool),
	}
}

func (p *PolicyEngine) SetApprover(a InteractiveApprover) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.approver = a
}

func (p *PolicyEngine) Approver() InteractiveApprover {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.approver
}

func (p *PolicyEngine) WhitelistCommand(cmd string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	clean := strings.ToLower(strings.TrimSpace(cmd))
	p.whitelisted[clean] = true
	parts := strings.Fields(clean)
	if len(parts) > 0 {
		p.whitelisted[parts[0]] = true
	}
}

func (p *PolicyEngine) IsWhitelisted(cmd string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	clean := strings.ToLower(strings.TrimSpace(cmd))
	if p.whitelisted[clean] {
		return true
	}
	parts := strings.Fields(clean)
	if len(parts) > 0 && p.whitelisted[parts[0]] {
		return true
	}
	return false
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
		strings.HasPrefix(lower, "echo ") ||
		strings.HasPrefix(lower, "sw_vers") ||
		strings.HasPrefix(lower, "whoami") ||
		strings.HasPrefix(lower, "pwd") ||
		strings.HasPrefix(lower, "uptime") ||
		strings.HasPrefix(lower, "date") ||
		strings.HasPrefix(lower, "hostname") ||
		strings.HasPrefix(lower, "df") ||
		strings.HasPrefix(lower, "ps") ||
		lower == "true" ||
		lower == "false" ||
		strings.HasPrefix(lower, "true ") ||
		strings.HasPrefix(lower, "false ") {
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
		sessionKey := fmt.Sprintf("%s:%s", req.WorkspaceID, req.Category)
		if p.store != nil {
			has, err := p.store.HasSessionApproval(context.Background(), sessionKey)
			if err == nil && has {
				return false
			}
		}
		p.mu.RLock()
		defer p.mu.RUnlock()
		return !p.sessionTokens[sessionKey]
	}

	return true
}

func (p *PolicyEngine) GrantSessionApproval(req ApprovalRequest) {
	sessionKey := fmt.Sprintf("%s:%s", req.WorkspaceID, req.Category)
	if p.store != nil {
		_ = p.store.SaveSessionApproval(context.Background(), sessionKey)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
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
	if p.IsWhitelisted(ec.Command) {
		return false
	}
	cat := p.ClassifyCommand(ec.Command)
	return cat != CategoryReadOnly
}

func (p *PolicyEngine) CreatePendingApproval(ec ExecutionContext) (*PendingApproval, error) {
	now := time.Now().UTC()
	pa := &PendingApproval{
		ID:        randomID("appr_req"),
		Context:   ec,
		CreatedAt: now,
		ExpiresAt: now.Add(5 * time.Minute),
	}
	if p.store != nil {
		if err := p.store.SavePending(context.Background(), pa); err != nil {
			return nil, err
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pending[pa.ID] = pa
	return pa, nil
}

func (p *PolicyEngine) GetPendingApproval(pendingID string) (*PendingApproval, error) {
	if p.store != nil {
		return p.store.GetPending(context.Background(), pendingID)
	}
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
	if p.store != nil {
		pa, err := p.store.GetPending(context.Background(), pendingID)
		if err != nil {
			return nil, err
		}
		_ = p.store.DeletePending(context.Background(), pendingID)

		now := time.Now().UTC()
		ticket := &ApprovalTicket{
			ID:          randomID("ticket"),
			RequestHash: pa.Context.Hash(),
			CreatedAt:   now,
			ExpiresAt:   now.Add(5 * time.Minute),
			Used:        false,
		}
		if err := p.store.SaveTicket(context.Background(), ticket); err != nil {
			return nil, err
		}
		p.mu.Lock()
		p.tickets[ticket.ID] = ticket
		p.mu.Unlock()
		return ticket, nil
	}

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

	if p.store != nil {
		return p.store.ConsumeTicket(context.Background(), ticketID, ec.Hash())
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
	if p.store != nil {
		_ = p.store.ExpireTicketForTest(context.Background(), ticketID)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if ticket, ok := p.tickets[ticketID]; ok {
		ticket.ExpiresAt = time.Now().UTC().Add(-1 * time.Hour)
	}
}

type ConsoleApprover struct {
	timeout time.Duration
	in      io.Reader
	out     io.Writer
	mu      sync.Mutex
}

func NewConsoleApprover() *ConsoleApprover {
	return &ConsoleApprover{
		timeout: 0,
		in:      os.Stdin,
		out:     os.Stdout,
	}
}

func (c *ConsoleApprover) SetTimeout(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.timeout = d
}

func (c *ConsoleApprover) SetIO(in io.Reader, out io.Writer) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.in = in
	c.out = out
}

func (c *ConsoleApprover) RequestApproval(ctx context.Context, ec ExecutionContext) (bool, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	user := ec.User
	if user == "" {
		user = "remote-peer"
	}

	fmt.Fprintf(c.out, "\n\033[1;33m[packetsd :: FRIEND APPROVAL REQUIRED]\033[0m\n")
	fmt.Fprintf(c.out, "Peer \033[1;36m%s\033[0m wants to run a non-whitelisted command:\n", user)
	fmt.Fprintf(c.out, "  \033[1;37mCommand:\033[0m %s\n", ec.Command)
	fmt.Fprintf(c.out, "  \033[1;37mAction:\033[0m  %s\n", ec.Action)
	if c.timeout > 0 {
		fmt.Fprintf(c.out, "Allow execution? [\033[1;32my\033[0m]es / [\033[1;34ma\033[0m]lways whitelist / [\033[1;31mn\033[0m]o (timeout %s): ", c.timeout)
	} else {
		fmt.Fprintf(c.out, "Allow execution? [\033[1;32my\033[0m]es / [\033[1;34ma\033[0m]lways whitelist / [\033[1;31mn\033[0m]o: ")
	}

	answerCh := make(chan string, 1)
	errCh := make(chan error, 1)

	go func() {
		reader := bufio.NewReader(c.in)
		line, err := reader.ReadString('\n')
		if err != nil {
			errCh <- err
			return
		}
		answerCh <- strings.TrimSpace(line)
	}()

	var timeoutCh <-chan time.Time
	if c.timeout > 0 {
		timer := time.NewTimer(c.timeout)
		defer timer.Stop()
		timeoutCh = timer.C
	}

	select {
	case <-ctx.Done():
		fmt.Fprintf(c.out, "\n\033[31mRequest cancelled by client.\033[0m\n")
		return false, false, ctx.Err()
	case <-timeoutCh:
		fmt.Fprintf(c.out, "\n\033[31mApproval timed out (no response).\033[0m\n")
		return false, false, errors.New("approval timed out")
	case err := <-errCh:
		return false, false, err
	case ans := <-answerCh:
		lower := strings.ToLower(ans)
		if lower == "y" || lower == "yes" {
			fmt.Fprintf(c.out, "\033[32mApproved for this run.\033[0m\n\n")
			return true, false, nil
		}
		if lower == "a" || lower == "always" {
			fmt.Fprintf(c.out, "\033[32mApproved and whitelisted for this session.\033[0m\n\n")
			return true, true, nil
		}
		fmt.Fprintf(c.out, "\033[31mRejected by owner.\033[0m\n\n")
		return false, false, nil
	}
}
