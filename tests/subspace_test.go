package tests

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/debaucheryparty/packets/internal/storage"
	"github.com/debaucheryparty/packets/internal/subspace"
	"github.com/debaucheryparty/packets/pkg/apitypes"
	pb "github.com/debaucheryparty/packets/proto/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func setupTestSubspaceServer(t *testing.T) (pb.SubspaceServiceClient, *subspace.Manager, func()) {
	t.Helper()
	store, err := storage.NewJobStore(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	wsDir := t.TempDir()
	mgr := subspace.NewManager(store, nil, wsDir)
	srv := subspace.NewServer(mgr)

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}

	grpcServer := grpc.NewServer()
	pb.RegisterSubspaceServiceServer(grpcServer, srv)

	go func() { _ = grpcServer.Serve(lis) }()

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	client := pb.NewSubspaceServiceClient(conn)
	cleanup := func() {
		_ = conn.Close()
		grpcServer.Stop()
		_ = lis.Close()
		_ = store.Close()
	}

	return client, mgr, cleanup
}

func TestSubspace_GRPCServiceLifecycle(t *testing.T) {
	client, _, cleanup := setupTestSubspaceServer(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	createResp, err := client.CreateSubspace(ctx, &pb.CreateSubspaceRequest{
		ProjectId:     "proj-android-app",
		OwnerId:       "dev-user-1",
		WorkerId:      "vps-remote-node",
		EnvironmentId: "jdk-21-gradle",
		Metadata: map[string]string{
			"flavor": "debug",
		},
	})
	if err != nil {
		t.Fatalf("CreateSubspace failed: %v", err)
	}

	sub := createResp.Subspace
	if sub.Id == "" || sub.State != string(apitypes.SubspaceReady) {
		t.Errorf("unexpected created subspace: %+v", sub)
	}
	if sub.ProjectId != "proj-android-app" || sub.WorkerId != "vps-remote-node" {
		t.Errorf("field mismatch: %+v", sub)
	}

	getResp, err := client.GetSubspace(ctx, &pb.GetSubspaceRequest{Id: sub.Id})
	if err != nil {
		t.Fatalf("GetSubspace failed: %v", err)
	}
	if getResp.Subspace.Id != sub.Id || getResp.Subspace.Metadata["flavor"] != "debug" {
		t.Errorf("mismatch on GetSubspace: got %+v, want %+v", getResp.Subspace, sub)
	}

	touchResp, err := client.TouchSubspace(ctx, &pb.TouchSubspaceRequest{Id: sub.Id})
	if err != nil {
		t.Fatalf("TouchSubspace failed: %v", err)
	}
	if touchResp.Subspace.Id != sub.Id {
		t.Errorf("TouchSubspace mismatch: %+v", touchResp.Subspace)
	}

	listResp, err := client.ListSubspaces(ctx, &pb.ListSubspacesRequest{
		OwnerId:   "dev-user-1",
		ProjectId: "proj-android-app",
	})
	if err != nil || len(listResp.Subspaces) != 1 {
		t.Fatalf("ListSubspaces expected 1 item, got %d (err=%v)", len(listResp.Subspaces), err)
	}

	delResp, err := client.DestroySubspace(ctx, &pb.DestroySubspaceRequest{Id: sub.Id})
	if err != nil || !delResp.Success {
		t.Fatalf("DestroySubspace failed: %v", err)
	}

	_, err = client.GetSubspace(ctx, &pb.GetSubspaceRequest{Id: sub.Id})
	if err == nil {
		t.Errorf("expected error getting destroyed subspace, got nil")
	}
}

func TestSubspace_PersistenceAcrossReload(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "subspaces_test.db")
	ctx := context.Background()

	s1, err := storage.NewJobStore(ctx, dbPath)
	if err != nil {
		t.Fatalf("open store 1: %v", err)
	}

	mgr1 := subspace.NewManager(s1, nil, filepath.Join(tempDir, "workspaces"))
	sub1, err := mgr1.Create(ctx, subspace.CreateOptions{
		ProjectID:     "persistent-proj",
		OwnerID:       "alice",
		WorkerID:      "worker-persisted",
		EnvironmentID: "env-rust",
		Metadata: map[string]string{
			"arch": "x86_64",
		},
	})
	if err != nil {
		t.Fatalf("create subspace: %v", err)
	}

	_ = s1.Close()

	s2, err := storage.NewJobStore(ctx, dbPath)
	if err != nil {
		t.Fatalf("open store 2: %v", err)
	}
	defer func() { _ = s2.Close() }()

	mgr2 := subspace.NewManager(s2, nil, filepath.Join(tempDir, "workspaces"))
	sub2, err := mgr2.Get(ctx, sub1.ID)
	if err != nil {
		t.Fatalf("get subspace from reloaded store: %v", err)
	}

	if sub2.ID != sub1.ID || sub2.ProjectID != "persistent-proj" || sub2.WorkerID != "worker-persisted" {
		t.Errorf("reloaded subspace mismatch: got %+v, want %+v", sub2, sub1)
	}
	if sub2.Metadata["arch"] != "x86_64" {
		t.Errorf("reloaded metadata mismatch: got %+v", sub2.Metadata)
	}
}

func TestSubspace_WorkspaceIsolation(t *testing.T) {
	tempDir := t.TempDir()
	store, err := storage.NewJobStore(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = store.Close() }()

	mgr := subspace.NewManager(store, nil, tempDir)
	ctx := context.Background()

	subA, err := mgr.Create(ctx, subspace.CreateOptions{ProjectID: "proj-A", OwnerID: "user-1"})
	if err != nil {
		t.Fatalf("create subA: %v", err)
	}
	subB, err := mgr.Create(ctx, subspace.CreateOptions{ProjectID: "proj-B", OwnerID: "user-2"})
	if err != nil {
		t.Fatalf("create subB: %v", err)
	}

	wsPathA, err := mgr.WorkspacePath(subA)
	if err != nil {
		t.Fatalf("WorkspacePath A: %v", err)
	}
	wsPathB, err := mgr.WorkspacePath(subB)
	if err != nil {
		t.Fatalf("WorkspacePath B: %v", err)
	}

	if wsPathA == wsPathB {
		t.Errorf("workspace paths must be isolated, got identical: %s", wsPathA)
	}

	fileA := filepath.Join(wsPathA, "secretA.txt")
	_ = os.WriteFile(fileA, []byte("user1-secret"), 0o644)

	fileB := filepath.Join(wsPathB, "secretA.txt")
	if _, err := os.Stat(fileB); !os.IsNotExist(err) {
		t.Errorf("file from subA leaked into subB workspace")
	}
}
