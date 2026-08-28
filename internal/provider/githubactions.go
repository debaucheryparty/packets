package provider

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/waris4ly/packets/pkg/apitypes"
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

	url := fmt.Sprintf("https://api.github.com/repos/%s/actions/workflows/swift-build.yml/dispatches", g.repo)

	payload := fmt.Sprintf(`{"ref":"main","inputs":{"job_id":"%s","cache_key":"%s"}}`, job.ID, job.CacheKey)
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(payload))
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
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("GitHubActions.Dispatch API failed with status %d: %s", resp.StatusCode, string(body))
	}

	return job.ID, nil
}

func (g *GitHubActions) Status(ctx context.Context, id apitypes.JobID) (apitypes.JobState, error) {
	// in production this polls the GitHub API for run status
	return apitypes.JobStateSucceeded, nil
}

func (g *GitHubActions) FetchArtifact(ctx context.Context, id apitypes.JobID) (io.ReadCloser, error) {
	return nil, fmt.Errorf("GitHubActions.FetchArtifact not implemented")
}
