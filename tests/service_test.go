package tests

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/debaucheryparty/packets/internal/service"
	"github.com/debaucheryparty/packets/internal/storage"
	pb "github.com/debaucheryparty/packets/proto/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func setupTestRemoteServiceServer(t *testing.T) (pb.RemoteServiceServiceClient, func()) {
	t.Helper()
	store, err := storage.NewJobStore(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	mgr := service.NewManager(store, nil)
	srv := service.NewServer(mgr)

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}

	grpcServer := grpc.NewServer()
	pb.RegisterRemoteServiceServiceServer(grpcServer, srv)

	go func() { _ = grpcServer.Serve(lis) }()

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	client := pb.NewRemoteServiceServiceClient(conn)
	cleanup := func() {
		_ = conn.Close()
		grpcServer.Stop()
		_ = lis.Close()
		_ = store.Close()
	}

	return client, cleanup
}

func TestRemoteService_GRPC(t *testing.T) {
	client, cleanup := setupTestRemoteServiceServer(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	subID := "sub-grpc-test-1"

	startResp, err := client.StartService(ctx, &pb.StartServiceRequest{
		SubspaceId: subID,
		Name:       "postgres",
		Image:      "postgres:17",
		Command:    []string{"echo", "postgres online"},
		Ports:      []string{"5432:5432"},
		Driver:     "process",
	})
	if err != nil {
		t.Fatalf("StartService failed: %v", err)
	}

	if startResp.Service.Name != "postgres" {
		t.Errorf("expected name postgres, got %s", startResp.Service.Name)
	}
	if startResp.Service.Status != "running" {
		t.Errorf("expected status running, got %s", startResp.Service.Status)
	}

	listResp, err := client.ListServices(ctx, &pb.ListServicesRequest{SubspaceId: subID})
	if err != nil {
		t.Fatalf("ListServices failed: %v", err)
	}
	if len(listResp.Services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(listResp.Services))
	}

	logsResp, err := client.GetServiceLogs(ctx, &pb.GetServiceLogsRequest{
		SubspaceId: subID,
		NameOrId:   "postgres",
		Lines:      10,
	})
	if err != nil {
		t.Fatalf("GetServiceLogs failed: %v", err)
	}
	if len(logsResp.Logs) == 0 {
		t.Errorf("expected non-empty logs")
	}

	stopResp, err := client.StopService(ctx, &pb.StopServiceRequest{
		SubspaceId: subID,
		NameOrId:   "postgres",
	})
	if err != nil {
		t.Fatalf("StopService failed: %v", err)
	}
	if stopResp.Service.Status != "stopped" {
		t.Errorf("expected status stopped, got %s", stopResp.Service.Status)
	}

	restartResp, err := client.RestartService(ctx, &pb.RestartServiceRequest{
		SubspaceId: subID,
		NameOrId:   "postgres",
	})
	if err != nil {
		t.Fatalf("RestartService failed: %v", err)
	}
	if restartResp.Service.Status != "running" {
		t.Errorf("expected status running, got %s", restartResp.Service.Status)
	}
}
