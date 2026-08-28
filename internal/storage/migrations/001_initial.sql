CREATE TABLE IF NOT EXISTS jobs (
    id TEXT PRIMARY KEY,
    toolchain TEXT NOT NULL,
    cache_key TEXT NOT NULL,
    state INTEGER NOT NULL DEFAULT 0,
    provider TEXT NOT NULL DEFAULT '',
    submitted_at DATETIME NOT NULL,
    completed_at DATETIME,
    artifact_ref TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_jobs_cache_key ON jobs(cache_key);
CREATE INDEX IF NOT EXISTS idx_jobs_state ON jobs(state);

CREATE TABLE IF NOT EXISTS cache_entries (
    cache_key TEXT PRIMARY KEY,
    artifact_ref TEXT NOT NULL,
    created_at DATETIME NOT NULL
);
