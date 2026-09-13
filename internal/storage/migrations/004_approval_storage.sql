CREATE TABLE IF NOT EXISTS pending_approvals (
    id TEXT PRIMARY KEY,
    user TEXT NOT NULL,
    project_id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    snapshot_hash TEXT NOT NULL,
    command TEXT NOT NULL,
    args TEXT NOT NULL,
    action TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    expires_at TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS approval_tickets (
    id TEXT PRIMARY KEY,
    request_hash TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    used INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS session_approvals (
    session_key TEXT PRIMARY KEY,
    created_at TIMESTAMP NOT NULL
);
