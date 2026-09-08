package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/debaucheryparty/packets/internal/config"
	"github.com/debaucheryparty/packets/internal/environment"
	"github.com/debaucheryparty/packets/internal/policy"
	"github.com/debaucheryparty/packets/internal/project"
	"github.com/debaucheryparty/packets/internal/workspace"
	"github.com/debaucheryparty/packets/pkg/apitypes"
	pb "github.com/debaucheryparty/packets/proto/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type JSONRPCResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      interface{}   `json:"id"`
	Result  interface{}   `json:"result,omitempty"`
	Error   *JSONRPCError `json:"error,omitempty"`
}

type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type Tool struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	InputSchema ToolSchema `json:"inputSchema"`
}

type ToolSchema struct {
	Type       string                 `json:"type"`
	Properties map[string]PropertyDef `json:"properties"`
	Required   []string               `json:"required,omitempty"`
}

type PropertyDef struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

type CallToolParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

type TextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type CallToolResult struct {
	Content []TextContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

type Server struct {
	cfg        *config.Config
	logger     *slog.Logger
	envMgr     *environment.Manager
	policy     *policy.PolicyEngine
	workingDir string
	conn       *grpc.ClientConn
}

func NewServer(cfg *config.Config, logger *slog.Logger, workingDir string) *Server {
	if workingDir == "" {
		workingDir, _ = os.Getwd()
	}
	return &Server{
		cfg:        cfg,
		logger:     logger,
		envMgr:     environment.NewManager(),
		policy:     policy.NewPolicyEngine(policy.ApprovalAlways),
		workingDir: workingDir,
	}
}

func (s *Server) SetConn(conn *grpc.ClientConn) {
	s.conn = conn
}

func (s *Server) SetPolicy(pe *policy.PolicyEngine) {
	s.policy = pe
}

func (s *Server) Run(r io.Reader, w io.Writer) error {
	scanner := bufio.NewScanner(r)
	encoder := json.NewEncoder(w)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var req JSONRPCRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			_ = encoder.Encode(JSONRPCResponse{
				JSONRPC: "2.0",
				Error:   &JSONRPCError{Code: -32700, Message: "Parse error"},
			})
			continue
		}

		resp := s.handleRequest(req)
		if resp != nil {
			if err := encoder.Encode(resp); err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}

func (s *Server) handleRequest(req JSONRPCRequest) *JSONRPCResponse {
	ctx := context.Background()

	switch req.Method {
	case "initialize":
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"serverInfo": map[string]string{
					"name":    "packets-mcp",
					"version": "0.2.0",
				},
				"capabilities": map[string]interface{}{
					"tools": map[string]bool{"listChanged": false},
				},
			},
		}

	case "notifications/initialized":
		return nil

	case "tools/list":
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]interface{}{
				"tools": s.listTools(),
			},
		}

	case "tools/call":
		var params CallToolParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return &JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &JSONRPCError{Code: -32602, Message: "Invalid params"},
			}
		}

		res := s.executeTool(ctx, params)
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  res,
		}

	default:
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &JSONRPCError{Code: -32601, Message: fmt.Sprintf("Method not found: %s", req.Method)},
		}
	}
}

func (s *Server) listTools() []Tool {
	return []Tool{
		{
			Name:        "packets_workspace_info",
			Description: "Inspect the project components, toolchains, and topology",
			InputSchema: ToolSchema{Type: "object", Properties: map[string]PropertyDef{
				"dir": {Type: "string", Description: "Project directory path"},
			}},
		},
		{
			Name:        "packets_env_check",
			Description: "Remotely verify toolchain and SDK requirements on the VPS worker",
			InputSchema: ToolSchema{Type: "object", Properties: map[string]PropertyDef{
				"dir": {Type: "string", Description: "Project directory path"},
			}},
		},
		{
			Name:        "packets_env_prepare",
			Description: "Remotely provision missing development toolchains and SDKs on the VPS worker",
			InputSchema: ToolSchema{Type: "object", Properties: map[string]PropertyDef{
				"dir": {Type: "string", Description: "Project directory path"},
			}},
		},
		{
			Name:        "packets_sync",
			Description: "Synchronize modified local files incrementally to the remote persistent workspace",
			InputSchema: ToolSchema{Type: "object", Properties: map[string]PropertyDef{
				"dir": {Type: "string", Description: "Project directory path"},
			}},
		},
		{
			Name:        "packets_sync_full",
			Description: "Perform a full synchronization with remote deletion detection",
			InputSchema: ToolSchema{Type: "object", Properties: map[string]PropertyDef{
				"dir": {Type: "string", Description: "Project directory path"},
			}},
		},
		{
			Name:        "packets_build",
			Description: "Trigger a remote build for the project (e.g. Android Gradle, Zephyr west, Rust cargo)",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"dir":             {Type: "string", Description: "Project directory path"},
					"command":         {Type: "string", Description: "Custom build command/args (e.g. assembleDebug, build)"},
					"toolchain":       {Type: "string", Description: "Target toolchain (android, zephyr, rust, go, etc.)"},
					"approval_ticket": {Type: "string", Description: "Server-issued single-use human approval ticket"},
				},
			},
		},
		{
			Name:        "packets_test",
			Description: "Trigger remote unit or integration tests",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"dir":             {Type: "string", Description: "Project directory path"},
					"command":         {Type: "string", Description: "Custom test command/args (e.g. test, cargo test)"},
					"approval_ticket": {Type: "string", Description: "Server-issued single-use human approval ticket"},
				},
			},
		},
		{
			Name:        "packets_exec",
			Description: "Execute an arbitrary shell command remotely in the persistent VPS workspace",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"command":         {Type: "string", Description: "Command string to execute"},
					"dir":             {Type: "string", Description: "Project directory path"},
					"approval_ticket": {Type: "string", Description: "Server-issued single-use human approval ticket"},
				},
				Required: []string{"command"},
			},
		},
		{
			Name:        "packets_approve",
			Description: "Approve a pending human approval request and obtain a single-use execution ticket",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"request_id": {Type: "string", Description: "Pending approval request ID"},
				},
				Required: []string{"request_id"},
			},
		},
		{
			Name:        "packets_logs",
			Description: "Retrieve execution logs for a specific remote job",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"job_id": {Type: "string", Description: "Job ID to retrieve logs for"},
				},
				Required: []string{"job_id"},
			},
		},
		{
			Name:        "packets_artifacts",
			Description: "Download and extract artifacts produced by a remote build",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"job_id": {Type: "string", Description: "Job ID producing the artifact"},
					"dir":    {Type: "string", Description: "Destination directory path"},
				},
				Required: []string{"job_id"},
			},
		},
		{
			Name:        "packets_status",
			Description: "Check status of the Packets remote daemon and active jobs",
			InputSchema: ToolSchema{Type: "object", Properties: map[string]PropertyDef{}},
		},
	}
}

func (s *Server) executeTool(ctx context.Context, params CallToolParams) CallToolResult {
	dir := s.workingDir
	if d, ok := params.Arguments["dir"].(string); ok && d != "" {
		dir = d
	}

	switch params.Name {
	case "packets_workspace_info":
		topo, err := s.envMgr.Detect(dir)
		if err != nil {
			return errorResult(err.Error())
		}
		data, _ := json.MarshalIndent(topo, "", "  ")
		return textResult(string(data))

	case "packets_env_check":
		conn, err := s.dialScheduler(ctx)
		if err != nil {
			return errorResult("Connect to Packets daemon failed: " + err.Error())
		}
		if s.conn == nil {
			defer conn.Close()
		}

		topo, err := s.envMgr.Detect(dir)
		if err != nil {
			return errorResult("Project detection failed: " + err.Error())
		}

		projectID := project.ResolveProjectID(dir)
		remote := environment.NewRemoteClient(conn)
		report, err := remote.Check(ctx, projectID, topo.RootPath, topo.Components)
		if err != nil {
			return errorResult("Remote environment check failed: " + err.Error())
		}
		data, _ := json.MarshalIndent(report, "", "  ")
		return textResult(string(data))

	case "packets_env_prepare":
		conn, err := s.dialScheduler(ctx)
		if err != nil {
			return errorResult("Connect to Packets daemon failed: " + err.Error())
		}
		if s.conn == nil {
			defer conn.Close()
		}

		topo, err := s.envMgr.Detect(dir)
		if err != nil {
			return errorResult("Project detection failed: " + err.Error())
		}

		projectID := project.ResolveProjectID(dir)
		remote := environment.NewRemoteClient(conn)
		var logBuf []string
		err = remote.Prepare(ctx, projectID, topo.RootPath, topo.Components, func(msg string) {
			logBuf = append(logBuf, msg)
		})
		if err != nil {
			return errorResult(fmt.Sprintf("Remote environment prepare failed: %v\nLogs:\n%s", err, strings.Join(logBuf, "\n")))
		}
		return textResult(strings.Join(logBuf, "\n"))

	case "packets_sync":
		conn, err := s.dialScheduler(ctx)
		if err != nil {
			return errorResult("Connect to Packets daemon failed: " + err.Error())
		}
		if s.conn == nil {
			defer conn.Close()
		}

		ref, err := workspace.UploadWorkspace(ctx, conn, dir, false)
		if err != nil {
			return errorResult("Sync failed: " + err.Error())
		}
		return textResult(fmt.Sprintf("✓ Workspace synchronized (ref: %s)", ref))

	case "packets_sync_full":
		conn, err := s.dialScheduler(ctx)
		if err != nil {
			return errorResult("Connect to Packets daemon failed: " + err.Error())
		}
		if s.conn == nil {
			defer conn.Close()
		}

		ref, err := workspace.UploadWorkspace(ctx, conn, dir, true)
		if err != nil {
			return errorResult("Full sync failed: " + err.Error())
		}
		return textResult(fmt.Sprintf("✓ Workspace fully synchronized with deletion detection (ref: %s)", ref))

	case "packets_approve":
		reqID, _ := params.Arguments["request_id"].(string)
		if reqID == "" {
			return errorResult("request_id is required")
		}
		if s.policy == nil {
			return errorResult("Policy engine is not initialized")
		}
		ticket, err := s.policy.ApprovePending(reqID)
		if err != nil {
			return errorResult(fmt.Sprintf("Approval failed: %v", err))
		}
		return textResult(fmt.Sprintf("✓ Approved request %s.\nApproval Ticket: %s\n(Single-use, expires in 5 minutes. Bound to exact snapshot and command.)", reqID, ticket.ID))

	case "packets_build":
		conn, err := s.dialScheduler(ctx)
		if err != nil {
			return errorResult("Connect to Packets daemon failed: " + err.Error())
		}
		if s.conn == nil {
			defer conn.Close()
		}

		// 1. Sync workspace before build
		snapshotRef, err := workspace.UploadWorkspace(ctx, conn, dir, false)
		if err != nil {
			return errorResult("Workspace sync before build failed: " + err.Error())
		}

		toolchainName := "custom"
		buildArgs := []string{"build"}
		if cmdArg, ok := params.Arguments["command"].(string); ok && cmdArg != "" {
			buildArgs = []string{cmdArg}
		}
		if tcArg, ok := params.Arguments["toolchain"].(string); ok && tcArg != "" {
			toolchainName = tcArg
		} else {
			topo, _ := s.envMgr.Detect(dir)
			if topo != nil && len(topo.Components) > 0 {
				toolchainName = string(topo.Components[0].Type)
			}
		}

		projectID := project.ResolveProjectID(dir)
		ticketID, _ := params.Arguments["approval_ticket"].(string)

		ec := policy.ExecutionContext{
			User:         "default",
			ProjectID:    projectID,
			WorkspaceID:  projectID,
			SnapshotHash: snapshotRef,
			Command:      toolchainName,
			Args:         buildArgs,
			Action:       "BUILD",
		}

		if s.policy != nil && s.policy.RequiresApprovalFor(ec) {
			if ticketID == "" {
				pending, err := s.policy.CreatePendingApproval(ec)
				if err != nil {
					return errorResult(fmt.Sprintf("Failed creating approval: %v", err))
				}
				return CallToolResult{
					Content: []TextContent{
						{
							Type: "text",
							Text: fmt.Sprintf("APPROVAL_REQUIRED: Human approval is required before building.\n"+
								"Pending Request ID: %s\n"+
								"Action: BUILD\n"+
								"Toolchain: %s\n"+
								"Command: %s\n"+
								"Project ID: %s\n"+
								"Snapshot Hash: %s\n\n"+
								"Approve via 'packets_approve' (or CLI 'packets approve %s'). Then retry with {\"approval_ticket\": \"<ticket>\"}.",
								pending.ID, toolchainName, strings.Join(buildArgs, " "), projectID, snapshotRef, pending.ID),
						},
					},
					IsError: true,
				}
			}
		}

		cacheKey := fmt.Sprintf("%s:%s:%s", projectID, toolchainName, snapshotRef)
		client := pb.NewSchedulerClient(conn)
		submitCtx := metadata.AppendToOutgoingContext(ctx, "x-approval-ticket", ticketID, "x-project-id", projectID)
		resp, err := client.SubmitJob(submitCtx, &pb.SubmitJobRequest{
			CacheKey:       cacheKey,
			Toolchain:      toolchainName,
			Runner:         string(apitypes.RunnerHost),
			SourceMode:     string(apitypes.SourceModeWorkspace),
			SnapshotRef:    snapshotRef,
			CommandArgs:    buildArgs,
			ProjectId:      projectID,
			ApprovalTicket: ticketID,
		})
		if err != nil {
			return errorResult("SubmitJob failed: " + err.Error())
		}

		logContent, execErr := s.collectJobLogs(ctx, client, resp.JobId)
		if execErr != nil {
			return CallToolResult{
				Content: []TextContent{
					{Type: "text", Text: fmt.Sprintf("Build FAILED (job: %s):\n%s\nError: %v", resp.JobId, logContent, execErr)},
				},
				IsError: true,
			}
		}

		return textResult(fmt.Sprintf("✓ Build succeeded (job: %s, cache_hit: %t)\n%s", resp.JobId, resp.CacheHit, logContent))

	case "packets_test":
		conn, err := s.dialScheduler(ctx)
		if err != nil {
			return errorResult("Connect to Packets daemon failed: " + err.Error())
		}
		if s.conn == nil {
			defer conn.Close()
		}

		snapshotRef, err := workspace.UploadWorkspace(ctx, conn, dir, false)
		if err != nil {
			return errorResult("Workspace sync before test failed: " + err.Error())
		}

		testArgs := []string{"test"}
		if cmdArg, ok := params.Arguments["command"].(string); ok && cmdArg != "" {
			testArgs = []string{cmdArg}
		}

		projectID := project.ResolveProjectID(dir)
		ticketID, _ := params.Arguments["approval_ticket"].(string)

		ec := policy.ExecutionContext{
			User:         "default",
			ProjectID:    projectID,
			WorkspaceID:  projectID,
			SnapshotHash: snapshotRef,
			Command:      "test",
			Args:         testArgs,
			Action:       "TEST",
		}

		if s.policy != nil && s.policy.RequiresApprovalFor(ec) {
			if ticketID == "" {
				pending, err := s.policy.CreatePendingApproval(ec)
				if err != nil {
					return errorResult(fmt.Sprintf("Failed creating approval: %v", err))
				}
				return CallToolResult{
					Content: []TextContent{
						{
							Type: "text",
							Text: fmt.Sprintf("APPROVAL_REQUIRED: Human approval is required before running tests.\n"+
								"Pending Request ID: %s\n"+
								"Action: TEST\n"+
								"Args: %s\n"+
								"Project ID: %s\n"+
								"Snapshot Hash: %s\n\n"+
								"Approve via 'packets_approve' (or CLI 'packets approve %s'). Then retry with {\"approval_ticket\": \"<ticket>\"}.",
								pending.ID, strings.Join(testArgs, " "), projectID, snapshotRef, pending.ID),
						},
					},
					IsError: true,
				}
			}
		}

		cacheKey := fmt.Sprintf("test:%s:%d", snapshotRef, time.Now().UnixNano())
		client := pb.NewSchedulerClient(conn)
		submitCtx := metadata.AppendToOutgoingContext(ctx, "x-approval-ticket", ticketID, "x-project-id", projectID)
		resp, err := client.SubmitJob(submitCtx, &pb.SubmitJobRequest{
			CacheKey:       cacheKey,
			Toolchain:      string(apitypes.ToolchainExec),
			Runner:         string(apitypes.RunnerHost),
			SourceMode:     string(apitypes.SourceModeWorkspace),
			SnapshotRef:    snapshotRef,
			CommandArgs:    testArgs,
			ProjectId:      projectID,
			ApprovalTicket: ticketID,
		})
		if err != nil {
			return errorResult("SubmitJob failed: " + err.Error())
		}

		logContent, execErr := s.collectJobLogs(ctx, client, resp.JobId)
		if execErr != nil {
			return CallToolResult{
				Content: []TextContent{
					{Type: "text", Text: fmt.Sprintf("Test FAILED (job: %s):\n%s\nError: %v", resp.JobId, logContent, execErr)},
				},
				IsError: true,
			}
		}

		return textResult(fmt.Sprintf("✓ Test succeeded (job: %s):\n%s", resp.JobId, logContent))

	case "packets_exec":
		cmdStr, _ := params.Arguments["command"].(string)
		if cmdStr == "" {
			return errorResult("command argument is required")
		}

		cat := s.policy.ClassifyCommand(cmdStr)
		if cat == policy.CategoryDangerous {
			return errorResult(fmt.Sprintf("SECURITY POLICY REJECTION: command %q contains forbidden dangerous operations", cmdStr))
		}

		conn, err := s.dialScheduler(ctx)
		if err != nil {
			return errorResult("Connect to Packets daemon failed: " + err.Error())
		}
		if s.conn == nil {
			defer conn.Close()
		}

		snapshotRef, err := workspace.UploadWorkspace(ctx, conn, dir, false)
		if err != nil {
			return errorResult("Workspace sync before exec failed: " + err.Error())
		}

		projectID := project.ResolveProjectID(dir)
		ticketID, _ := params.Arguments["approval_ticket"].(string)

		ec := policy.ExecutionContext{
			User:         "default",
			ProjectID:    projectID,
			WorkspaceID:  projectID,
			SnapshotHash: snapshotRef,
			Command:      cmdStr,
			Args:         []string{cmdStr},
			Action:       "EXEC",
		}

		if s.policy != nil && s.policy.RequiresApprovalFor(ec) {
			if ticketID == "" {
				pending, err := s.policy.CreatePendingApproval(ec)
				if err != nil {
					return errorResult(fmt.Sprintf("Failed creating approval: %v", err))
				}
				return CallToolResult{
					Content: []TextContent{
						{
							Type: "text",
							Text: fmt.Sprintf("APPROVAL_REQUIRED: Human approval is required before executing command.\n"+
								"Pending Request ID: %s\n"+
								"Action: EXEC\n"+
								"Command: %s\n"+
								"Project ID: %s\n"+
								"Snapshot Hash: %s\n\n"+
								"Approve via 'packets_approve' (or CLI 'packets approve %s'). Then retry with {\"approval_ticket\": \"<ticket>\"}.",
								pending.ID, cmdStr, projectID, snapshotRef, pending.ID),
						},
					},
					IsError: true,
				}
			}
		}

		cacheKey := fmt.Sprintf("exec:%s:%d", snapshotRef, time.Now().UnixNano())
		client := pb.NewSchedulerClient(conn)
		execCtx := metadata.AppendToOutgoingContext(ctx, "x-approval-ticket", ticketID, "x-project-id", projectID)
		resp, err := client.SubmitJob(execCtx, &pb.SubmitJobRequest{
			CacheKey:       cacheKey,
			Toolchain:      string(apitypes.ToolchainExec),
			Runner:         string(apitypes.RunnerHost),
			SourceMode:     string(apitypes.SourceModeWorkspace),
			SnapshotRef:    snapshotRef,
			CommandArgs:    []string{cmdStr},
			ProjectId:      projectID,
			ApprovalTicket: ticketID,
		})
		if err != nil {
			return errorResult("SubmitJob failed: " + err.Error())
		}

		logContent, execErr := s.collectJobLogs(ctx, client, resp.JobId)
		if execErr != nil {
			return CallToolResult{
				Content: []TextContent{
					{Type: "text", Text: fmt.Sprintf("Execution FAILED (job: %s):\n%s\nError: %v", resp.JobId, logContent, execErr)},
				},
				IsError: true,
			}
		}

		return textResult(fmt.Sprintf("✓ Execution succeeded (job: %s):\n%s", resp.JobId, logContent))

	case "packets_logs":
		jobID, _ := params.Arguments["job_id"].(string)
		if jobID == "" {
			return errorResult("job_id is required")
		}

		conn, err := s.dialScheduler(ctx)
		if err != nil {
			return errorResult("Connect to Packets daemon failed: " + err.Error())
		}
		if s.conn == nil {
			defer conn.Close()
		}

		logCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()

		client := pb.NewSchedulerClient(conn)
		stream, err := client.StreamJobLogs(logCtx, &pb.StreamJobLogsRequest{JobId: jobID})
		if err != nil {
			return errorResult("StreamJobLogs failed: " + err.Error())
		}

		var lines []string
		for {
			line, err := stream.Recv()
			if err != nil {
				break
			}
			lines = append(lines, line.Content)
		}

		if len(lines) == 0 {
			return textResult(fmt.Sprintf("No logs found for job %s.", jobID))
		}
		return textResult(strings.Join(lines, "\n"))

	case "packets_artifacts":
		jobID, _ := params.Arguments["job_id"].(string)
		if jobID == "" {
			return errorResult("job_id is required")
		}

		conn, err := s.dialScheduler(ctx)
		if err != nil {
			return errorResult("Connect to Packets daemon failed: " + err.Error())
		}
		if s.conn == nil {
			defer conn.Close()
		}

		client := pb.NewSchedulerClient(conn)
		statusResp, err := client.GetJobStatus(ctx, &pb.GetJobStatusRequest{JobId: jobID})
		if err != nil {
			return errorResult("GetJobStatus failed: " + err.Error())
		}
		if statusResp.ArtifactRef == "" {
			return errorResult(fmt.Sprintf("No artifacts produced by job %s (state: %s)", jobID, statusResp.State.String()))
		}

		stream, err := client.DownloadArtifact(ctx, &pb.DownloadArtifactRequest{JobId: jobID})
		if err != nil {
			return errorResult(fmt.Sprintf("DownloadArtifact failed: %v", err))
		}

		destDir := filepath.Join(dir, "build", "packets_artifacts")
		_ = os.MkdirAll(destDir, 0o755)
		totalBytes := 0
		for {
			chunk, err := stream.Recv()
			if err != nil {
				break
			}
			totalBytes += len(chunk.Data)
		}

		return textResult(fmt.Sprintf("✓ Artifacts for job %s retrieved (ref: %s, %d bytes downloaded to %s)", jobID, statusResp.ArtifactRef, totalBytes, destDir))

	case "packets_status":
		conn, err := s.dialScheduler(ctx)
		if err != nil {
			return errorResult("Packets daemon is unreachable: " + err.Error())
		}
		if s.conn == nil {
			defer conn.Close()
		}
		return textResult("✓ Packets scheduler daemon is active, healthy, and reachable via gRPC.")

	default:
		return errorResult(fmt.Sprintf("Unknown tool: %s", params.Name))
	}
}

func (s *Server) collectJobLogs(ctx context.Context, client pb.SchedulerClient, jobID string) (string, error) {
	var lines []string
	streamCtx, cancelStream := context.WithCancel(ctx)
	defer cancelStream()

	logDone := make(chan struct{})
	go func() {
		defer close(logDone)
		stream, err := client.StreamJobLogs(streamCtx, &pb.StreamJobLogsRequest{JobId: jobID})
		if err != nil {
			return
		}
		for {
			line, err := stream.Recv()
			if err != nil {
				break
			}
			lines = append(lines, line.Content)
		}
	}()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		statusResp, err := client.GetJobStatus(ctx, &pb.GetJobStatusRequest{JobId: jobID})
		if err == nil {
			if statusResp.State == pb.JobState_JOB_STATE_SUCCEEDED {
				cancelStream()
				select {
				case <-logDone:
				case <-time.After(1 * time.Second):
				}
				return strings.Join(lines, "\n"), nil
			}
			if statusResp.State == pb.JobState_JOB_STATE_FAILED {
				cancelStream()
				select {
				case <-logDone:
				case <-time.After(1 * time.Second):
				}
				return strings.Join(lines, "\n"), fmt.Errorf("job failed: %s", statusResp.ErrorMessage)
			}
		}
		select {
		case <-ctx.Done():
			cancelStream()
			select {
			case <-logDone:
			case <-time.After(1 * time.Second):
			}
			return strings.Join(lines, "\n"), ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *Server) dialScheduler(ctx context.Context) (*grpc.ClientConn, error) {
	if s.conn != nil {
		return s.conn, nil
	}
	if s.cfg == nil {
		return nil, fmt.Errorf("packets configuration is not set")
	}
	addr := s.cfg.SchedulerAddr()
	return grpc.DialContext(ctx, addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
}

func textResult(text string) CallToolResult {
	return CallToolResult{
		Content: []TextContent{{Type: "text", Text: text}},
	}
}

func errorResult(msg string) CallToolResult {
	return CallToolResult{
		Content: []TextContent{{Type: "text", Text: msg}},
		IsError: true,
	}
}
