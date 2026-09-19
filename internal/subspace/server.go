package subspace

import (
	"context"
	"time"

	"github.com/debaucheryparty/packets/pkg/apitypes"
	pb "github.com/debaucheryparty/packets/proto/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Server struct {
	pb.UnimplementedSubspaceServiceServer
	manager *Manager
}

func NewServer(manager *Manager) *Server {
	return &Server{
		manager: manager,
	}
}

func (s *Server) CreateSubspace(ctx context.Context, req *pb.CreateSubspaceRequest) (*pb.SubspaceResponse, error) {
	sub, err := s.manager.Create(ctx, CreateOptions{
		ProjectID:     req.ProjectId,
		OwnerID:       req.OwnerId,
		WorkerID:      req.WorkerId,
		WorkspaceID:   req.WorkspaceId,
		EnvironmentID: req.EnvironmentId,
		Metadata:      req.Metadata,
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "create subspace: %v", err)
	}
	return &pb.SubspaceResponse{Subspace: toProto(sub)}, nil
}

func (s *Server) GetSubspace(ctx context.Context, req *pb.GetSubspaceRequest) (*pb.SubspaceResponse, error) {
	sub, err := s.manager.Get(ctx, req.Id)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "get subspace %s: %v", req.Id, err)
	}
	return &pb.SubspaceResponse{Subspace: toProto(sub)}, nil
}

func (s *Server) ListSubspaces(ctx context.Context, req *pb.ListSubspacesRequest) (*pb.ListSubspacesResponse, error) {
	subs, err := s.manager.List(ctx, req.OwnerId, req.ProjectId)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list subspaces: %v", err)
	}

	records := make([]*pb.SubspaceRecord, 0, len(subs))
	for _, sub := range subs {
		records = append(records, toProto(sub))
	}
	return &pb.ListSubspacesResponse{Subspaces: records}, nil
}

func (s *Server) DestroySubspace(ctx context.Context, req *pb.DestroySubspaceRequest) (*pb.DestroySubspaceResponse, error) {
	if err := s.manager.Destroy(ctx, req.Id); err != nil {
		return nil, status.Errorf(codes.Internal, "destroy subspace %s: %v", req.Id, err)
	}
	return &pb.DestroySubspaceResponse{Success: true}, nil
}

func (s *Server) TouchSubspace(ctx context.Context, req *pb.TouchSubspaceRequest) (*pb.SubspaceResponse, error) {
	if err := s.manager.Touch(ctx, req.Id); err != nil {
		return nil, status.Errorf(codes.Internal, "touch subspace %s: %v", req.Id, err)
	}
	sub, err := s.manager.Get(ctx, req.Id)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "subspace not found: %v", err)
	}
	return &pb.SubspaceResponse{Subspace: toProto(sub)}, nil
}

func (s *Server) SleepSubspace(ctx context.Context, req *pb.SleepSubspaceRequest) (*pb.SubspaceResponse, error) {
	sub, err := s.manager.Sleep(ctx, req.Id)
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "sleep subspace %s: %v", req.Id, err)
	}
	return &pb.SubspaceResponse{Subspace: toProto(sub)}, nil
}

func (s *Server) WakeSubspace(ctx context.Context, req *pb.WakeSubspaceRequest) (*pb.SubspaceResponse, error) {
	sub, err := s.manager.Wake(ctx, req.Id)
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "wake subspace %s: %v", req.Id, err)
	}
	return &pb.SubspaceResponse{Subspace: toProto(sub)}, nil
}

func toProto(sub apitypes.Subspace) *pb.SubspaceRecord {
	return &pb.SubspaceRecord{
		Id:            sub.ID,
		ProjectId:     sub.ProjectID,
		OwnerId:       sub.OwnerID,
		WorkerId:      sub.WorkerID,
		WorkspaceId:   sub.WorkspaceID,
		EnvironmentId: sub.EnvironmentID,
		State:         string(sub.State),
		CreatedAt:     sub.CreatedAt.Format(time.RFC3339),
		LastUsedAt:    sub.LastUsedAt.Format(time.RFC3339),
		Metadata:      sub.Metadata,
	}
}
