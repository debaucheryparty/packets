CREATE TABLE IF NOT EXISTS workspace_transactions (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    subspace_id TEXT NOT NULL DEFAULT '',
    base_snapshot_ref TEXT NOT NULL,
    working_snapshot_ref TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_tx_project ON workspace_transactions (project_id);
CREATE INDEX IF NOT EXISTS idx_tx_status ON workspace_transactions (status);
