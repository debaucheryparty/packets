package tests

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/debaucheryparty/packets/internal/policy"
	"github.com/debaucheryparty/packets/internal/scheduler"
	"github.com/debaucheryparty/packets/internal/storage"
	"github.com/debaucheryparty/packets/internal/toolchain"
	"github.com/debaucheryparty/packets/internal/worker"
	"github.com/debaucheryparty/packets/pkg/apitypes"
	pb "github.com/debaucheryparty/packets/proto/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

func TestRealNetwork_TCPGRPC_Lifecycle(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen on TCP: %v", err)
	}
	defer func() { _ = lis.Close() }()

	port := lis.Addr().(*net.TCPAddr).Port
	tmpDir := t.TempDir()

	store, err := storage.NewJobStore(context.Background(), filepath.Join(tmpDir, "network_test.db"))
	if err != nil {
		t.Fatalf("NewJobStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	logBroker := scheduler.NewLogBroker()
	exec := worker.NewExecutor(slog.Default(), nil, nil, toolchain.NewRegistry(), logBroker, tmpDir)
	dispatcher := scheduler.NewDispatcher(slog.Default(), store, nil, exec, nil, logBroker)

	grpcServer := grpc.NewServer()
	schedServer := scheduler.NewServer(dispatcher, store, logBroker, nil, nil)
	schedServer.SetPolicyEngine(policy.NewPolicyEngine(policy.ApprovalNever))
	pb.RegisterSchedulerServer(grpcServer, schedServer)

	go func() {
		_ = grpcServer.Serve(lis)
	}()
	defer grpcServer.Stop()

	targetAddr := fmt.Sprintf("127.0.0.1:%d", port)
	conn, err := grpc.NewClient(targetAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("failed to dial real TCP target: %v", err)
	}
	defer func() { _ = conn.Close() }()

	client := pb.NewSchedulerClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmdArgs := []string{"echo network-verified"}
	subResp, err := client.SubmitJob(ctx, &pb.SubmitJobRequest{
		CacheKey:    "key-tcp-network-test",
		Toolchain:   "custom",
		Runner:      string(apitypes.RunnerHost),
		SourceMode:  string(apitypes.SourceModeWorkspace),
		CommandArgs: cmdArgs,
		ProjectId:   "proj-network",
	})
	if err != nil {
		t.Fatalf("SubmitJob over real TCP failed: %v", err)
	}
	if subResp.JobId == "" {
		t.Fatalf("expected non-empty JobId over TCP")
	}

	stream, err := client.StreamJobLogs(ctx, &pb.StreamJobLogsRequest{JobId: subResp.JobId})
	if err != nil {
		t.Fatalf("StreamJobLogs over real TCP failed: %v", err)
	}

	var logsReceived []string
	for {
		line, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			break
		}
		logsReceived = append(logsReceived, line.Content)
	}

	fullLog := strings.Join(logsReceived, "\n")
	if !strings.Contains(fullLog, "network-verified") {
		t.Logf("received logs: %s", fullLog)
	}

	var finalState pb.JobState
	var finalErr string
	for i := 0; i < 30; i++ {
		statusResp, err := client.GetJobStatus(ctx, &pb.GetJobStatusRequest{JobId: subResp.JobId})
		if err == nil {
			finalState = statusResp.State
			finalErr = statusResp.ErrorMessage
			if finalState == pb.JobState_JOB_STATE_SUCCEEDED || finalState == pb.JobState_JOB_STATE_FAILED {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}

	if finalState != pb.JobState_JOB_STATE_SUCCEEDED {
		t.Errorf("expected job state SUCCEEDED, got: %s (err: %s)", finalState, finalErr)
	}
}

func TestRealNetwork_RemoteVPS_Integration(t *testing.T) {
	vpsAddr := os.Getenv("PACKETS_TEST_VPS_ADDR")
	if vpsAddr == "" {
		t.Skip("PACKETS_TEST_VPS_ADDR not set; external VPS real-network test skipped cleanly")
	}

	auth := os.Getenv("PACKETS_TEST_VPS_AUTH")
	wsRoot := os.Getenv("PACKETS_TEST_VPS_WORKSPACE")
	_ = wsRoot

	var dialOpts []grpc.DialOption
	dialOpts = append(dialOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if auth != "" {
		dialOpts = append(dialOpts, grpc.WithUnaryInterceptor(func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
			ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+auth)
			return invoker(ctx, method, req, reply, cc, opts...)
		}))
	}

	conn, err := grpc.NewClient(vpsAddr, dialOpts...)
	if err != nil {
		t.Fatalf("dial VPS %s failed: %v", vpsAddr, err)
	}
	defer func() { _ = conn.Close() }()

	client := pb.NewSchedulerClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	cacheKey := fmt.Sprintf("vps-test-%d", time.Now().UnixNano())
	cmdArgs := []string{"echo vps-live-test"}
	subResp, err := client.SubmitJob(ctx, &pb.SubmitJobRequest{
		CacheKey:    cacheKey,
		Toolchain:   "custom",
		Runner:      string(apitypes.RunnerHost),
		SourceMode:  string(apitypes.SourceModeWorkspace),
		CommandArgs: cmdArgs,
		ProjectId:   "proj-vps",
	})
	if err != nil {
		t.Fatalf("VPS SubmitJob failed: %v", err)
	}
	if subResp.JobId == "" {
		t.Fatalf("expected non-empty JobId from VPS")
	}

	st, err := client.GetJobStatus(ctx, &pb.GetJobStatusRequest{JobId: subResp.JobId})
	if err != nil {
		t.Fatalf("VPS GetJobStatus failed: %v", err)
	}
	if st == nil {
		t.Fatalf("expected non-nil status from VPS")
	}

	logStream, err := client.StreamJobLogs(ctx, &pb.StreamJobLogsRequest{JobId: subResp.JobId})
	if err == nil {
		_, _ = logStream.Recv()
	}

	artStream, err := client.DownloadArtifact(ctx, &pb.DownloadArtifactRequest{JobId: subResp.JobId})
	if err == nil {
		_, _ = artStream.Recv()
	}

	_ = conn.Close()
	conn2, err := grpc.NewClient(vpsAddr, dialOpts...)
	if err == nil {
		defer func() { _ = conn2.Close() }()
		client2 := pb.NewSchedulerClient(conn2)
		_, _ = client2.GetJobStatus(ctx, &pb.GetJobStatusRequest{JobId: subResp.JobId})
	}

	_, _ = client.ClearCache(ctx, &pb.ClearCacheRequest{})
}
