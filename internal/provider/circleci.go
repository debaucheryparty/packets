package provider

import (
	"context"
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
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("CircleCI.Dispatch API failed with status %d: %s", resp.StatusCode, string(body))
	}

	return job.ID, nil
}

func (c *CircleCI) Status(ctx context.Context, id apitypes.JobID) (apitypes.JobState, error) {
	return apitypes.JobStateSucceeded, nil
}

func (c *CircleCI) FetchArtifact(ctx context.Context, id apitypes.JobID) (io.ReadCloser, error) {
	return nil, fmt.Errorf("CircleCI.FetchArtifact not implemented")
}
