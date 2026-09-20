package transaction

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/debaucheryparty/packets/pkg/apitypes"
)

var (
	ErrInvalidTransactionID = errors.New("invalid transaction ID")
	ErrTransactionNotOpen   = errors.New("transaction is not open")
	ErrProjectIDRequired    = errors.New("project ID is required")
)

type Store interface {
	CreateTransaction(ctx context.Context, tx apitypes.Transaction) error
	GetTransaction(ctx context.Context, id string) (apitypes.Transaction, error)
	ListTransactions(ctx context.Context, projectID string) ([]apitypes.Transaction, error)
	UpdateTransaction(ctx context.Context, id string, status apitypes.TransactionStatus, workingSnapshotRef string) error
}

type Manager struct {
	store  Store
	logger *slog.Logger
}

func NewManager(store Store, logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		store:  store,
		logger: logger,
	}
}

func (m *Manager) Create(ctx context.Context, projectID, subspaceID, baseSnapshotRef, desc string) (apitypes.Transaction, error) {
	if projectID == "" {
		return apitypes.Transaction{}, ErrProjectIDRequired
	}

	txID := generateTransactionID()
	now := time.Now().UTC()

	tx := apitypes.Transaction{
		ID:                 txID,
		ProjectID:          projectID,
		SubspaceID:         subspaceID,
		BaseSnapshotRef:    baseSnapshotRef,
		WorkingSnapshotRef: baseSnapshotRef,
		Status:             apitypes.TransactionOpen,
		Description:        desc,
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	if err := m.store.CreateTransaction(ctx, tx); err != nil {
		return apitypes.Transaction{}, fmt.Errorf("create transaction: %w", err)
	}

	m.logger.InfoContext(ctx, "workspace transaction opened",
		slog.String("id", tx.ID),
		slog.String("project", tx.ProjectID),
		slog.String("base_snapshot", tx.BaseSnapshotRef),
	)

	return tx, nil
}

func (m *Manager) Commit(ctx context.Context, id, workingSnapshotRef string) (apitypes.Transaction, error) {
	tx, err := m.store.GetTransaction(ctx, id)
	if err != nil {
		return apitypes.Transaction{}, err
	}

	if tx.Status != apitypes.TransactionOpen {
		return apitypes.Transaction{}, fmt.Errorf("%w: status is %s", ErrTransactionNotOpen, tx.Status)
	}

	if workingSnapshotRef == "" {
		workingSnapshotRef = tx.WorkingSnapshotRef
	}

	if err := m.store.UpdateTransaction(ctx, id, apitypes.TransactionCommitted, workingSnapshotRef); err != nil {
		return apitypes.Transaction{}, fmt.Errorf("commit transaction: %w", err)
	}

	tx.Status = apitypes.TransactionCommitted
	tx.WorkingSnapshotRef = workingSnapshotRef
	tx.UpdatedAt = time.Now().UTC()

	m.logger.InfoContext(ctx, "workspace transaction committed",
		slog.String("id", tx.ID),
		slog.String("working_snapshot", tx.WorkingSnapshotRef),
	)

	return tx, nil
}

func (m *Manager) Rollback(ctx context.Context, id string) (apitypes.Transaction, error) {
	tx, err := m.store.GetTransaction(ctx, id)
	if err != nil {
		return apitypes.Transaction{}, err
	}

	if tx.Status != apitypes.TransactionOpen {
		return apitypes.Transaction{}, fmt.Errorf("%w: status is %s", ErrTransactionNotOpen, tx.Status)
	}

	if err := m.store.UpdateTransaction(ctx, id, apitypes.TransactionRolledBack, tx.BaseSnapshotRef); err != nil {
		return apitypes.Transaction{}, fmt.Errorf("rollback transaction: %w", err)
	}

	tx.Status = apitypes.TransactionRolledBack
	tx.WorkingSnapshotRef = tx.BaseSnapshotRef
	tx.UpdatedAt = time.Now().UTC()

	m.logger.InfoContext(ctx, "workspace transaction rolled back",
		slog.String("id", tx.ID),
		slog.String("restored_snapshot", tx.BaseSnapshotRef),
	)

	return tx, nil
}

func (m *Manager) Get(ctx context.Context, id string) (apitypes.Transaction, error) {
	return m.store.GetTransaction(ctx, id)
}

func (m *Manager) List(ctx context.Context, projectID string) ([]apitypes.Transaction, error) {
	return m.store.ListTransactions(ctx, projectID)
}

func generateTransactionID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return "tx-" + hex.EncodeToString(b)
}
