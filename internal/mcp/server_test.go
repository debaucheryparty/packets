package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/debaucheryparty/packets/internal/config"
	"github.com/debaucheryparty/packets/internal/environment"
	"github.com/debaucheryparty/packets/internal/policy"
	"github.com/debaucheryparty/packets/internal/scheduler"
	"github.com/debaucheryparty/packets/internal/storage"
	"github.com/debaucheryparty/packets/internal/toolchain"
	"github.com/debaucheryparty/packets/internal/worker"
	pb "github.com/debaucheryparty/packets/proto/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

type testWorkspaceServer struct {
	pb.UnimplementedWorkspaceServer
}

func (s *testWorkspaceServer) Diff(ctx context.Context, req *pb.WorkspaceManifest) (*pb.DiffResponse, error) {
	return &pb.DiffResponse{
		ExistingSnapshotRef: req.RootHash,
	}, nil
}

func setupMCPTestServer(t *testing.T, pe *policy.PolicyEngine) (*Server, *grpc.ClientConn, func()) {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()

	tmpDir := t.TempDir()
	store, err := storage.NewJobStore(context.Background(), filepath.Join(tmpDir, "packets_test.db"))
	if err != nil {
		t.Fatalf("NewJobStore: %v", err)
	}

	logBroker := scheduler.NewLogBroker()
	exec := worker.NewExecutor(slog.Default(), nil, nil, toolchain.NewRegistry(), logBroker, tmpDir)
	dispatcher := scheduler.NewDispatcher(slog.Default(), store, nil, exec, nil, logBroker)

	schedServer := scheduler.NewServer(dispatcher, store, logBroker, nil, nil)
	schedServer.SetPolicyEngine(pe)
	pb.RegisterSchedulerServer(grpcServer, schedServer)

	wsServer := &testWorkspaceServer{}
	pb.RegisterWorkspaceServer(grpcServer, wsServer)

	envServer := environment.NewServer(environment.NewManager())
	pb.RegisterEnvironmentServer(grpcServer, envServer)

	go func() {
		_ = grpcServer.Serve(lis)
	}()

	dialer := func(context.Context, string) (net.Conn, error) {
		return lis.Dial()
	}

	conn, err := grpc.DialContext(
		context.Background(),
		"passthrough://bufnet",
		grpc.WithContextDialer(dialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("Failed to dial bufnet: %v", err)
	}

	cfg := &config.Config{SchedulerGRPCPort: "50051"}
	mcpServer := NewServer(cfg, slog.Default(), tmpDir)
	mcpServer.SetConn(conn)
	mcpServer.SetPolicy(pe)

	cleanup := func() {
		_ = conn.Close()
		grpcServer.Stop()
		_ = lis.Close()
		_ = store.Close()
	}

	return mcpServer, conn, cleanup
}

func callMCPTool(t *testing.T, server *Server, name string, args map[string]interface{}) CallToolResult {
	t.Helper()
	callReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      name,
			"arguments": args,
		},
	}
	raw, err := json.Marshal(callReq)
	if err != nil {
		t.Fatalf("Marshal callReq: %v", err)
	}

	t.Logf("--> Calling MCP tool %s...", name)
	var out bytes.Buffer
	err = server.Run(bytes.NewReader(append(raw, '\n')), &out)
	if err != nil {
		t.Fatalf("server.Run failed: %v", err)
	}

	var callResp struct {
		Result CallToolResult `json:"result"`
	}
	if err := json.Unmarshal(out.Bytes(), &callResp); err != nil {
		t.Fatalf("Unmarshal call response: %v\nOutput: %s", err, out.String())
	}
	t.Logf("<-- Tool %s returned (isError=%v): %s", name, callResp.Result.IsError, callResp.Result.Content[0].Text)
	return callResp.Result
}

func TestMCPServer_InitializeAndListTools(t *testing.T) {
	cfg := &config.Config{SchedulerGRPCPort: "50051"}
	server := NewServer(cfg, nil, ".")

	// 1. Test initialize
	initReq := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n"
	var out bytes.Buffer
	err := server.Run(strings.NewReader(initReq), &out)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	var initResp JSONRPCResponse
	if err := json.Unmarshal(out.Bytes(), &initResp); err != nil {
		t.Fatalf("Unmarshal init response: %v", err)
	}
	if initResp.Error != nil {
		t.Fatalf("unexpected error: %v", initResp.Error)
	}

	// 2. Test tools/list
	listReq := `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}` + "\n"
	out.Reset()
	err = server.Run(strings.NewReader(listReq), &out)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	var listResp struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Result  struct {
			Tools []Tool `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out.Bytes(), &listResp); err != nil {
		t.Fatalf("Unmarshal list response: %v", err)
	}

	if len(listResp.Result.Tools) != 13 {
		t.Errorf("expected 13 tools, got %d", len(listResp.Result.Tools))
	}

	expectedTools := map[string]bool{
		"packets_workspace_info": false,
		"packets_env_check":      false,
		"packets_env_prepare":    false,
		"packets_sync":           false,
		"packets_sync_full":      false,
		"packets_approve":        false,
		"packets_build":          false,
		"packets_test":           false,
		"packets_exec":           false,
		"packets_logs":           false,
		"packets_artifacts":      false,
		"packets_pull":           false,
		"packets_status":         false,
	}

	for _, tool := range listResp.Result.Tools {
		expectedTools[tool.Name] = true
	}

	for toolName, found := range expectedTools {
		if !found {
			t.Errorf("missing expected tool: %s", toolName)
		}
	}
}

func TestMCPServer_RealMCPExecutionAndApprovalWorkflow(t *testing.T) {
	pe := policy.NewPolicyEngine(policy.ApprovalAlways)
	server, _, cleanup := setupMCPTestServer(t, pe)
	defer cleanup()

	testCmd := "set DUMMY=1 && echo Hello remote execution"
	if runtime.GOOS != "windows" {
		testCmd = "export DUMMY=1 && echo Hello remote execution"
	}

	// 1. Call packets_exec without approval ticket -> must be paused and request approval
	res1 := callMCPTool(t, server, "packets_exec", map[string]interface{}{
		"command": testCmd,
	})
	if !res1.IsError {
		t.Fatalf("expected IsError: true for unapproved execution")
	}
	prompt := res1.Content[0].Text
	if !strings.Contains(prompt, "APPROVAL_REQUIRED") {
		t.Fatalf("expected APPROVAL_REQUIRED, got: %s", prompt)
	}

	// Extract Pending Request ID
	var reqID string
	for _, line := range strings.Split(prompt, "\n") {
		if strings.HasPrefix(line, "Pending Request ID: ") {
			reqID = strings.TrimSpace(strings.TrimPrefix(line, "Pending Request ID: "))
			break
		}
	}
	if reqID == "" {
		t.Fatalf("could not extract pending request ID from prompt: %s", prompt)
	}

	// 2. Reject arbitrary execution without approval ticket
	resUnapproved := callMCPTool(t, server, "packets_exec", map[string]interface{}{
		"command": testCmd,
	})
	if !strings.Contains(resUnapproved.Content[0].Text, "APPROVAL_REQUIRED") {
		t.Fatalf("expected unapproved call to be blocked again")
	}

	// 3. Human calls packets_approve tool
	resApprove := callMCPTool(t, server, "packets_approve", map[string]interface{}{
		"request_id": reqID,
	})
	if resApprove.IsError {
		t.Fatalf("packets_approve returned error: %s", resApprove.Content[0].Text)
	}
	approveText := resApprove.Content[0].Text
	var ticketID string
	for _, line := range strings.Split(approveText, "\n") {
		if strings.HasPrefix(line, "Approval Ticket: ") {
			ticketID = strings.TrimSpace(strings.TrimPrefix(line, "Approval Ticket: "))
			break
		}
	}
	if ticketID == "" {
		t.Fatalf("could not extract ticket ID from approval output: %s", approveText)
	}

	// 4. Submit packets_exec with the valid approval ticket -> executes via scheduler gRPC and streams real logs!
	resExec := callMCPTool(t, server, "packets_exec", map[string]interface{}{
		"command":         testCmd,
		"approval_ticket": ticketID,
	})
	if resExec.IsError {
		t.Fatalf("packets_exec failed with valid ticket: %s", resExec.Content[0].Text)
	}
	execOutput := resExec.Content[0].Text
	if !strings.Contains(execOutput, "(job:") {
		t.Errorf("expected job reference in execution output, got: %s", execOutput)
	}
	if !strings.Contains(execOutput, "Hello remote execution") {
		t.Errorf("expected streamed command output in execution logs, got: %s", execOutput)
	}

	// Extract Job ID
	var jobID string
	for _, part := range strings.Fields(execOutput) {
		if strings.HasPrefix(part, "j_") {
			jobID = strings.TrimRight(part, ",)\r\n:")
			break
		}
	}

	// 5. Test packets_logs for this real job
	if jobID != "" {
		resLogs := callMCPTool(t, server, "packets_logs", map[string]interface{}{
			"job_id": jobID,
		})
		if resLogs.IsError {
			t.Errorf("packets_logs returned error: %s", resLogs.Content[0].Text)
		}
		if !strings.Contains(resLogs.Content[0].Text, "Hello remote execution") {
			t.Errorf("packets_logs missing output: %s", resLogs.Content[0].Text)
		}

		// Test packets_artifacts query
		resArt := callMCPTool(t, server, "packets_artifacts", map[string]interface{}{
			"job_id": jobID,
		})
		// If no artifacts produced, reports no artifacts
		if !strings.Contains(resArt.Content[0].Text, "No artifact") && resArt.IsError {
			t.Errorf("unexpected packets_artifacts error: %s", resArt.Content[0].Text)
		}
	}

	// 6. Replay attack: try using the same ticket again -> must be rejected as single-use
	resReplay := callMCPTool(t, server, "packets_exec", map[string]interface{}{
		"command":         testCmd,
		"approval_ticket": ticketID,
	})
	if !resReplay.IsError && !strings.Contains(resReplay.Content[0].Text, "APPROVAL_REQUIRED") {
		t.Fatalf("expected reused ticket to be rejected, got: %v", resReplay.Content[0].Text)
	}
}

func TestMCPServer_RealBuildAndTestExecution(t *testing.T) {
	pe := policy.NewPolicyEngine(policy.ApprovalNever) // Allow without approval for this test
	server, _, cleanup := setupMCPTestServer(t, pe)
	defer cleanup()

	testCmd := "echo Build Completed"

	// 1. packets_build
	resBuild := callMCPTool(t, server, "packets_build", map[string]interface{}{
		"command":   testCmd,
		"toolchain": "custom",
	})
	if resBuild.IsError {
		t.Fatalf("packets_build failed: %s", resBuild.Content[0].Text)
	}
	if !strings.Contains(resBuild.Content[0].Text, "(job:") {
		t.Errorf("expected job ID in build output, got: %s", resBuild.Content[0].Text)
	}
	if !strings.Contains(resBuild.Content[0].Text, "Build Completed") {
		t.Errorf("expected real execution logs, got: %s", resBuild.Content[0].Text)
	}

	// 2. packets_test
	testCmd2 := "echo Test Passed"
	resTest := callMCPTool(t, server, "packets_test", map[string]interface{}{
		"command":   testCmd2,
		"toolchain": "custom",
	})
	if resTest.IsError {
		t.Fatalf("packets_test failed: %s", resTest.Content[0].Text)
	}
	if !strings.Contains(resTest.Content[0].Text, "(job:") {
		t.Errorf("expected job ID in test output, got: %s", resTest.Content[0].Text)
	}
	if !strings.Contains(resTest.Content[0].Text, "Test Passed") {
		t.Errorf("expected real test logs, got: %s", resTest.Content[0].Text)
	}
}

func TestMCPServer_RemoteEnvCheckViaGRPC(t *testing.T) {
	pe := policy.NewPolicyEngine(policy.ApprovalNever)
	server, _, cleanup := setupMCPTestServer(t, pe)
	defer cleanup()

	resCheck := callMCPTool(t, server, "packets_env_check", map[string]interface{}{})
	if resCheck.IsError {
		t.Fatalf("packets_env_check failed: %s", resCheck.Content[0].Text)
	}
	// Verify it reached remote environment gRPC server
	if !strings.Contains(resCheck.Content[0].Text, "all_ready") {
		t.Errorf("expected all_ready in response, got: %s", resCheck.Content[0].Text)
	}
}

func TestMCPServer_ProgressNotification(t *testing.T) {
	cfg := &config.Config{SchedulerGRPCPort: "50051"}
	server := NewServer(cfg, nil, ".")

	var out bytes.Buffer
	// Set writer through running empty reader or directly
	server.writer = &out

	server.SendProgress("token-abc", 42, 100, "Gradle assembleDebug running")

	var notif struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
		Params  struct {
			ProgressToken string  `json:"progressToken"`
			Progress      float64 `json:"progress"`
			Total         float64 `json:"total"`
			Message       string  `json:"message"`
		} `json:"params"`
	}

	if err := json.Unmarshal(out.Bytes(), &notif); err != nil {
		t.Fatalf("Unmarshal progress notification: %v\nOutput: %s", err, out.String())
	}

	if notif.Method != "notifications/progress" {
		t.Errorf("expected notifications/progress, got %s", notif.Method)
	}
	if notif.Params.ProgressToken != "token-abc" {
		t.Errorf("expected token-abc, got %s", notif.Params.ProgressToken)
	}
	if notif.Params.Progress != 42 {
		t.Errorf("expected progress 42, got %f", notif.Params.Progress)
	}
	if notif.Params.Message != "Gradle assembleDebug running" {
		t.Errorf("expected message, got %s", notif.Params.Message)
	}
}

