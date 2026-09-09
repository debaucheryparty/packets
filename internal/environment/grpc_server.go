package environment

import (
	"context"

	pb "github.com/debaucheryparty/packets/proto/v1"
	"google.golang.org/grpc"
)

type Server struct {
	pb.UnimplementedEnvironmentServer
	mgr *Manager
}

func NewServer(mgr *Manager) *Server {
	if mgr == nil {
		mgr = NewManager()
	}
	return &Server{
		mgr: mgr,
	}
}

func (s *Server) Check(ctx context.Context, req *pb.EnvCheckRequest) (*pb.EnvCheckResponse, error) {
	var comps []Component
	for _, c := range req.Components {
		if c == nil {
			continue
		}
		comps = append(comps, Component{
			Type:       ComponentType(c.Type),
			Name:       c.Name,
			Path:       c.Path,
			Confidence: c.Confidence,
			Metadata:   c.Metadata,
		})
	}

	var report *EnvironmentReport
	var err error

	if len(comps) > 0 {
		report, err = s.mgr.CheckComponents(ctx, comps)
		if err != nil {
			return nil, err
		}
		report.ProjectRoot = req.ProjectRoot
	} else if req.ProjectRoot != "" {
		report, err = s.mgr.Check(ctx, req.ProjectRoot)
		if err != nil {
			return nil, err
		}
	} else {
		report = &EnvironmentReport{
			AllReady:   true,
			Components: make(map[ComponentType][]ToolchainRequirement),
		}
	}

	resp := &pb.EnvCheckResponse{
		ProjectRoot: report.ProjectRoot,
		AllReady:    report.AllReady,
		Components:  make(map[string]*pb.ComponentRequirements),
	}

	for compType, reqs := range report.Components {
		cr := &pb.ComponentRequirements{}
		for _, r := range reqs {
			cr.Requirements = append(cr.Requirements, &pb.ToolchainRequirement{
				Name:       r.Name,
				Version:    r.Version,
				Status:     string(r.Status),
				Details:    r.Details,
				CanPrepare: r.CanPrepare,
				PrepareCmd: r.PrepareCmd,
			})
		}
		resp.Components[string(compType)] = cr
	}

	return resp, nil
}

func (s *Server) Prepare(req *pb.PrepareRequest, stream grpc.ServerStreamingServer[pb.PrepareProgressLine]) error {
	var comps []Component
	for _, c := range req.Components {
		if c == nil {
			continue
		}
		comps = append(comps, Component{
			Type:       ComponentType(c.Type),
			Name:       c.Name,
			Path:       c.Path,
			Confidence: c.Confidence,
			Metadata:   c.Metadata,
		})
	}

	onProgress := func(msg string) {
		_ = stream.Send(&pb.PrepareProgressLine{
			Content: msg,
		})
	}

	var err error
	if len(comps) > 0 {
		err = s.mgr.PrepareComponents(stream.Context(), comps, onProgress)
	} else if req.ProjectRoot != "" {
		err = s.mgr.Prepare(stream.Context(), req.ProjectRoot, onProgress)
	} else {
		onProgress("No components or project root specified to prepare.")
		return nil
	}

	if err != nil {
		_ = stream.Send(&pb.PrepareProgressLine{
			Content: err.Error(),
			Error:   true,
			Done:    true,
		})
		return err
	}

	_ = stream.Send(&pb.PrepareProgressLine{
		Content: "Environment preparation completed successfully.",
		Done:    true,
	})
	return nil
}
