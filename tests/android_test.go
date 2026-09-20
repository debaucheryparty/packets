package tests

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/debaucheryparty/packets/internal/android"
	"github.com/debaucheryparty/packets/internal/storage"
	"github.com/debaucheryparty/packets/internal/subspace"
	"github.com/debaucheryparty/packets/pkg/apitypes"
	pb "github.com/debaucheryparty/packets/proto/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestAndroid_ClassifyDevice(t *testing.T) {
	tests := []struct {
		serial     string
		state      string
		wantType   android.DeviceType
		wantStatus string
	}{
		{"Pixel-remote", "device", android.DeviceTypeNetwork, "ready"},
		{"192.168.1.55:5555", "device", android.DeviceTypeNetwork, "ready"},
		{"remote-device-1", "offline", android.DeviceTypeNetwork, "offline"},
		{"emulator-5554", "device", android.DeviceTypeEmulator, "ready"},
		{"emulator-01", "device", android.DeviceTypeEmulator, "ready"},
		{"localhost:5555", "device", android.DeviceTypeEmulator, "ready"},
		{"127.0.0.1:5555", "device", android.DeviceTypeEmulator, "ready"},
		{"HT7491A00123", "device", android.DeviceTypeUSB, "ready"},
		{"HT7491A00123", "unauthorized", android.DeviceTypeUSB, "unauthorized"},
	}

	for _, tt := range tests {
		dev := android.ClassifyDevice(tt.serial, tt.state)
		if dev.Type != tt.wantType {
			t.Errorf("ClassifyDevice(%q, %q).Type = %q, want %q", tt.serial, tt.state, dev.Type, tt.wantType)
		}
		if dev.Status != tt.wantStatus {
			t.Errorf("ClassifyDevice(%q, %q).Status = %q, want %q", tt.serial, tt.state, dev.Status, tt.wantStatus)
		}
	}
}

func TestAndroid_SessionManager(t *testing.T) {
	sm := android.NewSessionManager()
	s := sm.CreateSession("node-1", "Pixel_8_API_34")
	if s == nil {
		t.Fatal("expected non-nil session")
	}
	if s.State != android.SessionStateCreating {
		t.Errorf("expected state %s, got %s", android.SessionStateCreating, s.State)
	}

	found, ok := sm.GetSession(s.ID)
	if !ok || found.ID != s.ID {
		t.Fatalf("failed to retrieve created session: %s", s.ID)
	}

	sm.TouchSession(s.ID)
	list := sm.ListSessions()
	if len(list) != 1 {
		t.Errorf("expected 1 session, got %d", len(list))
	}

	sm.CloseSession(s.ID)
	if found.State != android.SessionStateStopped {
		t.Errorf("expected state %s, got %s", android.SessionStateStopped, found.State)
	}
}

func TestAndroid_SubspaceTargetValidation(t *testing.T) {
	store, err := storage.NewJobStore(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	wsDir := t.TempDir()
	mgr := subspace.NewManager(store, nil, wsDir)
	srv := subspace.NewServer(mgr)

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}
	defer func() { _ = lis.Close() }()

	grpcServer := grpc.NewServer()
	pb.RegisterSubspaceServiceServer(grpcServer, srv)
	go func() { _ = grpcServer.Serve(lis) }()
	defer grpcServer.Stop()

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer func() { _ = conn.Close() }()

	client := pb.NewSubspaceServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	createResp, err := client.CreateSubspace(ctx, &pb.CreateSubspaceRequest{
		ProjectId: "android-app",
		WorkerId:  "worker-1",
	})
	if err != nil {
		t.Fatalf("failed to create subspace: %v", err)
	}
	subID := createResp.Subspace.Id

	getResp, err := client.GetSubspace(ctx, &pb.GetSubspaceRequest{Id: subID})
	if err != nil {
		t.Fatalf("failed to get subspace: %v", err)
	}
	if getResp.Subspace.State != string(apitypes.SubspaceReady) {
		t.Errorf("expected ready state, got %s", getResp.Subspace.State)
	}

	_, err = client.SleepSubspace(ctx, &pb.SleepSubspaceRequest{Id: subID})
	if err != nil {
		t.Fatalf("failed to sleep subspace: %v", err)
	}

	getSleeping, err := client.GetSubspace(ctx, &pb.GetSubspaceRequest{Id: subID})
	if err != nil {
		t.Fatalf("failed to get sleeping subspace: %v", err)
	}
	if getSleeping.Subspace.State != string(apitypes.SubspaceSleeping) {
		t.Errorf("expected sleeping state, got %s", getSleeping.Subspace.State)
	}
}
