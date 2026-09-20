CREATE TABLE IF NOT EXISTS subspace_services (
    id TEXT PRIMARY KEY,
    subspace_id TEXT NOT NULL,
    name TEXT NOT NULL,
    image TEXT NOT NULL,
    command TEXT NOT NULL DEFAULT '[]',
    environment TEXT NOT NULL DEFAULT '{}',
    ports TEXT NOT NULL DEFAULT '[]',
    status TEXT NOT NULL,
    driver TEXT NOT NULL,
    container_id TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    FOREIGN KEY (subspace_id) REFERENCES subspaces(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_services_subspace ON subspace_services (subspace_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_services_subspace_name ON subspace_services (subspace_id, name);
