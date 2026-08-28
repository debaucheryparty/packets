package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/waris4ly/packets/pkg/apitypes"
)

type B2Storage struct {
	logger     *slog.Logger
	keyID      string
	appKey     string
	bucketName string
	httpClient *http.Client
	authToken  string
	apiURL     string
}

func NewB2Storage(logger *slog.Logger, keyID, appKey, bucketName string) *B2Storage {
	return &B2Storage{
		logger:     logger,
		keyID:      keyID,
		appKey:     appKey,
		bucketName: bucketName,
		httpClient: &http.Client{Timeout: 60 * time.Second},
	}
}

func (b *B2Storage) Authorize(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.backblazeb2.com/b2api/v2/b2_authorize_account", nil)
	if err != nil {
		return fmt.Errorf("B2Storage.Authorize request: %w", err)
	}
	req.SetBasicAuth(b.keyID, b.appKey)

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("B2Storage.Authorize: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("B2Storage.Authorize: status %d", resp.StatusCode)
	}

	var authResp struct {
		AuthorizationToken string `json:"authorizationToken"`
		APIURL             string `json:"apiUrl"`
		Allowed            struct {
			BucketID string `json:"bucketId"`
		} `json:"allowed"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		return fmt.Errorf("B2Storage.Authorize decode: %w", err)
	}

	b.authToken = authResp.AuthorizationToken
	b.apiURL = authResp.APIURL

	b.logger.InfoContext(ctx, "B2 authorized", slog.String("bucket", b.bucketName))
	return nil
}

func (b *B2Storage) Upload(ctx context.Context, ref apitypes.ArtifactRef, r io.Reader) error {
	if b.authToken == "" {
		if err := b.Authorize(ctx); err != nil {
			return fmt.Errorf("B2Storage.Upload auth: %w", err)
		}
	}

	b.logger.InfoContext(ctx, "B2 upload started",
		slog.String("ref", string(ref)),
		slog.String("bucket", b.bucketName),
	)

	return nil
}

func (b *B2Storage) Download(ctx context.Context, ref apitypes.ArtifactRef) (io.ReadCloser, error) {
	if b.authToken == "" {
		if err := b.Authorize(ctx); err != nil {
			return nil, fmt.Errorf("B2Storage.Download auth: %w", err)
		}
	}

	url := fmt.Sprintf("%s/file/%s/%s", b.apiURL, b.bucketName, string(ref))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("B2Storage.Download request: %w", err)
	}
	req.Header.Set("Authorization", b.authToken)

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("B2Storage.Download: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("B2Storage.Download: status %d", resp.StatusCode)
	}

	return resp.Body, nil
}
