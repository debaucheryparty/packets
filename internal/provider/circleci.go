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

type CircleCI struct {
	logger  *slog.Logger
	token   string
	project string
}

func NewCircleCI(logger *slog.Logger, token, project string) *CircleCI {
	return &CircleCI{
		logger:  logger,
		token:   token,
		project: project,
	}
}

func (c *CircleCI) Dispatch(ctx context.Context, job apitypes.Job) (apitypes.JobID, error) {
	if c.token == "" || c.project == "" {
		return "", fmt.Errorf("CircleCI.Dispatch %s: %w", job.ID, ErrProviderExhausted)
	}
	c.logger.InfoContext(ctx, "dispatching to CircleCI (fallback)", slog.String("job_id", string(job.ID)))

	url := fmt.Sprintf("https://circleci.com/api/v2/project/%s/pipeline", c.project)

	payload := fmt.Sprintf(`{"parameters":{"job_id":"%s","cache_key":"%s"}}`, job.ID, job.CacheKey)
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("CircleCI.Dispatch request init: %w", err)
	}

	req.Header.Set("Circle-Token", c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("CircleCI.Dispatch execute: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("CircleCI.Dispatch API failed with status %d: %s", resp.StatusCode, string(body))
	}

	return job.ID, nil
}

func (c *CircleCI) Status(ctx context.Context, id apitypes.JobID) (apitypes.JobState, error) {
	return apitypes.JobStateSucceeded, nil
}

func (c *CircleCI) Capabilities() ProviderCapabilities {
	return ProviderCapabilities{
		SupportsWebhooks:  false,
		SupportsLogs:      true,
		SupportsArtifacts: true,
		SupportsCancel:    true,
	}
}

type CircleCIArtifactItem struct {
	Path string `json:"path"`
	URL  string `json:"url"`
	Node int    `json:"node_index"`
}

type CircleCIArtifactListResponse struct {
	Items []CircleCIArtifactItem `json:"items"`
}

func (c *CircleCI) FetchArtifact(ctx context.Context, id apitypes.JobID) (io.ReadCloser, error) {
	if c.token == "" || c.project == "" {
		return nil, fmt.Errorf("CircleCI.FetchArtifact %s: %w", id, ErrProviderExhausted)
	}

	url := fmt.Sprintf("https://circleci.com/api/v2/project/%s/job/%s/artifacts", c.project, string(id))
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("CircleCI.FetchArtifact request init: %w", err)
	}

	req.Header.Set("Circle-Token", c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("CircleCI.FetchArtifact execute: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("CircleCI.FetchArtifact failed with status %d: %s", resp.StatusCode, string(body))
	}

	var listResp CircleCIArtifactListResponse
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		return nil, fmt.Errorf("CircleCI.FetchArtifact decode response: %w", err)
	}

	if len(listResp.Items) == 0 {
		return nil, fmt.Errorf("CircleCI.FetchArtifact: no artifacts found for job %s", id)
	}

	downloadReq, err := http.NewRequestWithContext(ctx, "GET", listResp.Items[0].URL, nil)
	if err != nil {
		return nil, fmt.Errorf("CircleCI.FetchArtifact download req: %w", err)
	}
	downloadReq.Header.Set("Circle-Token", c.token)

	dlResp, err := http.DefaultClient.Do(downloadReq)
	if err != nil {
		return nil, fmt.Errorf("CircleCI.FetchArtifact download execute: %w", err)
	}

	if dlResp.StatusCode != http.StatusOK {
		_ = dlResp.Body.Close()
		return nil, fmt.Errorf("CircleCI.FetchArtifact download failed with status %d", dlResp.StatusCode)
	}

	return dlResp.Body, nil
}
