package storage

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/debaucheryparty/packets/internal/policy"
	"github.com/debaucheryparty/packets/pkg/apitypes"
	_ "modernc.org/sqlite"
)

//go:embed migrations/001_initial.sql
var migration001 string

//go:embed migrations/002_execution_spec.sql
var migration002 string

//go:embed migrations/003_project_id.sql
var migration003 string

//go:embed migrations/004_approval_storage.sql
var migration004 string

//go:embed migrations/005_subspaces.sql
var migration005 string

//go:embed migrations/006_services.sql
var migration006 string

//go:embed migrations/007_transactions.sql
var migration007 string

var (
	ErrJobNotFound         = errors.New("job not found")
	ErrCacheMiss           = errors.New("cache miss")
	ErrSubspaceNotFound    = errors.New("subspace not found")
	ErrServiceNotFound     = errors.New("service not found")
	ErrTransactionNotFound = errors.New("transaction not found")
)

type JobStore struct {
	db      *sql.DB
	writeMu sync.Mutex
}

func NewJobStore(ctx context.Context, dbPath string) (*JobStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("NewJobStore open %q: %w", dbPath, err)
	}

	if dbPath == ":memory:" || strings.Contains(dbPath, "mode=memory") {
		db.SetMaxOpenConns(1)
	} else {
		db.SetMaxOpenConns(25)
		db.SetMaxIdleConns(10)
	}

	_, _ = db.ExecContext(ctx, "PRAGMA journal_mode = WAL;")
	_, _ = db.ExecContext(ctx, "PRAGMA busy_timeout = 5000;")
	_, _ = db.ExecContext(ctx, "PRAGMA synchronous = NORMAL;")

	if _, err := db.ExecContext(ctx, migration001); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("NewJobStore initial migration: %w", err)
	}

	if err := runMigrations(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("NewJobStore migrations: %w", err)
	}

	return &JobStore{db: db}, nil
}

func (s *JobStore) Close() error {
	return s.db.Close()
}

func (s *JobStore) CreateJob(ctx context.Context, job apitypes.Job) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	cmdArgs, _ := json.Marshal(job.CommandArgs)
	artifactPaths, _ := json.Marshal(job.ArtifactPaths)

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO jobs
		 (id, project_id, toolchain, cache_key, state, provider, runner, source_mode, snapshot_ref,
		  command_args, artifact_paths, image, error, owner, submitted_at, artifact_ref)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		string(job.ID), job.ProjectID, string(job.Toolchain), job.CacheKey,
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
		`SELECT id, project_id, toolchain, cache_key, state, provider, runner, source_mode, snapshot_ref,
		        command_args, artifact_paths, image, error, owner,
		        submitted_at, completed_at, artifact_ref
		 FROM jobs WHERE id = ?`, string(id),
	)
	return scanJob(row)
}

func (s *JobStore) UpdateJobState(ctx context.Context, id apitypes.JobID, state apitypes.JobState) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

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
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("CompleteJob begin: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE jobs SET state = ?, completed_at = ?, artifact_ref = ? WHERE id = ?`,
		int(apitypes.JobStateSucceeded), now, string(ref), string(id),
	); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("CompleteJob update job: %w", err)
	}
	if cacheKey != "" && ref != "" {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR REPLACE INTO cache_entries (cache_key, artifact_ref, created_at) VALUES (?, ?, ?)`,
			cacheKey, string(ref), now,
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("CompleteJob write cache: %w", err)
		}
	}
	return tx.Commit()
}

func (s *JobStore) FailJob(ctx context.Context, id apitypes.JobID, errMsg string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

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
		`SELECT id, project_id, toolchain, cache_key, state, provider, runner, source_mode, snapshot_ref,
		        command_args, artifact_paths, image, error, owner,
		        submitted_at, completed_at, artifact_ref
		 FROM jobs WHERE state IN (`+string(placeholders)+`)
		 ORDER BY submitted_at ASC`, args...,
	)
	if err != nil {
		return nil, fmt.Errorf("ListJobsByState: %w", err)
	}
	defer func() { _ = rows.Close() }()

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
		`SELECT id, project_id, toolchain, cache_key, state, provider, runner, source_mode, snapshot_ref,
		        command_args, artifact_paths, image, error, owner,
		        submitted_at, completed_at, artifact_ref
		 FROM jobs ORDER BY submitted_at DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("ListRecentJobs: %w", err)
	}
	defer func() { _ = rows.Close() }()

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

func (s *JobStore) GetLatestProjectArtifactJob(ctx context.Context, projectID string) (apitypes.Job, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, project_id, toolchain, cache_key, state, provider, runner, source_mode, snapshot_ref,
		        command_args, artifact_paths, image, error, owner,
		        submitted_at, completed_at, artifact_ref
		 FROM jobs WHERE project_id = ? AND state = ? AND artifact_ref != ''
		 ORDER BY submitted_at DESC LIMIT 1`, projectID, int(apitypes.JobStateSucceeded),
	)
	return scanJob(row)
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
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

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
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

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
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

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
	var id, projectID, tc, provider, runner, sourceMode, snapshotRef, cmdArgs, artPaths, image, errMsg, owner, artifactRef string
	var state int
	var completedAt *time.Time

	err := row.Scan(
		&id, &projectID, &tc, &j.CacheKey, &state, &provider, &runner, &sourceMode, &snapshotRef,
		&cmdArgs, &artPaths, &image, &errMsg, &owner,
		&j.SubmittedAt, &completedAt, &artifactRef,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return j, fmt.Errorf("scanJob: %w", ErrJobNotFound)
	}
	if err != nil {
		return j, fmt.Errorf("scanJob: %w", err)
	}
	return hydrateJob(j, id, projectID, tc, provider, runner, sourceMode, snapshotRef, cmdArgs, artPaths, image, errMsg, owner, artifactRef, state, completedAt), nil
}

func scanJobRows(rows *sql.Rows) (apitypes.Job, error) {
	var j apitypes.Job
	var id, projectID, tc, provider, runner, sourceMode, snapshotRef, cmdArgs, artPaths, image, errMsg, owner, artifactRef string
	var state int
	var completedAt *time.Time

	err := rows.Scan(
		&id, &projectID, &tc, &j.CacheKey, &state, &provider, &runner, &sourceMode, &snapshotRef,
		&cmdArgs, &artPaths, &image, &errMsg, &owner,
		&j.SubmittedAt, &completedAt, &artifactRef,
	)
	if err != nil {
		return j, err
	}
	return hydrateJob(j, id, projectID, tc, provider, runner, sourceMode, snapshotRef, cmdArgs, artPaths, image, errMsg, owner, artifactRef, state, completedAt), nil
}

func hydrateJob(j apitypes.Job, id, projectID, tc, provider, runner, sourceMode, snapshotRef, cmdArgs, artPaths, image, errMsg, owner, artifactRef string, state int, completedAt *time.Time) apitypes.Job {
	j.ID = apitypes.JobID(id)
	j.ProjectID = projectID
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

func (s *JobStore) SavePending(ctx context.Context, pa *policy.PendingApproval) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	argsJSON, _ := json.Marshal(pa.Context.Args)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO pending_approvals (id, user, project_id, workspace_id, snapshot_hash, command, args, action, created_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		pa.ID, pa.Context.User, pa.Context.ProjectID, pa.Context.WorkspaceID, pa.Context.SnapshotHash,
		pa.Context.Command, string(argsJSON), pa.Context.Action, pa.CreatedAt.UTC(), pa.ExpiresAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("SavePending %s: %w", pa.ID, err)
	}
	return nil
}

func (s *JobStore) GetPending(ctx context.Context, id string) (*policy.PendingApproval, error) {
	var pa policy.PendingApproval
	var argsJSON string
	row := s.db.QueryRowContext(ctx,
		`SELECT id, user, project_id, workspace_id, snapshot_hash, command, args, action, created_at, expires_at
		 FROM pending_approvals WHERE id = ?`, id,
	)
	err := row.Scan(
		&pa.ID, &pa.Context.User, &pa.Context.ProjectID, &pa.Context.WorkspaceID, &pa.Context.SnapshotHash,
		&pa.Context.Command, &argsJSON, &pa.Context.Action, &pa.CreatedAt, &pa.ExpiresAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, policy.ErrPendingNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("GetPending %s: %w", id, err)
	}
	_ = json.Unmarshal([]byte(argsJSON), &pa.Context.Args)
	if time.Now().UTC().After(pa.ExpiresAt) {
		return nil, policy.ErrPendingExpired
	}
	return &pa, nil
}

func (s *JobStore) DeletePending(ctx context.Context, id string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	_, err := s.db.ExecContext(ctx, `DELETE FROM pending_approvals WHERE id = ?`, id)
	return err
}

func (s *JobStore) SaveTicket(ctx context.Context, ticket *policy.ApprovalTicket) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	used := 0
	if ticket.Used {
		used = 1
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO approval_tickets (id, request_hash, created_at, expires_at, used)
		 VALUES (?, ?, ?, ?, ?)`,
		ticket.ID, ticket.RequestHash, ticket.CreatedAt.UTC(), ticket.ExpiresAt.UTC(), used,
	)
	if err != nil {
		return fmt.Errorf("SaveTicket %s: %w", ticket.ID, err)
	}
	return nil
}

func (s *JobStore) GetTicket(ctx context.Context, id string) (*policy.ApprovalTicket, error) {
	var ticket policy.ApprovalTicket
	var used int
	row := s.db.QueryRowContext(ctx,
		`SELECT id, request_hash, created_at, expires_at, used
		 FROM approval_tickets WHERE id = ?`, id,
	)
	err := row.Scan(&ticket.ID, &ticket.RequestHash, &ticket.CreatedAt, &ticket.ExpiresAt, &used)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, policy.ErrInvalidApprovalTicket
	}
	if err != nil {
		return nil, fmt.Errorf("GetTicket %s: %w", id, err)
	}
	ticket.Used = (used != 0)
	return &ticket, nil
}

func (s *JobStore) ConsumeTicket(ctx context.Context, id string, reqHash string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx,
		`UPDATE approval_tickets SET used = 1 WHERE id = ? AND used = 0 AND request_hash = ? AND expires_at > ?`,
		id, reqHash, now,
	)
	if err != nil {
		return fmt.Errorf("ConsumeTicket %s: %w", id, err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("ConsumeTicket rows affected %s: %w", id, err)
	}
	if affected > 0 {
		return nil
	}

	var storedHash string
	var expiresAt time.Time
	var used int
	row := s.db.QueryRowContext(ctx, `SELECT request_hash, expires_at, used FROM approval_tickets WHERE id = ?`, id)
	if err := row.Scan(&storedHash, &expiresAt, &used); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return policy.ErrInvalidApprovalTicket
		}
		return err
	}

	if used != 0 {
		return policy.ErrTicketAlreadyUsed
	}
	if now.After(expiresAt) {
		return policy.ErrTicketExpired
	}
	if storedHash != reqHash {
		return policy.ErrApprovalMismatch
	}

	return policy.ErrInvalidApprovalTicket
}

func (s *JobStore) SaveSessionApproval(ctx context.Context, key string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO session_approvals (session_key, created_at) VALUES (?, ?)`,
		key, time.Now().UTC(),
	)
	return err
}

func (s *JobStore) HasSessionApproval(ctx context.Context, key string) (bool, error) {
	var count int
	row := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM session_approvals WHERE session_key = ?`, key)
	if err := row.Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func (s *JobStore) ExpireTicketForTest(ctx context.Context, id string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	_, err := s.db.ExecContext(ctx, `UPDATE approval_tickets SET expires_at = ? WHERE id = ?`, time.Now().UTC().Add(-1*time.Hour), id)
	return err
}

func (s *JobStore) CreateSubspace(ctx context.Context, sub apitypes.Subspace) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	metaJSON, _ := json.Marshal(sub.Metadata)
	if sub.Metadata == nil {
		metaJSON = []byte("{}")
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO subspaces (id, project_id, owner_id, worker_id, workspace_id, environment_id, state, created_at, last_used_at, metadata)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sub.ID, sub.ProjectID, sub.OwnerID, sub.WorkerID, sub.WorkspaceID, sub.EnvironmentID,
		string(sub.State), sub.CreatedAt.UTC(), sub.LastUsedAt.UTC(), string(metaJSON),
	)
	if err != nil {
		return fmt.Errorf("CreateSubspace %s: %w", sub.ID, err)
	}
	return nil
}

func (s *JobStore) GetSubspace(ctx context.Context, id string) (apitypes.Subspace, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, project_id, owner_id, worker_id, workspace_id, environment_id, state, created_at, last_used_at, metadata
		 FROM subspaces WHERE id = ?`, id,
	)
	var sub apitypes.Subspace
	var stateStr, metaJSON string
	err := row.Scan(
		&sub.ID, &sub.ProjectID, &sub.OwnerID, &sub.WorkerID, &sub.WorkspaceID, &sub.EnvironmentID,
		&stateStr, &sub.CreatedAt, &sub.LastUsedAt, &metaJSON,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return apitypes.Subspace{}, ErrSubspaceNotFound
	}
	if err != nil {
		return apitypes.Subspace{}, fmt.Errorf("GetSubspace %s: %w", id, err)
	}
	sub.State = apitypes.SubspaceState(stateStr)
	if metaJSON != "" {
		_ = json.Unmarshal([]byte(metaJSON), &sub.Metadata)
	}
	if sub.Metadata == nil {
		sub.Metadata = make(map[string]string)
	}
	return sub, nil
}

func (s *JobStore) ListSubspaces(ctx context.Context, ownerID, projectID string) ([]apitypes.Subspace, error) {
	query := `SELECT id, project_id, owner_id, worker_id, workspace_id, environment_id, state, created_at, last_used_at, metadata FROM subspaces WHERE 1=1`
	var args []interface{}
	if ownerID != "" {
		query += ` AND owner_id = ?`
		args = append(args, ownerID)
	}
	if projectID != "" {
		query += ` AND project_id = ?`
		args = append(args, projectID)
	}
	query += ` ORDER BY last_used_at DESC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("ListSubspaces: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []apitypes.Subspace
	for rows.Next() {
		var sub apitypes.Subspace
		var stateStr, metaJSON string
		if err := rows.Scan(
			&sub.ID, &sub.ProjectID, &sub.OwnerID, &sub.WorkerID, &sub.WorkspaceID, &sub.EnvironmentID,
			&stateStr, &sub.CreatedAt, &sub.LastUsedAt, &metaJSON,
		); err != nil {
			return nil, fmt.Errorf("ListSubspaces scan: %w", err)
		}
		sub.State = apitypes.SubspaceState(stateStr)
		if metaJSON != "" {
			_ = json.Unmarshal([]byte(metaJSON), &sub.Metadata)
		}
		if sub.Metadata == nil {
			sub.Metadata = make(map[string]string)
		}
		results = append(results, sub)
	}
	return results, rows.Err()
}

func (s *JobStore) UpdateSubspaceState(ctx context.Context, id string, state apitypes.SubspaceState) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx,
		`UPDATE subspaces SET state = ?, last_used_at = ? WHERE id = ?`,
		string(state), now, id,
	)
	if err != nil {
		return fmt.Errorf("UpdateSubspaceState %s: %w", id, err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrSubspaceNotFound
	}
	return nil
}

func (s *JobStore) TouchSubspace(ctx context.Context, id string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx,
		`UPDATE subspaces SET last_used_at = ? WHERE id = ?`,
		now, id,
	)
	if err != nil {
		return fmt.Errorf("TouchSubspace %s: %w", id, err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrSubspaceNotFound
	}
	return nil
}

func (s *JobStore) DeleteSubspace(ctx context.Context, id string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	res, err := s.db.ExecContext(ctx, `DELETE FROM subspaces WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("DeleteSubspace %s: %w", id, err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrSubspaceNotFound
	}
	return nil
}

func (s *JobStore) CreateService(ctx context.Context, svc apitypes.Service) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	cmdJSON, _ := json.Marshal(svc.Command)
	if svc.Command == nil {
		cmdJSON = []byte("[]")
	}
	envJSON, _ := json.Marshal(svc.Environment)
	if svc.Environment == nil {
		envJSON = []byte("{}")
	}
	portsJSON, _ := json.Marshal(svc.Ports)
	if svc.Ports == nil {
		portsJSON = []byte("[]")
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO subspace_services (id, subspace_id, name, image, command, environment, ports, status, driver, container_id, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		svc.ID, svc.SubspaceID, svc.Name, svc.Image, string(cmdJSON), string(envJSON), string(portsJSON),
		string(svc.Status), svc.Driver, svc.ContainerID, svc.CreatedAt.UTC(), svc.UpdatedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("CreateService %s: %w", svc.ID, err)
	}
	return nil
}

func (s *JobStore) GetService(ctx context.Context, subspaceID, nameOrID string) (apitypes.Service, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, subspace_id, name, image, command, environment, ports, status, driver, container_id, created_at, updated_at
		 FROM subspace_services WHERE subspace_id = ? AND (id = ? OR name = ?)`,
		subspaceID, nameOrID, nameOrID,
	)
	var svc apitypes.Service
	var cmdJSON, envJSON, portsJSON, statusStr string
	err := row.Scan(
		&svc.ID, &svc.SubspaceID, &svc.Name, &svc.Image, &cmdJSON, &envJSON, &portsJSON,
		&statusStr, &svc.Driver, &svc.ContainerID, &svc.CreatedAt, &svc.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return apitypes.Service{}, ErrServiceNotFound
	}
	if err != nil {
		return apitypes.Service{}, fmt.Errorf("GetService %s: %w", nameOrID, err)
	}
	svc.Status = apitypes.ServiceStatus(statusStr)
	_ = json.Unmarshal([]byte(cmdJSON), &svc.Command)
	_ = json.Unmarshal([]byte(envJSON), &svc.Environment)
	_ = json.Unmarshal([]byte(portsJSON), &svc.Ports)
	return svc, nil
}

func (s *JobStore) ListServices(ctx context.Context, subspaceID string) ([]apitypes.Service, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, subspace_id, name, image, command, environment, ports, status, driver, container_id, created_at, updated_at
		 FROM subspace_services WHERE subspace_id = ? ORDER BY created_at ASC`,
		subspaceID,
	)
	if err != nil {
		return nil, fmt.Errorf("ListServices %s: %w", subspaceID, err)
	}
	defer func() { _ = rows.Close() }()

	var results []apitypes.Service
	for rows.Next() {
		var svc apitypes.Service
		var cmdJSON, envJSON, portsJSON, statusStr string
		if err := rows.Scan(
			&svc.ID, &svc.SubspaceID, &svc.Name, &svc.Image, &cmdJSON, &envJSON, &portsJSON,
			&statusStr, &svc.Driver, &svc.ContainerID, &svc.CreatedAt, &svc.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("ListServices scan: %w", err)
		}
		svc.Status = apitypes.ServiceStatus(statusStr)
		_ = json.Unmarshal([]byte(cmdJSON), &svc.Command)
		_ = json.Unmarshal([]byte(envJSON), &svc.Environment)
		_ = json.Unmarshal([]byte(portsJSON), &svc.Ports)
		results = append(results, svc)
	}
	return results, rows.Err()
}

func (s *JobStore) UpdateServiceStatus(ctx context.Context, id string, status apitypes.ServiceStatus, containerID string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx,
		`UPDATE subspace_services SET status = ?, container_id = ?, updated_at = ? WHERE id = ?`,
		string(status), containerID, now, id,
	)
	if err != nil {
		return fmt.Errorf("UpdateServiceStatus %s: %w", id, err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrServiceNotFound
	}
	return nil
}

func (s *JobStore) DeleteService(ctx context.Context, subspaceID, nameOrID string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	res, err := s.db.ExecContext(ctx,
		`DELETE FROM subspace_services WHERE subspace_id = ? AND (id = ? OR name = ?)`,
		subspaceID, nameOrID, nameOrID,
	)
	if err != nil {
		return fmt.Errorf("DeleteService %s: %w", nameOrID, err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrServiceNotFound
	}
	return nil
}

func (s *JobStore) CreateTransaction(ctx context.Context, tx apitypes.Transaction) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO workspace_transactions (id, project_id, subspace_id, base_snapshot_ref, working_snapshot_ref, status, description, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		tx.ID, tx.ProjectID, tx.SubspaceID, tx.BaseSnapshotRef, tx.WorkingSnapshotRef,
		string(tx.Status), tx.Description, tx.CreatedAt.UTC(), tx.UpdatedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("CreateTransaction %s: %w", tx.ID, err)
	}
	return nil
}

func (s *JobStore) GetTransaction(ctx context.Context, id string) (apitypes.Transaction, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, project_id, subspace_id, base_snapshot_ref, working_snapshot_ref, status, description, created_at, updated_at
		 FROM workspace_transactions WHERE id = ?`, id,
	)
	var tx apitypes.Transaction
	var statusStr string
	err := row.Scan(
		&tx.ID, &tx.ProjectID, &tx.SubspaceID, &tx.BaseSnapshotRef, &tx.WorkingSnapshotRef,
		&statusStr, &tx.Description, &tx.CreatedAt, &tx.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return apitypes.Transaction{}, ErrTransactionNotFound
	}
	if err != nil {
		return apitypes.Transaction{}, fmt.Errorf("GetTransaction %s: %w", id, err)
	}
	tx.Status = apitypes.TransactionStatus(statusStr)
	return tx, nil
}

func (s *JobStore) ListTransactions(ctx context.Context, projectID string) ([]apitypes.Transaction, error) {
	query := `SELECT id, project_id, subspace_id, base_snapshot_ref, working_snapshot_ref, status, description, created_at, updated_at FROM workspace_transactions WHERE 1=1`
	var args []interface{}
	if projectID != "" {
		query += ` AND project_id = ?`
		args = append(args, projectID)
	}
	query += ` ORDER BY created_at DESC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("ListTransactions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []apitypes.Transaction
	for rows.Next() {
		var tx apitypes.Transaction
		var statusStr string
		if err := rows.Scan(
			&tx.ID, &tx.ProjectID, &tx.SubspaceID, &tx.BaseSnapshotRef, &tx.WorkingSnapshotRef,
			&statusStr, &tx.Description, &tx.CreatedAt, &tx.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("ListTransactions scan: %w", err)
		}
		tx.Status = apitypes.TransactionStatus(statusStr)
		results = append(results, tx)
	}
	return results, rows.Err()
}

func (s *JobStore) UpdateTransaction(ctx context.Context, id string, status apitypes.TransactionStatus, workingSnapshotRef string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx,
		`UPDATE workspace_transactions SET status = ?, working_snapshot_ref = ?, updated_at = ? WHERE id = ?`,
		string(status), workingSnapshotRef, now, id,
	)
	if err != nil {
		return fmt.Errorf("UpdateTransaction %s: %w", id, err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrTransactionNotFound
	}
	return nil
}
