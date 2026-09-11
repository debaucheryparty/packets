package tests

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
	"github.com/debaucheryparty/packets/internal/mcp"
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

func setupMCPTestServer(t *testing.T, pe *policy.PolicyEngine) (*mcp.Server, *grpc.ClientConn, func()) { //nolint:unparam
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

	conn, err := grpc.DialContext( //nolint:staticcheck
		context.Background(),
		"passthrough://bufnet",
		grpc.WithContextDialer(dialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("Failed to dial bufnet: %v", err)
	}

	cfg := &config.Config{SchedulerGRPCPort: "50051"}
	mcpServer := mcp.NewServer(cfg, slog.Default(), tmpDir)
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

func callMCPTool(t *testing.T, server *mcp.Server, name string, args map[string]interface{}) mcp.CallToolResult {
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
		Result mcp.CallToolResult `json:"result"`
	}
	if err := json.Unmarshal(out.Bytes(), &callResp); err != nil {
		t.Fatalf("Unmarshal call response: %v\nOutput: %s", err, out.String())
	}
	t.Logf("<-- Tool %s returned (isError=%v): %s", name, callResp.Result.IsError, callResp.Result.Content[0].Text)
	return callResp.Result
}

func TestMCPServer_InitializeAndListTools(t *testing.T) {
	cfg := &config.Config{SchedulerGRPCPort: "50051"}
	server := mcp.NewServer(cfg, nil, ".")

	initReq := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n"
	var out bytes.Buffer
	err := server.Run(strings.NewReader(initReq), &out)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	var initResp mcp.JSONRPCResponse
	if err := json.Unmarshal(out.Bytes(), &initResp); err != nil {
		t.Fatalf("Unmarshal init response: %v", err)
	}
	if initResp.Error != nil {
		t.Fatalf("unexpected error: %v", initResp.Error)
	}

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
			Tools []mcp.Tool `json:"tools"`
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
		if _, ok := expectedTools[tool.Name]; ok {
			expectedTools[tool.Name] = true
		} else {
			t.Errorf("unexpected tool registered: %s", tool.Name)
		}
	}

	for toolName, found := range expectedTools {
		if !found {
			t.Errorf("expected tool not found: %s", toolName)
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

	res1 := callMCPTool(t, server, "packets_exec", map[string]interface{}{
		"command": testCmd,
	})
	if !res1.IsError {
		t.Fatalf("expected packets_exec to require approval, but succeeded: %s", res1.Content[0].Text)
	}
	approveText := res1.Content[0].Text
	if !strings.Contains(approveText, "APPROVAL_REQUIRED") {
		t.Fatalf("expected APPROVAL_REQUIRED in output, got: %s", approveText)
	}
	if !strings.Contains(approveText, "Pending Request ID: appr_req_") {
		t.Fatalf("expected Pending Request ID in output, got: %s", approveText)
	}

	var reqID string
	for _, line := range strings.Split(approveText, "\n") {
		if strings.HasPrefix(line, "Pending Request ID: ") {
			reqID = strings.TrimSpace(strings.TrimPrefix(line, "Pending Request ID: "))
			break
		}
	}
	if reqID == "" {
		t.Fatalf("could not extract pending request ID from %s", approveText)
	}

	res2 := callMCPTool(t, server, "packets_exec", map[string]interface{}{
		"command": testCmd,
	})
	if !res2.IsError || !strings.Contains(res2.Content[0].Text, "APPROVAL_REQUIRED") {
		t.Fatalf("second call without ticket should still require approval")
	}

	resApprove := callMCPTool(t, server, "packets_approve", map[string]interface{}{
		"request_id": reqID,
	})
	if resApprove.IsError {
		t.Fatalf("packets_approve failed: %s", resApprove.Content[0].Text)
	}
	if !strings.Contains(resApprove.Content[0].Text, "Approval Ticket: ticket_") {
		t.Fatalf("expected Approval Ticket in approve output, got: %s", resApprove.Content[0].Text)
	}

	var ticketID string
	for _, line := range strings.Split(resApprove.Content[0].Text, "\n") {
		if strings.HasPrefix(line, "Approval Ticket: ") {
			ticketID = strings.TrimSpace(strings.TrimPrefix(line, "Approval Ticket: "))
			break
		}
	}
	if ticketID == "" {
		t.Fatalf("could not extract ticket ID from approval output: %s", approveText)
	}

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

	var jobID string
	for _, part := range strings.Fields(execOutput) {
		if strings.HasPrefix(part, "j_") {
			jobID = strings.TrimRight(part, ",)\r\n:")
			break
		}
	}
	if jobID == "" {
		t.Fatalf("could not extract job ID from: %s", execOutput)
	}

	resLogs := callMCPTool(t, server, "packets_logs", map[string]interface{}{
		"job_id": jobID,
	})
	if resLogs.IsError {
		t.Fatalf("packets_logs failed: %s", resLogs.Content[0].Text)
	}
	if !strings.Contains(resLogs.Content[0].Text, "Hello remote execution") {
		t.Errorf("expected logs to contain output, got: %s", resLogs.Content[0].Text)
	}

	resArt := callMCPTool(t, server, "packets_artifacts", map[string]interface{}{
		"job_id": jobID,
	})
	if !resArt.IsError || !strings.Contains(resArt.Content[0].Text, "No artifacts produced") {
		t.Logf("packets_artifacts response: %s", resArt.Content[0].Text)
	}

	resReplay := callMCPTool(t, server, "packets_exec", map[string]interface{}{
		"command":         testCmd,
		"approval_ticket": ticketID,
	})
	if !resReplay.IsError || !strings.Contains(resReplay.Content[0].Text, "already used") {
		t.Errorf("expected replay with consumed ticket to be rejected, got: isError=%v %s", resReplay.IsError, resReplay.Content[0].Text)
	}
}

func TestMCPServer_RealBuildAndTestExecution(t *testing.T) {
	pe := policy.NewPolicyEngine(policy.ApprovalNever)
	server, _, cleanup := setupMCPTestServer(t, pe)
	defer cleanup()

	testCmd := "echo Build Completed"

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
	if !strings.Contains(resCheck.Content[0].Text, "all_ready") {
		t.Errorf("expected all_ready in response, got: %s", resCheck.Content[0].Text)
	}
}

func TestMCPServer_ProgressNotification(t *testing.T) {
	cfg := &config.Config{SchedulerGRPCPort: "50051"}
	server := mcp.NewServer(cfg, nil, ".")

	var out bytes.Buffer
	server.SetWriter(&out)

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

func TestMCPServer_DirectGRPCBypassRejection(t *testing.T) {
	pe := policy.NewPolicyEngine(policy.ApprovalAlways)
	_, conn, cleanup := setupMCPTestServer(t, pe)
	defer cleanup()

	client := pb.NewSchedulerClient(conn)

	reqDirect := &pb.SubmitJobRequest{
		CacheKey:    "test-cache-key-direct",
		Toolchain:   "exec",
		CommandArgs: []string{"./deploy.sh", "bypass-attempt"},
		ProjectId:   "proj-direct",
		SnapshotRef: "snap-direct",
	}

	_, err := client.SubmitJob(context.Background(), reqDirect)
	if err == nil {
		t.Fatalf("expected direct gRPC submit without approval ticket to be rejected by scheduler policy")
	}
	if !strings.Contains(err.Error(), "policy rejection") {
		t.Errorf("expected policy rejection error, got: %v", err)
	}

	reqForged := &pb.SubmitJobRequest{
		CacheKey:       "test-cache-key-forged",
		Toolchain:      "exec",
		CommandArgs:    []string{"./deploy.sh", "forged-attempt"},
		ProjectId:      "proj-direct",
		SnapshotRef:    "snap-direct",
		ApprovalTicket: "ticket_forged_9999",
	}
	_, err = client.SubmitJob(context.Background(), reqForged)
	if err == nil {
		t.Fatalf("expected direct gRPC submit with forged ticket to be rejected")
	}
}
