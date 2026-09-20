package tests

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/debaucheryparty/packets/internal/storage"
	"github.com/debaucheryparty/packets/internal/transaction"
	pb "github.com/debaucheryparty/packets/proto/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func setupTestTransactionServer(t *testing.T) (pb.TransactionServiceClient, func()) {
	t.Helper()
	store, err := storage.NewJobStore(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	mgr := transaction.NewManager(store, nil)
	srv := transaction.NewServer(mgr)

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}

	grpcServer := grpc.NewServer()
	pb.RegisterTransactionServiceServer(grpcServer, srv)

	go func() { _ = grpcServer.Serve(lis) }()

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	client := pb.NewTransactionServiceClient(conn)
	cleanup := func() {
		_ = conn.Close()
		grpcServer.Stop()
		_ = lis.Close()
		_ = store.Close()
	}

	return client, cleanup
}

func TestTransaction_GRPC(t *testing.T) {
	client, cleanup := setupTestTransactionServer(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	createResp, err := client.CreateTransaction(ctx, &pb.CreateTransactionRequest{
		ProjectId:       "proj-grpc-test",
		BaseSnapshotRef: "snap-base-123",
		Description:     "test transaction",
	})
	if err != nil {
		t.Fatalf("CreateTransaction failed: %v", err)
	}

	tx := createResp.Transaction
	if tx.Status != "open" {
		t.Errorf("expected open status, got %s", tx.Status)
	}

	getResp, err := client.GetTransaction(ctx, &pb.GetTransactionRequest{Id: tx.Id})
	if err != nil {
		t.Fatalf("GetTransaction failed: %v", err)
	}
	if getResp.Transaction.Id != tx.Id {
		t.Errorf("expected transaction ID %s, got %s", tx.Id, getResp.Transaction.Id)
	}

	commitResp, err := client.CommitTransaction(ctx, &pb.CommitTransactionRequest{
		Id:                 tx.Id,
		WorkingSnapshotRef: "snap-working-456",
	})
	if err != nil {
		t.Fatalf("CommitTransaction failed: %v", err)
	}
	if commitResp.Transaction.Status != "committed" {
		t.Errorf("expected committed status, got %s", commitResp.Transaction.Status)
	}

	createResp2, err := client.CreateTransaction(ctx, &pb.CreateTransactionRequest{
		ProjectId:       "proj-grpc-test",
		BaseSnapshotRef: "snap-base-999",
		Description:     "test rollback",
	})
	if err != nil {
		t.Fatalf("CreateTransaction 2 failed: %v", err)
	}

	rollbackResp, err := client.RollbackTransaction(ctx, &pb.RollbackTransactionRequest{
		Id: createResp2.Transaction.Id,
	})
	if err != nil {
		t.Fatalf("RollbackTransaction failed: %v", err)
	}
	if rollbackResp.Transaction.Status != "rolled_back" {
		t.Errorf("expected rolled_back status, got %s", rollbackResp.Transaction.Status)
	}

	listResp, err := client.ListTransactions(ctx, &pb.ListTransactionsRequest{
		ProjectId: "proj-grpc-test",
	})
	if err != nil {
		t.Fatalf("ListTransactions failed: %v", err)
	}
	if len(listResp.Transactions) != 2 {
		t.Errorf("expected 2 transactions, got %d", len(listResp.Transactions))
	}
}
