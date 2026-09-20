package apitypes

import (
	"time"
)

type TransactionStatus string

const (
	TransactionOpen       TransactionStatus = "open"
	TransactionCommitted  TransactionStatus = "committed"
	TransactionRolledBack TransactionStatus = "rolled_back"
	TransactionFailed     TransactionStatus = "failed"
)

type Transaction struct {
	ID                 string            `json:"id"`
	ProjectID          string            `json:"project_id"`
	SubspaceID         string            `json:"subspace_id"`
	BaseSnapshotRef    string            `json:"base_snapshot_ref"`
	WorkingSnapshotRef string            `json:"working_snapshot_ref"`
	Status             TransactionStatus `json:"status"`
	Description        string            `json:"description"`
	CreatedAt          time.Time         `json:"created_at"`
	UpdatedAt          time.Time         `json:"updated_at"`
}
