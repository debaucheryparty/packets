package service

import (
	"context"
	"time"

	"github.com/debaucheryparty/packets/pkg/apitypes"
	pb "github.com/debaucheryparty/packets/proto/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Server struct {
	pb.UnimplementedRemoteServiceServiceServer
	manager *Manager
}

func NewServer(manager *Manager) *Server {
	return &Server{
		manager: manager,
	}
}

func (s *Server) StartService(ctx context.Context, req *pb.StartServiceRequest) (*pb.ServiceResponse, error) {
	svc := apitypes.Service{
		Name:        req.Name,
		Image:       req.Image,
		Command:     req.Command,
		Environment: req.Environment,
		Ports:       req.Ports,
		Driver:      req.Driver,
	}

	started, err := s.manager.Start(ctx, req.SubspaceId, svc)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "start service %s: %v", req.Name, err)
	}

	return &pb.ServiceResponse{Service: toProto(started)}, nil
}

func (s *Server) StopService(ctx context.Context, req *pb.StopServiceRequest) (*pb.ServiceResponse, error) {
	stopped, err := s.manager.Stop(ctx, req.SubspaceId, req.NameOrId)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "stop service %s: %v", req.NameOrId, err)
	}

	return &pb.ServiceResponse{Service: toProto(stopped)}, nil
}

func (s *Server) RestartService(ctx context.Context, req *pb.RestartServiceRequest) (*pb.ServiceResponse, error) {
	restarted, err := s.manager.Restart(ctx, req.SubspaceId, req.NameOrId)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "restart service %s: %v", req.NameOrId, err)
	}

	return &pb.ServiceResponse{Service: toProto(restarted)}, nil
}

func (s *Server) ListServices(ctx context.Context, req *pb.ListServicesRequest) (*pb.ListServicesResponse, error) {
	services, err := s.manager.List(ctx, req.SubspaceId)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list services: %v", err)
	}

	records := make([]*pb.ServiceRecord, 0, len(services))
	for _, svc := range services {
		records = append(records, toProto(svc))
	}

	return &pb.ListServicesResponse{Services: records}, nil
}

func (s *Server) GetServiceLogs(ctx context.Context, req *pb.GetServiceLogsRequest) (*pb.GetServiceLogsResponse, error) {
	logs, err := s.manager.Logs(ctx, req.SubspaceId, req.NameOrId, int(req.Lines))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "get service logs %s: %v", req.NameOrId, err)
	}

	return &pb.GetServiceLogsResponse{Logs: logs}, nil
}

func toProto(svc apitypes.Service) *pb.ServiceRecord {
	return &pb.ServiceRecord{
		Id:          svc.ID,
		SubspaceId:  svc.SubspaceID,
		Name:        svc.Name,
		Image:       svc.Image,
		Command:     svc.Command,
		Environment: svc.Environment,
		Ports:       svc.Ports,
		Status:      string(svc.Status),
		Driver:      svc.Driver,
		ContainerId: svc.ContainerID,
		CreatedAt:   svc.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:   svc.UpdatedAt.UTC().Format(time.RFC3339),
	}
}
