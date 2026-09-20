package subspace

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/debaucheryparty/packets/pkg/apitypes"
)

var (
	ErrInvalidSubspaceID    = errors.New("invalid subspace ID")
	ErrPathTraversal        = errors.New("path traversal detected in subspace workspace")
	ErrSubspaceNotReady     = errors.New("subspace is not ready for execution")
	ErrSubspaceNotSleeping  = errors.New("subspace is not sleeping")
	ErrSubspaceInvalidState = errors.New("invalid subspace state transition")
	ErrUnauthorizedAccess   = errors.New("unauthorized access to subspace")
)

type Store interface {
	CreateSubspace(ctx context.Context, sub apitypes.Subspace) error
	GetSubspace(ctx context.Context, id string) (apitypes.Subspace, error)
	ListSubspaces(ctx context.Context, ownerID, projectID string) ([]apitypes.Subspace, error)
	UpdateSubspaceState(ctx context.Context, id string, state apitypes.SubspaceState) error
	TouchSubspace(ctx context.Context, id string) error
	DeleteSubspace(ctx context.Context, id string) error
}

type CreateOptions struct {
	ProjectID     string
	OwnerID       string
	WorkerID      string
	WorkspaceID   string
	EnvironmentID string
	Metadata      map[string]string
}

type Manager struct {
	store        Store
	logger       *slog.Logger
	workspaceDir string
}

func NewManager(store Store, logger *slog.Logger, workspaceDir string) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	if workspaceDir == "" {
		workspaceDir = filepath.Join(os.TempDir(), "packets-subspaces")
	}
	_ = os.MkdirAll(workspaceDir, 0o755)

	return &Manager{
		store:        store,
		logger:       logger,
		workspaceDir: filepath.Clean(workspaceDir),
	}
}

func (m *Manager) Create(ctx context.Context, opts CreateOptions) (apitypes.Subspace, error) {
	owner := opts.OwnerID
	if owner == "" {
		owner = "default"
	}
	proj := opts.ProjectID
	if proj == "" {
		proj = "default"
	}

	subID := generateSubspaceID()
	wsID := opts.WorkspaceID
	if wsID == "" {
		wsID = "ws-" + subID
	}
	worker := opts.WorkerID
	if worker == "" {
		worker = "local"
	}
	envID := opts.EnvironmentID
	if envID == "" {
		envID = "env-default"
	}

	wsPath, err := m.resolveWorkspacePath(owner, proj, subID)
	if err != nil {
		return apitypes.Subspace{}, fmt.Errorf("resolve workspace path: %w", err)
	}

	if err := os.MkdirAll(wsPath, 0o755); err != nil {
		return apitypes.Subspace{}, fmt.Errorf("create subspace directory: %w", err)
	}

	now := time.Now().UTC()
	sub := apitypes.Subspace{
		ID:            subID,
		ProjectID:     proj,
		OwnerID:       owner,
		WorkerID:      worker,
		WorkspaceID:   wsID,
		EnvironmentID: envID,
		State:         apitypes.SubspaceReady,
		CreatedAt:     now,
		LastUsedAt:    now,
		Metadata:      opts.Metadata,
	}
	if sub.Metadata == nil {
		sub.Metadata = make(map[string]string)
	}

	if err := m.store.CreateSubspace(ctx, sub); err != nil {
		_ = os.RemoveAll(wsPath)
		return apitypes.Subspace{}, fmt.Errorf("persist subspace: %w", err)
	}

	m.logger.InfoContext(ctx, "subspace created",
		slog.String("id", sub.ID),
		slog.String("project", sub.ProjectID),
		slog.String("owner", sub.OwnerID),
		slog.String("worker", sub.WorkerID),
	)

	return sub, nil
}

func (m *Manager) Get(ctx context.Context, id string) (apitypes.Subspace, error) {
	if strings.TrimSpace(id) == "" {
		return apitypes.Subspace{}, ErrInvalidSubspaceID
	}
	sub, err := m.store.GetSubspace(ctx, id)
	if err != nil {
		return apitypes.Subspace{}, err
	}
	_ = m.store.TouchSubspace(ctx, id)
	return sub, nil
}

func (m *Manager) List(ctx context.Context, ownerID, projectID string) ([]apitypes.Subspace, error) {
	return m.store.ListSubspaces(ctx, ownerID, projectID)
}

func (m *Manager) UpdateState(ctx context.Context, id string, state apitypes.SubspaceState) error {
	return m.store.UpdateSubspaceState(ctx, id, state)
}

func (m *Manager) Touch(ctx context.Context, id string) error {
	return m.store.TouchSubspace(ctx, id)
}

func (m *Manager) Sleep(ctx context.Context, id string) (apitypes.Subspace, error) {
	sub, err := m.store.GetSubspace(ctx, id)
	if err != nil {
		return apitypes.Subspace{}, err
	}
	if !apitypes.CanTransitionSubspaceState(sub.State, apitypes.SubspaceSleeping) {
		return apitypes.Subspace{}, fmt.Errorf("cannot sleep subspace from state %q: %w", sub.State, ErrSubspaceInvalidState)
	}

	if err := m.store.UpdateSubspaceState(ctx, id, apitypes.SubspaceSleeping); err != nil {
		return apitypes.Subspace{}, fmt.Errorf("update subspace state to sleeping: %w", err)
	}
	sub.State = apitypes.SubspaceSleeping
	m.logger.InfoContext(ctx, "subspace entered sleeping state", slog.String("id", id))
	return sub, nil
}

func (m *Manager) Wake(ctx context.Context, id string) (apitypes.Subspace, error) {
	sub, err := m.store.GetSubspace(ctx, id)
	if err != nil {
		return apitypes.Subspace{}, err
	}
	if !apitypes.CanTransitionSubspaceState(sub.State, apitypes.SubspaceReady) {
		return apitypes.Subspace{}, fmt.Errorf("cannot wake subspace from state %q: %w", sub.State, ErrSubspaceInvalidState)
	}

	if err := m.store.UpdateSubspaceState(ctx, id, apitypes.SubspaceReady); err != nil {
		return apitypes.Subspace{}, fmt.Errorf("update subspace state to ready: %w", err)
	}
	_ = m.store.TouchSubspace(ctx, id)
	sub.State = apitypes.SubspaceReady
	m.logger.InfoContext(ctx, "subspace woke up and returned to ready", slog.String("id", id))
	return sub, nil
}

func (m *Manager) EnsureReady(ctx context.Context, id string) (apitypes.Subspace, error) {
	sub, err := m.Get(ctx, id)
	if err != nil {
		return apitypes.Subspace{}, err
	}
	if sub.State == apitypes.SubspaceSleeping {
		return m.Wake(ctx, id)
	}
	if sub.State != apitypes.SubspaceReady && sub.State != apitypes.SubspaceBusy {
		return sub, fmt.Errorf("subspace %s is not ready (state: %s)", id, sub.State)
	}
	return sub, nil
}

func (m *Manager) Destroy(ctx context.Context, id string) error {
	sub, err := m.store.GetSubspace(ctx, id)
	if err != nil {
		return err
	}

	_ = m.store.UpdateSubspaceState(ctx, id, apitypes.SubspaceDestroying)

	wsPath, err := m.resolveWorkspacePath(sub.OwnerID, sub.ProjectID, sub.ID)
	if err == nil {
		_ = os.RemoveAll(wsPath)
	}

	if err := m.store.DeleteSubspace(ctx, id); err != nil {
		return fmt.Errorf("delete subspace record %s: %w", id, err)
	}

	m.logger.InfoContext(ctx, "subspace destroyed", slog.String("id", id))
	return nil
}

func (m *Manager) WorkspacePath(sub apitypes.Subspace) (string, error) {
	return m.resolveWorkspacePath(sub.OwnerID, sub.ProjectID, sub.ID)
}

func (m *Manager) resolveWorkspacePath(ownerID, projectID, subID string) (string, error) {
	if strings.Contains(ownerID, "..") || strings.Contains(projectID, "..") || strings.Contains(subID, "..") {
		return "", ErrPathTraversal
	}
	cleanOwner := filepath.Clean(ownerID)
	cleanProj := filepath.Clean(projectID)
	cleanSub := filepath.Clean(subID)

	target := filepath.Join(m.workspaceDir, cleanOwner, cleanProj, cleanSub)
	target = filepath.Clean(target)

	rel, err := filepath.Rel(m.workspaceDir, target)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", ErrPathTraversal
	}
	return target, nil
}

func generateSubspaceID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return "sub-" + hex.EncodeToString(b)
}

type WorkloadChecker interface {
	HasActiveSubspaceWorkload(ctx context.Context, subspaceID string) (bool, error)
}

func (m *Manager) AutoSleepIdle(ctx context.Context, idleTimeout time.Duration) ([]apitypes.Subspace, error) {
	subs, err := m.store.ListSubspaces(ctx, "", "")
	if err != nil {
		return nil, fmt.Errorf("list subspaces for idle check: %w", err)
	}

	checker, hasChecker := m.store.(WorkloadChecker)
	now := time.Now().UTC()
	var slept []apitypes.Subspace

	for _, sub := range subs {
		if sub.State != apitypes.SubspaceReady {
			continue
		}
		if now.Sub(sub.LastUsedAt) < idleTimeout {
			continue
		}

		if hasChecker {
			active, err := checker.HasActiveSubspaceWorkload(ctx, sub.ID)
			if err == nil && active {
				continue
			}
		}

		updated, err := m.Sleep(ctx, sub.ID)
		if err != nil {
			m.logger.WarnContext(ctx, "failed to auto-sleep idle subspace",
				slog.String("id", sub.ID),
				slog.String("error", err.Error()),
			)
			continue
		}
		slept = append(slept, updated)
	}

	return slept, nil
}

func (m *Manager) StartIdleReaper(ctx context.Context, checkInterval, idleTimeout time.Duration) {
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, _ = m.AutoSleepIdle(ctx, idleTimeout)
		}
	}
}
