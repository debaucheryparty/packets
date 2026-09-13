package provider

import (
	"context"
	"errors"
	"io"

	"github.com/debaucheryparty/packets/pkg/apitypes"
)

var (
	ErrProviderExhausted = errors.New("provider exhausted minutes or quota")
	ErrProviderFailed    = errors.New("provider failed to complete job")
)

type BuildProvider interface {
	Dispatch(ctx context.Context, job apitypes.Job) (apitypes.JobID, error)
	Status(ctx context.Context, id apitypes.JobID) (apitypes.JobState, error)
	FetchArtifact(ctx context.Context, id apitypes.JobID) (io.ReadCloser, error)
}

type ProviderCapabilities struct {
	SupportsWebhooks  bool `json:"supports_webhooks"`
	SupportsLogs      bool `json:"supports_logs"`
	SupportsArtifacts bool `json:"supports_artifacts"`
	SupportsCancel    bool `json:"supports_cancel"`
}

type CapableProvider interface {
	BuildProvider
	Capabilities() ProviderCapabilities
}
