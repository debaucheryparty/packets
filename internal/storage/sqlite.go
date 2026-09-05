package storage

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/debaucheryparty/packets/pkg/apitypes"
	_ "modernc.org/sqlite"
)

//go:embed migrations/001_initial.sql
var migration001 string

//go:embed migrations/002_execution_spec.sql
var migration002 string

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

	if _, err := db.ExecContext(ctx, migration001); err != nil {
		db.Close() //nolint:errcheck
		return nil, fmt.Errorf("NewJobStore initial migration: %w", err)
	}

	if err := runMigrations(db); err != nil {
		db.Close() //nolint:errcheck
		return nil, fmt.Errorf("NewJobStore migrations: %w", err)
	}

	return &JobStore{db: db}, nil
}

func (s *JobStore) Close() error {
	return s.db.Close()
}

func (s *JobStore) CreateJob(ctx context.Context, job apitypes.Job) error {
	cmdArgs, _ := json.Marshal(job.CommandArgs)
	artifactPaths, _ := json.Marshal(job.ArtifactPaths)

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO jobs
		 (id, toolchain, cache_key, state, provider, runner, source_mode, snapshot_ref,
		  command_args, artifact_paths, image, error, owner, submitted_at, artifact_ref)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		string(job.ID), string(job.Toolchain), job.CacheKey,
		int(job.State), string(job.Provider), string(job.Runner), string(job.SourceMode),
		job.SnapshotRef, string(cmdArgs), string(artifactPaths),
		job.Image, job.Error, job.Owner,
		job.SubmittedAt.UTC(), string(job.ArtifactRef),
	)
	if err != nil {
		return fmt.Errorf("CreateJob %s: %w", job.ID, err)
	}
	return nil
}

func (s *JobStore) GetJob(ctx context.Context, id apitypes.JobID) (apitypes.Job, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, toolchain, cache_key, state, provider, runner, source_mode, snapshot_ref,
		        command_args, artifact_paths, image, error, owner,
		        submitted_at, completed_at, artifact_ref
		 FROM jobs WHERE id = ?`, string(id),
	)
	return scanJob(row)
}

func (s *JobStore) UpdateJobState(ctx context.Context, id apitypes.JobID, state apitypes.JobState) error {
	var completedAt *time.Time
	if state.IsTerminal() {
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

func (s *JobStore) CompleteJob(ctx context.Context, id apitypes.JobID, ref apitypes.ArtifactRef, cacheKey string) error {
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("CompleteJob begin: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE jobs SET state = ?, completed_at = ?, artifact_ref = ? WHERE id = ?`,
		int(apitypes.JobStateSucceeded), now, string(ref), string(id),
	); err != nil {
		tx.Rollback() //nolint:errcheck
		return fmt.Errorf("CompleteJob update job: %w", err)
	}
	if cacheKey != "" && ref != "" {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR REPLACE INTO cache_entries (cache_key, artifact_ref, created_at) VALUES (?, ?, ?)`,
			cacheKey, string(ref), now,
		); err != nil {
			tx.Rollback() //nolint:errcheck
			return fmt.Errorf("CompleteJob write cache: %w", err)
		}
	}
	return tx.Commit()
}

func (s *JobStore) FailJob(ctx context.Context, id apitypes.JobID, errMsg string) error {
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx,
		`UPDATE jobs SET state = ?, completed_at = ?, error = ? WHERE id = ?`,
		int(apitypes.JobStateFailed), now, errMsg, string(id),
	)
	if err != nil {
		return fmt.Errorf("FailJob %s: %w", id, err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("FailJob %s: %w", id, ErrJobNotFound)
	}
	return nil
}

func (s *JobStore) ListJobsByState(ctx context.Context, states ...apitypes.JobState) ([]apitypes.Job, error) {
	if len(states) == 0 {
		return nil, nil
	}
	placeholders := make([]byte, 0, len(states)*2)
	args := make([]any, len(states))
	for i, st := range states {
		if i > 0 {
			placeholders = append(placeholders, ',')
		}
		placeholders = append(placeholders, '?')
		args[i] = int(st)
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, toolchain, cache_key, state, provider, runner, source_mode, snapshot_ref,
		        command_args, artifact_paths, image, error, owner,
		        submitted_at, completed_at, artifact_ref
		 FROM jobs WHERE state IN (`+string(placeholders)+`)
		 ORDER BY submitted_at ASC`, args...,
	)
	if err != nil {
		return nil, fmt.Errorf("ListJobsByState: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var jobs []apitypes.Job
	for rows.Next() {
		job, err := scanJobRows(rows)
		if err != nil {
			return nil, fmt.Errorf("ListJobsByState scan: %w", err)
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (s *JobStore) ListRecentJobs(ctx context.Context, limit int) ([]apitypes.Job, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, toolchain, cache_key, state, provider, runner, source_mode, snapshot_ref,
		        command_args, artifact_paths, image, error, owner,
		        submitted_at, completed_at, artifact_ref
		 FROM jobs ORDER BY submitted_at DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("ListRecentJobs: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var jobs []apitypes.Job
	for rows.Next() {
		job, err := scanJobRows(rows)
		if err != nil {
			return nil, fmt.Errorf("ListRecentJobs scan: %w", err)
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
		`INSERT OR REPLACE INTO cache_entries (cache_key, artifact_ref, created_at) VALUES (?, ?, ?)`,
		key, string(artifact), time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("Store cache key %q: %w", key, err)
	}
	return nil
}

func (s *JobStore) DeleteCacheEntries(ctx context.Context, toolchain string) (int, error) {
	var res sql.Result
	var err error
	if toolchain == "" {
		res, err = s.db.ExecContext(ctx, `DELETE FROM cache_entries`)
	} else {
		res, err = s.db.ExecContext(ctx,
			`DELETE FROM cache_entries WHERE cache_key LIKE ?`, toolchain+":%",
		)
	}
	if err != nil {
		return 0, fmt.Errorf("DeleteCacheEntries: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
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

func scanJob(row *sql.Row) (apitypes.Job, error) {
	var j apitypes.Job
	var id, tc, provider, runner, sourceMode, snapshotRef, cmdArgs, artPaths, image, errMsg, owner, artifactRef string
	var state int
	var completedAt *time.Time

	err := row.Scan(
		&id, &tc, &j.CacheKey, &state, &provider, &runner, &sourceMode, &snapshotRef,
		&cmdArgs, &artPaths, &image, &errMsg, &owner,
		&j.SubmittedAt, &completedAt, &artifactRef,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return j, fmt.Errorf("scanJob: %w", ErrJobNotFound)
	}
	if err != nil {
		return j, fmt.Errorf("scanJob: %w", err)
	}
	return hydrateJob(j, id, tc, provider, runner, sourceMode, snapshotRef, cmdArgs, artPaths, image, errMsg, owner, artifactRef, state, completedAt), nil
}

func scanJobRows(rows *sql.Rows) (apitypes.Job, error) {
	var j apitypes.Job
	var id, tc, provider, runner, sourceMode, snapshotRef, cmdArgs, artPaths, image, errMsg, owner, artifactRef string
	var state int
	var completedAt *time.Time

	err := rows.Scan(
		&id, &tc, &j.CacheKey, &state, &provider, &runner, &sourceMode, &snapshotRef,
		&cmdArgs, &artPaths, &image, &errMsg, &owner,
		&j.SubmittedAt, &completedAt, &artifactRef,
	)
	if err != nil {
		return j, err
	}
	return hydrateJob(j, id, tc, provider, runner, sourceMode, snapshotRef, cmdArgs, artPaths, image, errMsg, owner, artifactRef, state, completedAt), nil
}

func hydrateJob(j apitypes.Job, id, tc, provider, runner, sourceMode, snapshotRef, cmdArgs, artPaths, image, errMsg, owner, artifactRef string, state int, completedAt *time.Time) apitypes.Job {
	j.ID = apitypes.JobID(id)
	j.Toolchain = apitypes.Toolchain(tc)
	j.State = apitypes.JobState(state)
	j.Provider = apitypes.ProviderName(provider)
	j.Runner = apitypes.RunnerName(runner)
	j.SourceMode = apitypes.SourceMode(sourceMode)
	j.SnapshotRef = snapshotRef
	j.Image = image
	j.Error = errMsg
	j.Owner = owner
	j.ArtifactRef = apitypes.ArtifactRef(artifactRef)
	j.CompletedAt = completedAt
	_ = json.Unmarshal([]byte(cmdArgs), &j.CommandArgs)
	_ = json.Unmarshal([]byte(artPaths), &j.ArtifactPaths)
	return j
}
