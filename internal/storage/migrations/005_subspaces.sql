CREATE TABLE IF NOT EXISTS subspaces (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    owner_id TEXT NOT NULL,
    worker_id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    environment_id TEXT NOT NULL,
    state TEXT NOT NULL,
    created_at DATETIME NOT NULL,
    last_used_at DATETIME NOT NULL,
    metadata TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_subspaces_owner_proj ON subspaces (owner_id, project_id);
CREATE INDEX IF NOT EXISTS idx_subspaces_state ON subspaces (state);
