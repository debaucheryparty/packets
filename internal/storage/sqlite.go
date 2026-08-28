package storage

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"time"

	"github.com/waris4ly/packets/pkg/apitypes"
	_ "modernc.org/sqlite"
)

//go:embed migrations/001_initial.sql
var initialMigration string

var (
	ErrJobNotFound = errors.New("job not found")
	ErrCacheMiss   = errors.New("cache miss")
)

type JobStore struct {
	db *sql.DB
}

func NewJobStore(ctx context.Context, dbPath string) (*JobStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("NewJobStore open %q: %w", dbPath, err)
	}

	if _, err := db.ExecContext(ctx, initialMigration); err != nil {
		db.Close()
		return nil, fmt.Errorf("NewJobStore migrate: %w", err)
	}

	return &JobStore{db: db}, nil
}

func (s *JobStore) Close() error {
	return s.db.Close()
}

func (s *JobStore) CreateJob(ctx context.Context, job apitypes.Job) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO jobs (id, toolchain, cache_key, state, provider, submitted_at, artifact_ref)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		string(job.ID), string(job.Toolchain), job.CacheKey,
		int(job.State), string(job.Provider), job.SubmittedAt.UTC(), string(job.ArtifactRef),
	)
	if err != nil {
		return fmt.Errorf("CreateJob %s: %w", job.ID, err)
	}
	return nil
}

func (s *JobStore) GetJob(ctx context.Context, id apitypes.JobID) (apitypes.Job, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, toolchain, cache_key, state, provider, submitted_at, completed_at, artifact_ref
		 FROM jobs WHERE id = ?`, string(id),
	)
	return scanJob(row)
}

func (s *JobStore) UpdateJobState(ctx context.Context, id apitypes.JobID, state apitypes.JobState) error {
	var completedAt *time.Time
	if state == apitypes.JobStateSucceeded || state == apitypes.JobStateFailed || state == apitypes.JobStateFallbackLocal {
		now := time.Now().UTC()
		completedAt = &now
	}

	res, err := s.db.ExecContext(ctx,
		`UPDATE jobs SET state = ?, completed_at = ? WHERE id = ?`,
		int(state), completedAt, string(id),
	)
	if err != nil {
		return fmt.Errorf("UpdateJobState %s to %s: %w", id, state, err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("UpdateJobState %s: %w", id, ErrJobNotFound)
	}
	return nil
}

func (s *JobStore) SetJobArtifact(ctx context.Context, id apitypes.JobID, ref apitypes.ArtifactRef) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE jobs SET artifact_ref = ? WHERE id = ?`,
		string(ref), string(id),
	)
	if err != nil {
		return fmt.Errorf("SetJobArtifact %s: %w", id, err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("SetJobArtifact %s: %w", id, ErrJobNotFound)
	}
	return nil
}

func (s *JobStore) ListRecentJobs(ctx context.Context, limit int) ([]apitypes.Job, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, toolchain, cache_key, state, provider, submitted_at, completed_at, artifact_ref
		 FROM jobs ORDER BY submitted_at DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("ListRecentJobs: %w", err)
	}
	defer rows.Close()

	var jobs []apitypes.Job
	for rows.Next() {
		job, scanErr := scanJobRows(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("ListRecentJobs scan: %w", scanErr)
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (s *JobStore) Lookup(ctx context.Context, key string) (apitypes.ArtifactRef, bool, error) {
	var ref string
	err := s.db.QueryRowContext(ctx,
		`SELECT artifact_ref FROM cache_entries WHERE cache_key = ?`, key,
	).Scan(&ref)

	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("Lookup cache key %q: %w", key, err)
	}
	return apitypes.ArtifactRef(ref), true, nil
}

func (s *JobStore) Store(ctx context.Context, key string, artifact apitypes.ArtifactRef) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO cache_entries (cache_key, artifact_ref, created_at)
		 VALUES (?, ?, ?)`,
		key, string(artifact), time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("Store cache key %q: %w", key, err)
	}
	return nil
}

func scanJob(row *sql.Row) (apitypes.Job, error) {
	var j apitypes.Job
	var id, tc, provider, artifactRef string
	var state int
	var completedAt *time.Time

	err := row.Scan(&id, &tc, &j.CacheKey, &state, &provider, &j.SubmittedAt, &completedAt, &artifactRef)
	if errors.Is(err, sql.ErrNoRows) {
		return j, fmt.Errorf("scanJob: %w", ErrJobNotFound)
	}
	if err != nil {
		return j, fmt.Errorf("scanJob: %w", err)
	}

	j.ID = apitypes.JobID(id)
	j.Toolchain = apitypes.Toolchain(tc)
	j.State = apitypes.JobState(state)
	j.Provider = apitypes.ProviderName(provider)
	j.ArtifactRef = apitypes.ArtifactRef(artifactRef)
	j.CompletedAt = completedAt
	return j, nil
}

func scanJobRows(rows *sql.Rows) (apitypes.Job, error) {
	var j apitypes.Job
	var id, tc, provider, artifactRef string
	var state int
	var completedAt *time.Time

	err := rows.Scan(&id, &tc, &j.CacheKey, &state, &provider, &j.SubmittedAt, &completedAt, &artifactRef)
	if err != nil {
		return j, err
	}

	j.ID = apitypes.JobID(id)
	j.Toolchain = apitypes.Toolchain(tc)
	j.State = apitypes.JobState(state)
	j.Provider = apitypes.ProviderName(provider)
	j.ArtifactRef = apitypes.ArtifactRef(artifactRef)
	j.CompletedAt = completedAt
	return j, nil
}
