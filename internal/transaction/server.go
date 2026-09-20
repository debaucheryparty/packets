package transaction

import (
	"context"
	"time"

	"github.com/debaucheryparty/packets/pkg/apitypes"
	pb "github.com/debaucheryparty/packets/proto/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Server struct {
	pb.UnimplementedTransactionServiceServer
	manager *Manager
}

func NewServer(manager *Manager) *Server {
	return &Server{
		manager: manager,
	}
}

func (s *Server) CreateTransaction(ctx context.Context, req *pb.CreateTransactionRequest) (*pb.TransactionResponse, error) {
	tx, err := s.manager.Create(ctx, req.ProjectId, req.SubspaceId, req.BaseSnapshotRef, req.Description)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "create transaction: %v", err)
	}
	return &pb.TransactionResponse{Transaction: toProto(tx)}, nil
}

func (s *Server) GetTransaction(ctx context.Context, req *pb.GetTransactionRequest) (*pb.TransactionResponse, error) {
	tx, err := s.manager.Get(ctx, req.Id)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "get transaction %s: %v", req.Id, err)
	}
	return &pb.TransactionResponse{Transaction: toProto(tx)}, nil
}

func (s *Server) CommitTransaction(ctx context.Context, req *pb.CommitTransactionRequest) (*pb.TransactionResponse, error) {
	tx, err := s.manager.Commit(ctx, req.Id, req.WorkingSnapshotRef)
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "commit transaction %s: %v", req.Id, err)
	}
	return &pb.TransactionResponse{Transaction: toProto(tx)}, nil
}

func (s *Server) RollbackTransaction(ctx context.Context, req *pb.RollbackTransactionRequest) (*pb.TransactionResponse, error) {
	tx, err := s.manager.Rollback(ctx, req.Id)
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "rollback transaction %s: %v", req.Id, err)
	}
	return &pb.TransactionResponse{Transaction: toProto(tx)}, nil
}

func (s *Server) ListTransactions(ctx context.Context, req *pb.ListTransactionsRequest) (*pb.ListTransactionsResponse, error) {
	txs, err := s.manager.List(ctx, req.ProjectId)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list transactions: %v", err)
	}

	records := make([]*pb.TransactionRecord, 0, len(txs))
	for _, tx := range txs {
		records = append(records, toProto(tx))
	}

	return &pb.ListTransactionsResponse{Transactions: records}, nil
}

func toProto(tx apitypes.Transaction) *pb.TransactionRecord {
	return &pb.TransactionRecord{
		Id:                 tx.ID,
		ProjectId:          tx.ProjectID,
		SubspaceId:         tx.SubspaceID,
		BaseSnapshotRef:    tx.BaseSnapshotRef,
		WorkingSnapshotRef: tx.WorkingSnapshotRef,
		Status:             string(tx.Status),
		Description:        tx.Description,
		CreatedAt:          tx.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:          tx.UpdatedAt.UTC().Format(time.RFC3339),
	}
}
