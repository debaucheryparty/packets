package environment

import (
	"context"
	"errors"
	"fmt"
	"io"

	pb "github.com/debaucheryparty/packets/proto/v1"
	"google.golang.org/grpc"
)

type RemoteClient struct {
	client pb.EnvironmentClient
}

func NewRemoteClient(conn grpc.ClientConnInterface) *RemoteClient {
	return &RemoteClient{
		client: pb.NewEnvironmentClient(conn),
	}
}

func (r *RemoteClient) Check(ctx context.Context, projectID, projectRoot string, comps []Component) (*EnvironmentReport, error) {
	var pbComps []*pb.Component
	for _, c := range comps {
		pbComps = append(pbComps, &pb.Component{
			Type:       string(c.Type),
			Name:       c.Name,
			Path:       c.Path,
			Confidence: c.Confidence,
			Metadata:   c.Metadata,
		})
	}

	req := &pb.EnvCheckRequest{
		ProjectID:   projectID,
		ProjectRoot: projectRoot,
		Components:  pbComps,
	}

	resp, err := r.client.Check(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("remote env check: %w", err)
	}

	report := &EnvironmentReport{
		ProjectRoot: resp.ProjectRoot,
		AllReady:    resp.AllReady,
		Components:  make(map[ComponentType][]ToolchainRequirement),
	}

	for compTypeStr, reqsWrapper := range resp.Components {
		compType := ComponentType(compTypeStr)
		if reqsWrapper == nil {
			continue
		}
		for _, req := range reqsWrapper.Requirements {
			report.Components[compType] = append(report.Components[compType], ToolchainRequirement{
				Name:       req.Name,
				Version:    req.Version,
				Status:     CheckStatus(req.Status),
				Details:    req.Details,
				CanPrepare: req.CanPrepare,
				PrepareCmd: req.PrepareCmd,
			})
		}
	}

	return report, nil
}

func (r *RemoteClient) Prepare(ctx context.Context, projectID, projectRoot string, comps []Component, onProgress func(string)) error {
	var pbComps []*pb.Component
	for _, c := range comps {
		pbComps = append(pbComps, &pb.Component{
			Type:       string(c.Type),
			Name:       c.Name,
			Path:       c.Path,
			Confidence: c.Confidence,
			Metadata:   c.Metadata,
		})
	}

	req := &pb.PrepareRequest{
		ProjectID:   projectID,
		ProjectRoot: projectRoot,
		Components:  pbComps,
	}

	stream, err := r.client.Prepare(ctx, req)
	if err != nil {
		return fmt.Errorf("remote env prepare: %w", err)
	}

	for {
		line, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("remote env prepare stream: %w", err)
		}

		if onProgress != nil && line.Content != "" {
			onProgress(line.Content)
		}

		if line.Error {
			return fmt.Errorf("remote env prepare error: %s", line.Content)
		}

		if line.Done {
			break
		}
	}

	return nil
}
