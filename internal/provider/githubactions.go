package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/debaucheryparty/packets/pkg/apitypes"
)

type GitHubActions struct {
	logger *slog.Logger
	token  string
	repo   string
}

func NewGitHubActions(logger *slog.Logger, token, repo string) *GitHubActions {
	return &GitHubActions{
		logger: logger,
		token:  token,
		repo:   repo,
	}
}

func (g *GitHubActions) Dispatch(ctx context.Context, job apitypes.Job) (apitypes.JobID, error) {
	if g.token == "" || g.repo == "" {
		return "", fmt.Errorf("GitHubActions.Dispatch %s: %w", job.ID, ErrProviderExhausted)
	}

	g.logger.InfoContext(ctx, "dispatching to GitHub Actions", slog.String("job_id", string(job.ID)))

	url := fmt.Sprintf("https://api.github.com/repos/%s/actions/workflows/build.yml/dispatches", g.repo)

	inputs := map[string]string{
		"job_id":       string(job.ID),
		"cache_key":    job.CacheKey,
		"source_mode":  string(job.SourceMode),
		"snapshot_ref": job.SnapshotRef,
		"toolchain":    string(job.Toolchain),
		"command":      strings.Join(job.CommandArgs, " "),
	}

	payloadMap := map[string]any{
		"ref":    "main",
		"inputs": inputs,
	}
	payloadBytes, err := json.Marshal(payloadMap)
	if err != nil {
		return "", fmt.Errorf("marshal dispatch payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(payloadBytes)))
	if err != nil {
		return "", fmt.Errorf("GitHubActions.Dispatch request init: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+g.token)
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("GitHubActions.Dispatch execute: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("GitHubActions.Dispatch API failed with status %d: %s", resp.StatusCode, string(body))
	}

	return job.ID, nil
}

type ghWorkflowRunsResponse struct {
	WorkflowRuns []struct {
		ID         int64  `json:"id"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		HeadBranch string `json:"head_branch"`
	} `json:"workflow_runs"`
}

func (g *GitHubActions) Status(ctx context.Context, id apitypes.JobID) (apitypes.JobState, error) {
	if g.token == "" || g.repo == "" {
		return apitypes.JobStateFailed, fmt.Errorf("credentials missing")
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/actions/runs?per_page=5", g.repo)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return apitypes.JobStateFailed, err
	}

	req.Header.Set("Authorization", "Bearer "+g.token)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return apitypes.JobStateFailed, err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return apitypes.JobStateFailed, fmt.Errorf("status check failed with code: %d", resp.StatusCode)
	}

	var data ghWorkflowRunsResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return apitypes.JobStateFailed, err
	}

	if len(data.WorkflowRuns) == 0 {
		return apitypes.JobStateRunning, nil
	}

	latest := data.WorkflowRuns[0]
	switch latest.Status {
	case "completed":
		if latest.Conclusion == "success" {
			return apitypes.JobStateSucceeded, nil
		}
		return apitypes.JobStateFailed, nil
	case "in_progress", "queued", "waiting":
		return apitypes.JobStateRunning, nil
	default:
		return apitypes.JobStateRunning, nil
	}
}

type ghArtifactsResponse struct {
	Artifacts []struct {
		ID                 int64  `json:"id"`
		Name               string `json:"name"`
		ArchiveDownloadURL string `json:"archive_download_url"`
		Expired            bool   `json:"expired"`
	} `json:"artifacts"`
}

func (g *GitHubActions) FetchArtifact(ctx context.Context, id apitypes.JobID) (io.ReadCloser, error) {
	if g.token == "" || g.repo == "" {
		return nil, fmt.Errorf("credentials missing for GitHub artifact download")
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/actions/artifacts?per_page=10", g.repo)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+g.token)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch artifacts list: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list artifacts failed with status: %d", resp.StatusCode)
	}

	var data ghArtifactsResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("decode artifacts list: %w", err)
	}

	if len(data.Artifacts) == 0 {
		return nil, fmt.Errorf("no artifacts found for repo %s", g.repo)
	}

	downloadURL := data.Artifacts[0].ArchiveDownloadURL
	dlReq, err := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
	if err != nil {
		return nil, err
	}
	dlReq.Header.Set("Authorization", "Bearer "+g.token)
	dlReq.Header.Set("Accept", "application/vnd.github.v3+json")

	dlResp, err := http.DefaultClient.Do(dlReq)
	if err != nil {
		return nil, fmt.Errorf("download artifact zip: %w", err)
	}

	if dlResp.StatusCode != http.StatusOK {
		dlResp.Body.Close() //nolint:errcheck
		return nil, fmt.Errorf("download artifact failed with status: %d", dlResp.StatusCode)
	}

	return dlResp.Body, nil
}
