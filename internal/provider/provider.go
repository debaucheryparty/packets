package provider

import (
	"context"
	"errors"
	"io"

	"github.com/waris4ly/packets/pkg/apitypes"
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
