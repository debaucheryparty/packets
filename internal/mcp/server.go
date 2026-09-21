package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/debaucheryparty/packets/internal/android"
	"github.com/debaucheryparty/packets/internal/config"
	"github.com/debaucheryparty/packets/internal/environment"
	"github.com/debaucheryparty/packets/internal/policy"
	"github.com/debaucheryparty/packets/internal/project"
	"github.com/debaucheryparty/packets/internal/workspace"
	"github.com/debaucheryparty/packets/pkg/apitypes"
	"github.com/debaucheryparty/packets/pkg/devfile"
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
	Meta      struct {
		ProgressToken interface{} `json:"progressToken"`
	} `json:"_meta"`
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
	writer     io.Writer
	writeMu    sync.Mutex
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
	s.writer = w
	scanner := bufio.NewScanner(r)
	encoder := json.NewEncoder(w)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var req JSONRPCRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			s.writeMu.Lock()
			_ = encoder.Encode(JSONRPCResponse{
				JSONRPC: "2.0",
				Error:   &JSONRPCError{Code: -32700, Message: "Parse error"},
			})
			s.writeMu.Unlock()
			continue
		}

		resp := s.handleRequest(req)
		if resp != nil {
			s.writeMu.Lock()
			if err := encoder.Encode(resp); err != nil {
				s.writeMu.Unlock()
				return err
			}
			s.writeMu.Unlock()
		}
	}
	return scanner.Err()
}

func (s *Server) SetWriter(w io.Writer) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.writer = w
}

func (s *Server) SendNotification(method string, params interface{}) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if s.writer == nil {
		return nil
	}
	notif := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	}
	return json.NewEncoder(s.writer).Encode(notif)
}

func (s *Server) SendProgress(token interface{}, progress, total float64, message string) {
	if token == nil {
		return
	}
	p := map[string]interface{}{
		"progressToken": token,
		"progress":      progress,
		"message":       message,
	}
	if total > 0 {
		p["total"] = total
	}
	_ = s.SendNotification("notifications/progress", p)
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
			Name:        "packets_pull",
			Description: "Pull build outputs, generated files, and artifacts from the remote persistent workspace into the local workspace",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"dir":    {Type: "string", Description: "Project destination directory"},
					"job_id": {Type: "string", Description: "Optional specific job ID to pull artifacts from"},
				},
			},
		},
		{
			Name:        "packets_status",
			Description: "Check status of the Packets remote daemon and active jobs",
			InputSchema: ToolSchema{Type: "object", Properties: map[string]PropertyDef{}},
		},
		{
			Name:        "packets_subspace_create",
			Description: "Create a persistent remote Subspace development environment",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"dir":        {Type: "string", Description: "Project directory path"},
					"project_id": {Type: "string", Description: "Optional project identifier"},
					"worker_id":  {Type: "string", Description: "Target worker node ID (defaults to local)"},
					"env_id":     {Type: "string", Description: "Environment profile identifier"},
				},
			},
		},
		{
			Name:        "packets_subspace_list",
			Description: "List all persistent remote Subspaces",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"project_id": {Type: "string", Description: "Filter by project ID"},
					"owner_id":   {Type: "string", Description: "Filter by owner ID"},
				},
			},
		},
		{
			Name:        "packets_subspace_status",
			Description: "Inspect details, environment, and health of a Subspace",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"subspace_id": {Type: "string", Description: "Subspace ID to inspect"},
				},
				Required: []string{"subspace_id"},
			},
		},
		{
			Name:        "packets_subspace_destroy",
			Description: "Destroy a persistent remote Subspace and release its resources",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"subspace_id": {Type: "string", Description: "Subspace ID to destroy"},
				},
				Required: []string{"subspace_id"},
			},
		},
		{
			Name:        "packets_subspace_sleep",
			Description: "Put a persistent remote Subspace into sleeping state to conserve remote compute",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"subspace_id": {Type: "string", Description: "Subspace ID to sleep"},
				},
				Required: []string{"subspace_id"},
			},
		},
		{
			Name:        "packets_subspace_wake",
			Description: "Wake a sleeping remote Subspace and restore its environment to ready state",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"subspace_id": {Type: "string", Description: "Subspace ID to wake"},
				},
				Required: []string{"subspace_id"},
			},
		},
		{
			Name:        "packets_service_list",
			Description: "List running services in a remote Subspace",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"subspace_id": {Type: "string", Description: "Subspace ID"},
				},
				Required: []string{"subspace_id"},
			},
		},
		{
			Name:        "packets_service_start",
			Description: "Start a background service in a remote Subspace",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"subspace_id": {Type: "string", Description: "Subspace ID"},
					"name":        {Type: "string", Description: "Service name (e.g. postgres, redis)"},
					"image":       {Type: "string", Description: "Container image or command"},
					"driver":      {Type: "string", Description: "Driver (process, docker)"},
				},
				Required: []string{"subspace_id", "name"},
			},
		},
		{
			Name:        "packets_service_stop",
			Description: "Stop a background service in a remote Subspace",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"subspace_id": {Type: "string", Description: "Subspace ID"},
					"name":        {Type: "string", Description: "Service name or ID"},
				},
				Required: []string{"subspace_id", "name"},
			},
		},
		{
			Name:        "packets_transaction_create",
			Description: "Open a safe workspace modification transaction snapshotting clean state before AI modifications",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"dir":         {Type: "string", Description: "Project directory path"},
					"project_id":  {Type: "string", Description: "Optional project identifier"},
					"subspace_id": {Type: "string", Description: "Optional Subspace identifier"},
					"description": {Type: "string", Description: "Transaction intent description"},
				},
			},
		},
		{
			Name:        "packets_transaction_status",
			Description: "Inspect the status and snapshot details of a workspace transaction",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"id": {Type: "string", Description: "Transaction ID"},
				},
				Required: []string{"id"},
			},
		},
		{
			Name:        "packets_transaction_commit",
			Description: "Commit an open workspace transaction after successful build and verification",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"id":                   {Type: "string", Description: "Transaction ID"},
					"working_snapshot_ref": {Type: "string", Description: "Optional snapshot reference to commit"},
				},
				Required: []string{"id"},
			},
		},
		{
			Name:        "packets_transaction_rollback",
			Description: "Rollback a failed workspace transaction restoring workspace to base snapshot",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"id": {Type: "string", Description: "Transaction ID"},
				},
				Required: []string{"id"},
			},
		},
		{
			Name:        "packets_devfile_get",
			Description: "Read and parse the project's packets.yaml devfile specification",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"dir": {Type: "string", Description: "Project directory containing packets.yaml"},
				},
			},
		},
		{
			Name:        "packets_devfile_validate",
			Description: "Validate a packets.yaml devfile or content for syntax, ports, and service definitions",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"dir":     {Type: "string", Description: "Project directory containing packets.yaml"},
					"content": {Type: "string", Description: "Optional raw YAML content to validate"},
				},
			},
		},
		{
			Name:        "packets_android_devices",
			Description: "List connected Android physical devices and running emulators on host or Subspace",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"subspace_id": {Type: "string", Description: "Optional remote Subspace ID to query"},
				},
			},
		},
		{
			Name:        "packets_android_screenshot",
			Description: "Capture a screenshot from an Android device or emulator display",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"dir":         {Type: "string", Description: "Project or destination directory"},
					"serial":      {Type: "string", Description: "Target device serial"},
					"output_path": {Type: "string", Description: "Output file path (default: screenshot.png)"},
					"subspace_id": {Type: "string", Description: "Optional remote Subspace ID"},
				},
			},
		},
		{
			Name:        "packets_android_run",
			Description: "Build, install, and launch an Android application on target device or emulator",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"dir":             {Type: "string", Description: "Android project directory"},
					"serial":          {Type: "string", Description: "Target device serial"},
					"package":         {Type: "string", Description: "Application package name to launch"},
					"activity":        {Type: "string", Description: "Activity to launch (default: .MainActivity)"},
					"variant":         {Type: "string", Description: "Build variant (default: debug)"},
					"subspace_id":     {Type: "string", Description: "Optional remote Subspace ID"},
					"approval_ticket": {Type: "string", Description: "Approval ticket ID from human approval"},
				},
			},
		},
		{
			Name:        "packets_android_logcat",
			Description: "Retrieve recent logcat logs from target Android device or emulator",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"serial":      {Type: "string", Description: "Target device serial"},
					"package":     {Type: "string", Description: "Filter logs by application package"},
					"tag":         {Type: "string", Description: "Filter logs by tag"},
					"lines":       {Type: "integer", Description: "Number of recent lines to retrieve (default: 100)"},
					"subspace_id": {Type: "string", Description: "Optional remote Subspace ID"},
				},
			},
		},
		{
			Name:        "packets_android_install",
			Description: "Install an APK artifact onto target Android device or emulator",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"apk_path":        {Type: "string", Description: "Path to APK file to install"},
					"serial":          {Type: "string", Description: "Target device serial"},
					"subspace_id":     {Type: "string", Description: "Optional remote Subspace ID"},
					"approval_ticket": {Type: "string", Description: "Approval ticket ID from human approval"},
				},
				Required: []string{"apk_path"},
			},
		},
		{
			Name:        "packets_android_shell",
			Description: "Execute a one-shot ADB shell command on target Android device or emulator",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"command":         {Type: "string", Description: "Shell command to run on device"},
					"serial":          {Type: "string", Description: "Target device serial"},
					"subspace_id":     {Type: "string", Description: "Optional remote Subspace ID"},
					"approval_ticket": {Type: "string", Description: "Approval ticket ID from human approval"},
				},
				Required: []string{"command"},
			},
		},
		{
			Name:        "packets_android_bundle_build",
			Description: "Build an Android App Bundle (.aab) remotely",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"dir":             {Type: "string", Description: "Android project directory"},
					"variant":         {Type: "string", Description: "Build variant (default: release)"},
					"subspace_id":     {Type: "string", Description: "Optional remote Subspace ID"},
					"approval_ticket": {Type: "string", Description: "Approval ticket ID from human approval"},
				},
			},
		},
		{
			Name:        "packets_android_bundle_to_apks",
			Description: "Convert an Android App Bundle (.aab) to an .apks archive or extract universal APK",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"aab_path":    {Type: "string", Description: "Path to input .aab file"},
					"output_path": {Type: "string", Description: "Destination path for .apks or universal .apk"},
					"universal":   {Type: "boolean", Description: "Generate a standalone universal APK"},
					"serial":      {Type: "string", Description: "Target device serial to optimize APK splits"},
				},
				Required: []string{"aab_path"},
			},
		},
		{
			Name:        "packets_android_sign",
			Description: "Sign an Android APK or App Bundle (.aab)",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"file_path":         {Type: "string", Description: "Path to APK or AAB file to sign"},
					"keystore_path":     {Type: "string", Description: "Path to keystore file"},
					"keystore_password": {Type: "string", Description: "Keystore password"},
					"key_alias":         {Type: "string", Description: "Key alias"},
					"key_password":      {Type: "string", Description: "Key password"},
					"output_path":       {Type: "string", Description: "Output path for signed artifact (default: overwrite input)"},
					"approval_ticket":   {Type: "string", Description: "Approval ticket ID from human approval"},
				},
				Required: []string{"file_path"},
			},
		},
		{
			Name:        "packets_android_verify",
			Description: "Verify the digital signature of an Android APK or App Bundle (.aab)",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"file_path": {Type: "string", Description: "Path to APK or AAB file to verify"},
				},
				Required: []string{"file_path"},
			},
		},
		{
			Name:        "packets_android_keystore_gen",
			Description: "Generate a new cryptographic keystore for Android signing",
			InputSchema: ToolSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"path":            {Type: "string", Description: "Output path for the generated keystore"},
					"alias":           {Type: "string", Description: "Key alias (default: release)"},
					"password":        {Type: "string", Description: "Keystore and key password"},
					"validity_days":   {Type: "number", Description: "Validity in days (default: 10000)"},
					"dname":           {Type: "string", Description: "Distinguished name for certificate"},
					"approval_ticket": {Type: "string", Description: "Approval ticket ID from human approval"},
				},
				Required: []string{"path", "password"},
			},
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
			defer func() { _ = conn.Close() }()
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
			defer func() { _ = conn.Close() }()
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
			defer func() { _ = conn.Close() }()
		}

		ref, err := workspace.UploadWorkspace(ctx, conn, dir, false)
		if err != nil {
			return errorResult("Sync failed: " + err.Error())
		}
		return textResult(fmt.Sprintf("Workspace synchronized (ref: %s)", ref))

	case "packets_sync_full":
		conn, err := s.dialScheduler(ctx)
		if err != nil {
			return errorResult("Connect to Packets daemon failed: " + err.Error())
		}
		if s.conn == nil {
			defer func() { _ = conn.Close() }()
		}

		ref, err := workspace.UploadWorkspace(ctx, conn, dir, true)
		if err != nil {
			return errorResult("Full sync failed: " + err.Error())
		}
		return textResult(fmt.Sprintf("Workspace fully synchronized with deletion detection (ref: %s)", ref))

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
		return textResult(fmt.Sprintf("Approved request %s.\nApproval Ticket: %s\n(Single-use, expires in 5 minutes. Bound to exact snapshot and command.)", reqID, ticket.ID))

	case "packets_build":
		conn, err := s.dialScheduler(ctx)
		if err != nil {
			return errorResult("Connect to Packets daemon failed: " + err.Error())
		}
		if s.conn == nil {
			defer func() { _ = conn.Close() }()
		}

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

		logContent, execErr := s.collectJobLogs(ctx, client, resp.JobId, params.Meta.ProgressToken)
		if execErr != nil {
			return CallToolResult{
				Content: []TextContent{
					{Type: "text", Text: fmt.Sprintf("Build FAILED (job: %s):\n%s\nError: %v", resp.JobId, logContent, execErr)},
				},
				IsError: true,
			}
		}

		return textResult(fmt.Sprintf("Build succeeded (job: %s, cache_hit: %t)\n%s", resp.JobId, resp.CacheHit, logContent))

	case "packets_test":
		conn, err := s.dialScheduler(ctx)
		if err != nil {
			return errorResult("Connect to Packets daemon failed: " + err.Error())
		}
		if s.conn == nil {
			defer func() { _ = conn.Close() }()
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

		toolchainName := string(apitypes.ToolchainExec)
		if tcArg, ok := params.Arguments["toolchain"].(string); ok && tcArg != "" {
			toolchainName = tcArg
		}

		cacheKey := fmt.Sprintf("test:%s:%d", snapshotRef, time.Now().UnixNano())
		client := pb.NewSchedulerClient(conn)
		submitCtx := metadata.AppendToOutgoingContext(ctx, "x-approval-ticket", ticketID, "x-project-id", projectID)
		resp, err := client.SubmitJob(submitCtx, &pb.SubmitJobRequest{
			CacheKey:       cacheKey,
			Toolchain:      toolchainName,
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

		logContent, execErr := s.collectJobLogs(ctx, client, resp.JobId, params.Meta.ProgressToken)
		if execErr != nil {
			return CallToolResult{
				Content: []TextContent{
					{Type: "text", Text: fmt.Sprintf("Test FAILED (job: %s):\n%s\nError: %v", resp.JobId, logContent, execErr)},
				},
				IsError: true,
			}
		}

		return textResult(fmt.Sprintf("Test succeeded (job: %s):\n%s", resp.JobId, logContent))

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
			defer func() { _ = conn.Close() }()
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

		logContent, execErr := s.collectJobLogs(ctx, client, resp.JobId, params.Meta.ProgressToken)
		if execErr != nil {
			return CallToolResult{
				Content: []TextContent{
					{Type: "text", Text: fmt.Sprintf("Execution FAILED (job: %s):\n%s\nError: %v", resp.JobId, logContent, execErr)},
				},
				IsError: true,
			}
		}

		return textResult(fmt.Sprintf("Execution succeeded (job: %s):\n%s", resp.JobId, logContent))

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
			defer func() { _ = conn.Close() }()
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
			defer func() { _ = conn.Close() }()
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
		if d, ok := params.Arguments["dir"].(string); ok && d != "" {
			destDir = d
		}

		var buf bytes.Buffer
		for {
			chunk, err := stream.Recv()
			if err != nil {
				break
			}
			buf.Write(chunk.Data)
		}

		if err := workspace.ExtractArtifact(buf.Bytes(), destDir, fmt.Sprintf("artifact_%s.bin", jobID)); err != nil {
			return errorResult(fmt.Sprintf("Extracting artifact failed: %v", err))
		}

		return textResult(fmt.Sprintf("Artifacts for job %s retrieved and extracted to %s (%d bytes)", jobID, destDir, buf.Len()))

	case "packets_pull":
		destDir := dir
		if d, ok := params.Arguments["dir"].(string); ok && d != "" {
			destDir = d
		}
		jobID, _ := params.Arguments["job_id"].(string)

		conn, err := s.dialScheduler(ctx)
		if err != nil {
			return errorResult("Connect to Packets daemon failed: " + err.Error())
		}
		if s.conn == nil {
			defer func() { _ = conn.Close() }()
		}

		client := pb.NewSchedulerClient(conn)

		if jobID == "" {
			projectID := project.ResolveProjectID(dir)
			jobID = projectID
		}

		stream, err := client.DownloadArtifact(ctx, &pb.DownloadArtifactRequest{JobId: jobID})
		if err != nil {
			return errorResult(fmt.Sprintf("DownloadArtifact failed: %v", err))
		}

		var buf bytes.Buffer
		for {
			chunk, err := stream.Recv()
			if err != nil {
				break
			}
			buf.Write(chunk.Data)
		}

		if buf.Len() == 0 {
			return errorResult(fmt.Sprintf("No artifacts found to pull for %s", jobID))
		}

		if err := workspace.ExtractArtifact(buf.Bytes(), destDir, fmt.Sprintf("artifact_%s.bin", jobID)); err != nil {
			return errorResult(fmt.Sprintf("Extracting pulled outputs failed: %v", err))
		}

		return textResult(fmt.Sprintf("Successfully pulled %d bytes of build outputs/artifacts to %s", buf.Len(), destDir))

	case "packets_status":
		conn, err := s.dialScheduler(ctx)
		if err != nil {
			return errorResult("Packets daemon is unreachable: " + err.Error())
		}
		if s.conn == nil {
			defer func() { _ = conn.Close() }()
		}
		return textResult("Packets scheduler daemon is active, healthy, and reachable via gRPC.")

	case "packets_subspace_create":
		conn, err := s.dialScheduler(ctx)
		if err != nil {
			return errorResult("Connect to Packets daemon failed: " + err.Error())
		}
		if s.conn == nil {
			defer func() { _ = conn.Close() }()
		}

		projID, _ := params.Arguments["project_id"].(string)
		if projID == "" {
			projID = project.ResolveProjectID(dir)
		}
		workerID, _ := params.Arguments["worker_id"].(string)
		envID, _ := params.Arguments["env_id"].(string)

		client := pb.NewSubspaceServiceClient(conn)
		resp, err := client.CreateSubspace(ctx, &pb.CreateSubspaceRequest{
			ProjectId:     projID,
			WorkerId:      workerID,
			EnvironmentId: envID,
		})
		if err != nil {
			return errorResult("Create subspace failed: " + err.Error())
		}
		data, _ := json.MarshalIndent(resp.Subspace, "", "  ")
		return textResult(string(data))

	case "packets_subspace_list":
		conn, err := s.dialScheduler(ctx)
		if err != nil {
			return errorResult("Connect to Packets daemon failed: " + err.Error())
		}
		if s.conn == nil {
			defer func() { _ = conn.Close() }()
		}
		projID, _ := params.Arguments["project_id"].(string)
		ownerID, _ := params.Arguments["owner_id"].(string)

		client := pb.NewSubspaceServiceClient(conn)
		resp, err := client.ListSubspaces(ctx, &pb.ListSubspacesRequest{
			ProjectId: projID,
			OwnerId:   ownerID,
		})
		if err != nil {
			return errorResult("List subspaces failed: " + err.Error())
		}
		data, _ := json.MarshalIndent(resp.Subspaces, "", "  ")
		return textResult(string(data))

	case "packets_subspace_status":
		conn, err := s.dialScheduler(ctx)
		if err != nil {
			return errorResult("Connect to Packets daemon failed: " + err.Error())
		}
		if s.conn == nil {
			defer func() { _ = conn.Close() }()
		}
		subID, _ := params.Arguments["subspace_id"].(string)
		if subID == "" {
			return errorResult("subspace_id is required")
		}

		client := pb.NewSubspaceServiceClient(conn)
		resp, err := client.GetSubspace(ctx, &pb.GetSubspaceRequest{Id: subID})
		if err != nil {
			return errorResult("Get subspace failed: " + err.Error())
		}
		data, _ := json.MarshalIndent(resp.Subspace, "", "  ")
		return textResult(string(data))

	case "packets_subspace_destroy":
		conn, err := s.dialScheduler(ctx)
		if err != nil {
			return errorResult("Connect to Packets daemon failed: " + err.Error())
		}
		if s.conn == nil {
			defer func() { _ = conn.Close() }()
		}
		subID, _ := params.Arguments["subspace_id"].(string)
		if subID == "" {
			return errorResult("subspace_id is required")
		}

		client := pb.NewSubspaceServiceClient(conn)
		_, err = client.DestroySubspace(ctx, &pb.DestroySubspaceRequest{Id: subID})
		if err != nil {
			return errorResult("Destroy subspace failed: " + err.Error())
		}
		return textResult(fmt.Sprintf("Subspace %s successfully destroyed", subID))

	case "packets_subspace_sleep":
		conn, err := s.dialScheduler(ctx)
		if err != nil {
			return errorResult("Connect to Packets daemon failed: " + err.Error())
		}
		if s.conn == nil {
			defer func() { _ = conn.Close() }()
		}
		subID, _ := params.Arguments["subspace_id"].(string)
		if subID == "" {
			return errorResult("subspace_id is required")
		}
		client := pb.NewSubspaceServiceClient(conn)
		resp, err := client.SleepSubspace(ctx, &pb.SleepSubspaceRequest{Id: subID})
		if err != nil {
			return errorResult("Sleep subspace failed: " + err.Error())
		}
		data, _ := json.MarshalIndent(resp.Subspace, "", "  ")
		return textResult(string(data))

	case "packets_subspace_wake":
		conn, err := s.dialScheduler(ctx)
		if err != nil {
			return errorResult("Connect to Packets daemon failed: " + err.Error())
		}
		if s.conn == nil {
			defer func() { _ = conn.Close() }()
		}
		subID, _ := params.Arguments["subspace_id"].(string)
		if subID == "" {
			return errorResult("subspace_id is required")
		}
		client := pb.NewSubspaceServiceClient(conn)
		resp, err := client.WakeSubspace(ctx, &pb.WakeSubspaceRequest{Id: subID})
		if err != nil {
			return errorResult("Wake subspace failed: " + err.Error())
		}
		data, _ := json.MarshalIndent(resp.Subspace, "", "  ")
		return textResult(string(data))

	case "packets_service_list":
		conn, err := s.dialScheduler(ctx)
		if err != nil {
			return errorResult("Connect to Packets daemon failed: " + err.Error())
		}
		if s.conn == nil {
			defer func() { _ = conn.Close() }()
		}
		subID, _ := params.Arguments["subspace_id"].(string)
		if subID == "" {
			return errorResult("subspace_id is required")
		}
		client := pb.NewRemoteServiceServiceClient(conn)
		resp, err := client.ListServices(ctx, &pb.ListServicesRequest{SubspaceId: subID})
		if err != nil {
			return errorResult("List services failed: " + err.Error())
		}
		data, _ := json.MarshalIndent(resp.Services, "", "  ")
		return textResult(string(data))

	case "packets_service_start":
		conn, err := s.dialScheduler(ctx)
		if err != nil {
			return errorResult("Connect to Packets daemon failed: " + err.Error())
		}
		if s.conn == nil {
			defer func() { _ = conn.Close() }()
		}
		subID, _ := params.Arguments["subspace_id"].(string)
		name, _ := params.Arguments["name"].(string)
		image, _ := params.Arguments["image"].(string)
		driver, _ := params.Arguments["driver"].(string)
		if subID == "" || name == "" {
			return errorResult("subspace_id and name are required")
		}
		client := pb.NewRemoteServiceServiceClient(conn)
		resp, err := client.StartService(ctx, &pb.StartServiceRequest{
			SubspaceId: subID,
			Name:       name,
			Image:      image,
			Driver:     driver,
		})
		if err != nil {
			return errorResult("Start service failed: " + err.Error())
		}
		data, _ := json.MarshalIndent(resp.Service, "", "  ")
		return textResult(string(data))

	case "packets_service_stop":
		conn, err := s.dialScheduler(ctx)
		if err != nil {
			return errorResult("Connect to Packets daemon failed: " + err.Error())
		}
		if s.conn == nil {
			defer func() { _ = conn.Close() }()
		}
		subID, _ := params.Arguments["subspace_id"].(string)
		name, _ := params.Arguments["name"].(string)
		if subID == "" || name == "" {
			return errorResult("subspace_id and name are required")
		}
		client := pb.NewRemoteServiceServiceClient(conn)
		resp, err := client.StopService(ctx, &pb.StopServiceRequest{
			SubspaceId: subID,
			NameOrId:   name,
		})
		if err != nil {
			return errorResult("Stop service failed: " + err.Error())
		}
		data, _ := json.MarshalIndent(resp.Service, "", "  ")
		return textResult(string(data))

	case "packets_transaction_create":
		conn, err := s.dialScheduler(ctx)
		if err != nil {
			return errorResult("Connect to Packets daemon failed: " + err.Error())
		}
		if s.conn == nil {
			defer func() { _ = conn.Close() }()
		}
		projID, _ := params.Arguments["project_id"].(string)
		if projID == "" {
			projID = project.ResolveProjectID(dir)
		}
		subspaceID, _ := params.Arguments["subspace_id"].(string)
		desc, _ := params.Arguments["description"].(string)

		snapRef, err := workspace.UploadWorkspace(ctx, conn, dir, false)
		if err != nil {
			return errorResult("Workspace sync before transaction failed: " + err.Error())
		}

		client := pb.NewTransactionServiceClient(conn)
		resp, err := client.CreateTransaction(ctx, &pb.CreateTransactionRequest{
			ProjectId:       projID,
			SubspaceId:      subspaceID,
			BaseSnapshotRef: snapRef,
			Description:     desc,
		})
		if err != nil {
			return errorResult("Create transaction failed: " + err.Error())
		}
		data, _ := json.MarshalIndent(resp.Transaction, "", "  ")
		return textResult(string(data))

	case "packets_transaction_status":
		conn, err := s.dialScheduler(ctx)
		if err != nil {
			return errorResult("Connect to Packets daemon failed: " + err.Error())
		}
		if s.conn == nil {
			defer func() { _ = conn.Close() }()
		}
		id, _ := params.Arguments["id"].(string)
		if id == "" {
			return errorResult("id is required")
		}
		client := pb.NewTransactionServiceClient(conn)
		resp, err := client.GetTransaction(ctx, &pb.GetTransactionRequest{Id: id})
		if err != nil {
			return errorResult("Get transaction failed: " + err.Error())
		}
		data, _ := json.MarshalIndent(resp.Transaction, "", "  ")
		return textResult(string(data))

	case "packets_transaction_commit":
		conn, err := s.dialScheduler(ctx)
		if err != nil {
			return errorResult("Connect to Packets daemon failed: " + err.Error())
		}
		if s.conn == nil {
			defer func() { _ = conn.Close() }()
		}
		id, _ := params.Arguments["id"].(string)
		if id == "" {
			return errorResult("id is required")
		}
		snapRef, _ := params.Arguments["working_snapshot_ref"].(string)
		if snapRef == "" {
			if sRef, err := workspace.UploadWorkspace(ctx, conn, dir, false); err == nil {
				snapRef = sRef
			}
		}
		client := pb.NewTransactionServiceClient(conn)
		resp, err := client.CommitTransaction(ctx, &pb.CommitTransactionRequest{
			Id:                 id,
			WorkingSnapshotRef: snapRef,
		})
		if err != nil {
			return errorResult("Commit transaction failed: " + err.Error())
		}
		data, _ := json.MarshalIndent(resp.Transaction, "", "  ")
		return textResult(string(data))

	case "packets_transaction_rollback":
		conn, err := s.dialScheduler(ctx)
		if err != nil {
			return errorResult("Connect to Packets daemon failed: " + err.Error())
		}
		if s.conn == nil {
			defer func() { _ = conn.Close() }()
		}
		id, _ := params.Arguments["id"].(string)
		if id == "" {
			return errorResult("id is required")
		}
		client := pb.NewTransactionServiceClient(conn)
		resp, err := client.RollbackTransaction(ctx, &pb.RollbackTransactionRequest{Id: id})
		if err != nil {
			return errorResult("Rollback transaction failed: " + err.Error())
		}
		data, _ := json.MarshalIndent(resp.Transaction, "", "  ")
		return textResult(string(data))

	case "packets_devfile_get":
		df, err := devfile.Load(dir)
		if err != nil {
			return errorResult("Failed to parse devfile: " + err.Error())
		}
		if df == nil {
			return textResult("No packets.yaml devfile found in " + dir)
		}
		data, err := json.MarshalIndent(df, "", "  ")
		if err != nil {
			return errorResult("Failed to serialize devfile: " + err.Error())
		}
		return textResult(string(data))

	case "packets_devfile_validate":
		content, _ := params.Arguments["content"].(string)
		if content != "" {
			df, err := devfile.Parse([]byte(content))
			if err != nil {
				return errorResult("Devfile validation failed: " + err.Error())
			}
			return textResult(fmt.Sprintf("Valid devfile: %s (services: %d, ports: %d)", df.Name, len(df.Services), len(df.Ports)))
		}

		path, found := devfile.Find(dir)
		if !found {
			return errorResult("No packets.yaml devfile found in " + dir)
		}
		df, err := devfile.ParseFile(path)
		if err != nil {
			return errorResult("Devfile validation failed for " + path + ": " + err.Error())
		}
		return textResult(fmt.Sprintf("Valid devfile at %s: %s (services: %d, ports: %d)", path, df.Name, len(df.Services), len(df.Ports)))

	case "packets_android_devices":
		subspaceID, _ := params.Arguments["subspace_id"].(string)
		if subspaceID != "" {
			conn, err := s.dialScheduler(ctx)
			if err != nil {
				return errorResult("Connect to Packets daemon failed: " + err.Error())
			}
			if s.conn == nil {
				defer func() { _ = conn.Close() }()
			}
			if err := s.validateSubspace(ctx, conn, subspaceID); err != nil {
				return errorResult(err.Error())
			}
			logs, err := s.execRemote(ctx, conn, "adb", "devices")
			if err != nil {
				return errorResult("Failed to query devices on subspace: " + err.Error())
			}
			devices := android.ParseDevicesOutput(logs)
			data, _ := json.MarshalIndent(devices, "", "  ")
			return textResult(string(data))
		}

		adb := android.NewExecADBClient()
		devices, err := adb.Devices(ctx)
		if err != nil || len(devices) == 0 {
			if conn, connErr := s.dialScheduler(ctx); connErr == nil {
				if s.conn == nil {
					defer func() { _ = conn.Close() }()
				}
				if logs, remErr := s.execRemote(ctx, conn, "adb", "devices"); remErr == nil {
					remDevices := android.ParseDevicesOutput(logs)
					if len(remDevices) > 0 {
						devices = remDevices
						err = nil
					}
				}
			}
		}
		if err != nil {
			return errorResult("Failed to query android devices: " + err.Error())
		}
		data, _ := json.MarshalIndent(devices, "", "  ")
		return textResult(string(data))

	case "packets_android_screenshot":
		subspaceID, _ := params.Arguments["subspace_id"].(string)
		serial, _ := params.Arguments["serial"].(string)
		outPath, _ := params.Arguments["output_path"].(string)
		if outPath == "" {
			outPath = "screenshot.png"
		}
		if !filepath.IsAbs(outPath) {
			outPath = filepath.Join(dir, outPath)
		}
		_ = os.MkdirAll(filepath.Dir(outPath), 0o755)

		if subspaceID == "" {
			cmdArgs := []string{}
			if serial != "" {
				cmdArgs = append(cmdArgs, "-s", serial)
			}
			cmdArgs = append(cmdArgs, "exec-out", "screencap", "-p")
			outFile, err := os.Create(outPath)
			if err == nil {
				cmd := exec.CommandContext(ctx, android.ResolveADBPath(), cmdArgs...)
				cmd.Stdout = outFile
				if runErr := cmd.Run(); runErr == nil {
					_ = outFile.Close()
					fi, statErr := os.Stat(outPath)
					if statErr == nil && fi.Size() > 0 {
						return textResult(fmt.Sprintf("Screenshot saved to %s (%d bytes)", outPath, fi.Size()))
					}
				}
				_ = outFile.Close()
			}
		}

		conn, err := s.dialScheduler(ctx)
		if err != nil {
			return errorResult("Connect to Packets daemon failed: " + err.Error())
		}
		if s.conn == nil {
			defer func() { _ = conn.Close() }()
		}
		if subspaceID != "" {
			if err := s.validateSubspace(ctx, conn, subspaceID); err != nil {
				return errorResult(err.Error())
			}
		}
		remoteSerial := ""
		if serial != "" {
			remoteSerial = "-s " + serial + " "
		}
		shCmd := fmt.Sprintf("adb %sexec-out screencap -p > /tmp/packets_screenshot.png", remoteSerial)
		if _, err := s.execRemote(ctx, conn, "bash", "-c", shCmd); err != nil {
			return errorResult("Failed capturing screenshot on remote node: " + err.Error())
		}
		return textResult(fmt.Sprintf("Screenshot captured on remote node at /tmp/packets_screenshot.png for device %s", serial))

	case "packets_android_run":
		projectID := project.ResolveProjectID(dir)
		ticketID, _ := params.Arguments["approval_ticket"].(string)
		pkgName, _ := params.Arguments["package"].(string)
		activity, _ := params.Arguments["activity"].(string)
		if activity == "" {
			activity = ".MainActivity"
		}
		variant, _ := params.Arguments["variant"].(string)
		if variant == "" {
			variant = "debug"
		}
		subspaceID, _ := params.Arguments["subspace_id"].(string)
		serial, _ := params.Arguments["serial"].(string)

		ec := policy.ExecutionContext{
			User:         "default",
			ProjectID:    projectID,
			WorkspaceID:  projectID,
			SnapshotHash: "",
			Command:      "android_run",
			Args:         []string{pkgName, activity, variant},
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
							Text: fmt.Sprintf("APPROVAL_REQUIRED: Human approval is required before running Android app.\n"+
								"Pending Request ID: %s\n"+
								"Action: EXEC\n"+
								"Target: %s/%s\n"+
								"Variant: %s\n"+
								"Project ID: %s\n\n"+
								"Approve via 'packets_approve' (or CLI 'packets approve %s'). Then retry with {\"approval_ticket\": \"<ticket>\"}.",
								pending.ID, pkgName, activity, variant, projectID, pending.ID),
						},
					},
					IsError: true,
				}
			}
		}

		conn, err := s.dialScheduler(ctx)
		if err != nil {
			return errorResult("Connect to Packets daemon failed: " + err.Error())
		}
		if s.conn == nil {
			defer func() { _ = conn.Close() }()
		}
		if subspaceID != "" {
			if err := s.validateSubspace(ctx, conn, subspaceID); err != nil {
				return errorResult(err.Error())
			}
		}

		snapshotRef, err := workspace.UploadWorkspace(ctx, conn, dir, false)
		if err != nil {
			return errorResult("Workspace sync before build failed: " + err.Error())
		}

		taskVariant := variant
		if len(taskVariant) > 0 {
			taskVariant = strings.ToUpper(taskVariant[:1]) + taskVariant[1:]
		}
		gradleTask := fmt.Sprintf("assemble%s", taskVariant)
		cacheKey := fmt.Sprintf("%s:android:%s", projectID, snapshotRef)
		client := pb.NewSchedulerClient(conn)
		submitCtx := metadata.AppendToOutgoingContext(ctx, "x-approval-ticket", ticketID, "x-project-id", projectID)
		resp, err := client.SubmitJob(submitCtx, &pb.SubmitJobRequest{
			CacheKey:       cacheKey,
			Toolchain:      string(apitypes.ToolchainAndroid),
			Runner:         string(apitypes.RunnerHost),
			SourceMode:     string(apitypes.SourceModeWorkspace),
			SnapshotRef:    snapshotRef,
			CommandArgs:    []string{gradleTask},
			ProjectId:      projectID,
			ApprovalTicket: ticketID,
		})
		if err != nil {
			return errorResult("Submit build job failed: " + err.Error())
		}
		logContent, execErr := s.collectJobLogs(ctx, client, resp.JobId, params.Meta.ProgressToken)
		if execErr != nil {
			return CallToolResult{
				Content: []TextContent{
					{Type: "text", Text: fmt.Sprintf("Android build FAILED (job: %s):\n%s\nError: %v", resp.JobId, logContent, execErr)},
				},
				IsError: true,
			}
		}

		apkPath, findErr := android.FindAPK(dir, variant)
		if findErr == nil {
			adb := android.NewExecADBClient()
			if installErr := adb.Install(ctx, serial, apkPath); installErr == nil {
				if pkgName != "" {
					target := pkgName + "/" + activity
					out, launchErr := adb.Shell(ctx, serial, "am", "start", "-n", target)
					if launchErr == nil {
						return textResult(fmt.Sprintf("Application built, installed, and launched successfully: %s\n%s", target, out))
					}
					return textResult(fmt.Sprintf("Application installed, but launch failed: %v\n%s", launchErr, out))
				}
				return textResult(fmt.Sprintf("Application built and installed from %s", apkPath))
			}
		}

		if subspaceID != "" {
			remoteSerial := ""
			if serial != "" {
				remoteSerial = "-s " + serial + " "
			}
			installCmd := fmt.Sprintf("adb %sinstall -r app/build/outputs/apk/%s/*.apk", remoteSerial, variant)
			_, _ = s.execRemote(ctx, conn, "bash", "-c", installCmd)
			if pkgName != "" {
				target := pkgName + "/" + activity
				shCmd := fmt.Sprintf("adb %sshell am start -n %s", remoteSerial, target)
				launchOut, _ := s.execRemote(ctx, conn, "bash", "-c", shCmd)
				return textResult(fmt.Sprintf("Application built, installed, and launched on subspace %s: %s\n%s", subspaceID, target, launchOut))
			}
		}
		return textResult(fmt.Sprintf("Android build completed (job: %s):\n%s", resp.JobId, logContent))

	case "packets_android_logcat":
		subspaceID, _ := params.Arguments["subspace_id"].(string)
		serial, _ := params.Arguments["serial"].(string)
		tag, _ := params.Arguments["tag"].(string)
		pkg, _ := params.Arguments["package"].(string)
		linesCount := 100
		if l, ok := params.Arguments["lines"].(float64); ok && l > 0 {
			linesCount = int(l)
		}

		logcatArgs := []string{"logcat", "-d", "-t", fmt.Sprintf("%d", linesCount)}
		if tag != "" {
			logcatArgs = append(logcatArgs, "-s", tag)
		}

		if subspaceID != "" {
			conn, err := s.dialScheduler(ctx)
			if err != nil {
				return errorResult("Connect to Packets daemon failed: " + err.Error())
			}
			if s.conn == nil {
				defer func() { _ = conn.Close() }()
			}
			if err := s.validateSubspace(ctx, conn, subspaceID); err != nil {
				return errorResult(err.Error())
			}
			remoteCmd := []string{"adb"}
			if serial != "" {
				remoteCmd = append(remoteCmd, "-s", serial)
			}
			remoteCmd = append(remoteCmd, logcatArgs...)
			out, err := s.execRemote(ctx, conn, remoteCmd...)
			if err != nil {
				return errorResult("Remote logcat failed: " + err.Error())
			}
			return textResult(out)
		}

		adbArgs := []string{}
		if serial != "" {
			adbArgs = append(adbArgs, "-s", serial)
		}
		adbArgs = append(adbArgs, logcatArgs...)
		cmd := exec.CommandContext(ctx, android.ResolveADBPath(), adbArgs...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			conn, connErr := s.dialScheduler(ctx)
			if connErr == nil {
				if s.conn == nil {
					defer func() { _ = conn.Close() }()
				}
				remoteCmd := []string{"adb"}
				if serial != "" {
					remoteCmd = append(remoteCmd, "-s", serial)
				}
				remoteCmd = append(remoteCmd, logcatArgs...)
				if remOut, remErr := s.execRemote(ctx, conn, remoteCmd...); remErr == nil {
					out = []byte(remOut)
					err = nil
				}
			}
		}
		if err != nil {
			return errorResult(fmt.Sprintf("ADB logcat failed: %v (%s)", err, string(out)))
		}
		res := string(out)
		if pkg != "" {
			var filtered []string
			for _, line := range strings.Split(res, "\n") {
				if strings.Contains(line, pkg) {
					filtered = append(filtered, line)
				}
			}
			res = strings.Join(filtered, "\n")
		}
		return textResult(res)

	case "packets_android_install":
		apkPath, _ := params.Arguments["apk_path"].(string)
		if apkPath == "" {
			return errorResult("apk_path is required")
		}
		serial, _ := params.Arguments["serial"].(string)
		subspaceID, _ := params.Arguments["subspace_id"].(string)
		ticketID, _ := params.Arguments["approval_ticket"].(string)
		projectID := project.ResolveProjectID(dir)

		ec := policy.ExecutionContext{
			User:         "default",
			ProjectID:    projectID,
			WorkspaceID:  projectID,
			SnapshotHash: "",
			Command:      "android_install",
			Args:         []string{apkPath, serial},
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
							Text: fmt.Sprintf("APPROVAL_REQUIRED: Human approval is required before installing APK.\n"+
								"Pending Request ID: %s\n"+
								"Action: EXEC\n"+
								"APK: %s\n"+
								"Serial: %s\n\n"+
								"Approve via 'packets_approve' (or CLI 'packets approve %s'). Then retry with {\"approval_ticket\": \"<ticket>\"}.",
								pending.ID, apkPath, serial, pending.ID),
						},
					},
					IsError: true,
				}
			}
		}

		adb := android.NewExecADBClient()
		if err := adb.Install(ctx, serial, apkPath); err == nil {
			return textResult(fmt.Sprintf("APK installed successfully on %s: %s", serial, apkPath))
		}

		conn, err := s.dialScheduler(ctx)
		if err != nil {
			return errorResult("Connect to Packets daemon failed: " + err.Error())
		}
		if s.conn == nil {
			defer func() { _ = conn.Close() }()
		}
		if subspaceID != "" {
			if err := s.validateSubspace(ctx, conn, subspaceID); err != nil {
				return errorResult(err.Error())
			}
		}
		installCmd := []string{"adb"}
		if serial != "" {
			installCmd = append(installCmd, "-s", serial)
		}
		installCmd = append(installCmd, "install", "-r", apkPath)
		logs, err := s.execRemote(ctx, conn, installCmd...)
		if err != nil {
			return errorResult("Failed to install APK: " + err.Error() + "\n" + logs)
		}
		return textResult(fmt.Sprintf("APK installed on remote node (serial: %s):\n%s", serial, logs))

	case "packets_android_shell":
		cmdStr, _ := params.Arguments["command"].(string)
		if cmdStr == "" {
			return errorResult("command is required")
		}
		serial, _ := params.Arguments["serial"].(string)
		subspaceID, _ := params.Arguments["subspace_id"].(string)
		ticketID, _ := params.Arguments["approval_ticket"].(string)
		projectID := project.ResolveProjectID(dir)

		ec := policy.ExecutionContext{
			User:         "default",
			ProjectID:    projectID,
			WorkspaceID:  projectID,
			SnapshotHash: "",
			Command:      "android_shell",
			Args:         []string{cmdStr, serial},
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
							Text: fmt.Sprintf("APPROVAL_REQUIRED: Human approval is required before running Android shell command.\n"+
								"Pending Request ID: %s\n"+
								"Action: EXEC\n"+
								"Command: %s\n"+
								"Serial: %s\n\n"+
								"Approve via 'packets_approve' (or CLI 'packets approve %s'). Then retry with {\"approval_ticket\": \"<ticket>\"}.",
								pending.ID, cmdStr, serial, pending.ID),
						},
					},
					IsError: true,
				}
			}
		}

		adb := android.NewExecADBClient()
		out, err := adb.Shell(ctx, serial, cmdStr)
		if err == nil {
			return textResult(out)
		}

		conn, connErr := s.dialScheduler(ctx)
		if connErr != nil {
			return errorResult("ADB shell failed: " + err.Error() + "\nConnect to daemon failed: " + connErr.Error())
		}
		if s.conn == nil {
			defer func() { _ = conn.Close() }()
		}
		if subspaceID != "" {
			if err := s.validateSubspace(ctx, conn, subspaceID); err != nil {
				return errorResult(err.Error())
			}
		}
		remoteCmd := []string{"adb"}
		if serial != "" {
			remoteCmd = append(remoteCmd, "-s", serial)
		}
		remoteCmd = append(remoteCmd, "shell", cmdStr)
		remOut, remErr := s.execRemote(ctx, conn, remoteCmd...)
		if remErr != nil {
			return errorResult("Remote ADB shell failed: " + remErr.Error() + "\n" + remOut)
		}
		return textResult(remOut)

	case "packets_android_bundle_build":
		variant, _ := params.Arguments["variant"].(string)
		if variant == "" {
			variant = "release"
		}
		subspaceID, _ := params.Arguments["subspace_id"].(string)
		ticketID, _ := params.Arguments["approval_ticket"].(string)
		projectID := project.ResolveProjectID(dir)

		taskVariant := variant
		if len(taskVariant) > 0 {
			taskVariant = strings.ToUpper(taskVariant[:1]) + taskVariant[1:]
		}
		gradleTask := fmt.Sprintf("bundle%s", taskVariant)

		ec := policy.ExecutionContext{
			User:         "default",
			ProjectID:    projectID,
			WorkspaceID:  projectID,
			SnapshotHash: "",
			Command:      "android_bundle_build",
			Args:         []string{gradleTask},
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
							Text: fmt.Sprintf("APPROVAL_REQUIRED: Human approval is required before building Android App Bundle.\n"+
								"Pending Request ID: %s\n"+
								"Action: BUILD\n"+
								"Task: %s\n"+
								"Project ID: %s\n\n"+
								"Approve via 'packets_approve' (or CLI 'packets approve %s'). Then retry with {\"approval_ticket\": \"<ticket>\"}.",
								pending.ID, gradleTask, projectID, pending.ID),
						},
					},
					IsError: true,
				}
			}
		}

		conn, err := s.dialScheduler(ctx)
		if err != nil {
			return errorResult("Connect to Packets daemon failed: " + err.Error())
		}
		if s.conn == nil {
			defer func() { _ = conn.Close() }()
		}
		if subspaceID != "" {
			if err := s.validateSubspace(ctx, conn, subspaceID); err != nil {
				return errorResult(err.Error())
			}
		}

		snapshotRef, err := workspace.UploadWorkspace(ctx, conn, dir, false)
		if err != nil {
			return errorResult("Workspace sync before bundle build failed: " + err.Error())
		}

		artifactGlob := fmt.Sprintf("app/build/outputs/bundle/%s/*.aab", variant)
		cacheKey := fmt.Sprintf("%s:android-bundle:%s", projectID, snapshotRef)
		client := pb.NewSchedulerClient(conn)
		submitCtx := metadata.AppendToOutgoingContext(ctx, "x-approval-ticket", ticketID, "x-project-id", projectID)
		resp, err := client.SubmitJob(submitCtx, &pb.SubmitJobRequest{
			CacheKey:       cacheKey,
			Toolchain:      string(apitypes.ToolchainAndroid),
			Runner:         string(apitypes.RunnerDocker),
			SourceMode:     string(apitypes.SourceModeWorkspace),
			SnapshotRef:    snapshotRef,
			CommandArgs:    []string{gradleTask},
			ArtifactPaths:  []string{artifactGlob},
			ProjectId:      projectID,
			ApprovalTicket: ticketID,
		})
		if err != nil {
			return errorResult("Submit bundle build job failed: " + err.Error())
		}
		logContent, execErr := s.collectJobLogs(ctx, client, resp.JobId, params.Meta.ProgressToken)
		if execErr != nil {
			return CallToolResult{
				Content: []TextContent{
					{Type: "text", Text: fmt.Sprintf("Android App Bundle build FAILED (job: %s):\n%s\nError: %v", resp.JobId, logContent, execErr)},
				},
				IsError: true,
			}
		}
		aabPath, _ := android.FindAAB(dir, variant)
		return textResult(fmt.Sprintf("Android App Bundle build succeeded (job: %s):\n%s\nAAB Path: %s", resp.JobId, logContent, aabPath))

	case "packets_android_bundle_to_apks":
		aabPath, _ := params.Arguments["aab_path"].(string)
		if aabPath == "" {
			return errorResult("aab_path is required")
		}
		outPath, _ := params.Arguments["output_path"].(string)
		if outPath == "" {
			ext := filepath.Ext(aabPath)
			outPath = strings.TrimSuffix(aabPath, ext) + ".apks"
		}
		universal, _ := params.Arguments["universal"].(bool)
		serial, _ := params.Arguments["serial"].(string)

		bt := android.NewExecBundletool()
		if err := bt.BuildAPKs(ctx, aabPath, outPath, universal, serial); err != nil {
			return errorResult(fmt.Sprintf("Failed building APKs from bundle: %v", err))
		}
		fi, _ := os.Stat(outPath)
		var size int64
		if fi != nil {
			size = fi.Size()
		}
		return textResult(fmt.Sprintf("APKs generated from bundle: %s (%d bytes)", outPath, size))

	case "packets_android_sign":
		filePath, _ := params.Arguments["file_path"].(string)
		if filePath == "" {
			return errorResult("file_path is required")
		}
		ksPath, _ := params.Arguments["keystore_path"].(string)
		ksPass, _ := params.Arguments["keystore_password"].(string)
		keyAlias, _ := params.Arguments["key_alias"].(string)
		keyPass, _ := params.Arguments["key_password"].(string)
		outPath, _ := params.Arguments["output_path"].(string)
		ticketID, _ := params.Arguments["approval_ticket"].(string)
		projectID := project.ResolveProjectID(dir)

		ec := policy.ExecutionContext{
			User:         "default",
			ProjectID:    projectID,
			WorkspaceID:  projectID,
			SnapshotHash: "",
			Command:      "android_sign",
			Args:         []string{filePath},
			Action:       "SIGN",
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
							Text: fmt.Sprintf("APPROVAL_REQUIRED: Human approval is required before signing Android artifact.\n"+
								"Pending Request ID: %s\n"+
								"Action: SIGN\n"+
								"File: %s\n"+
								"Project ID: %s\n\n"+
								"Approve via 'packets_approve' (or CLI 'packets approve %s'). Then retry with {\"approval_ticket\": \"<ticket>\"}.",
								pending.ID, filePath, projectID, pending.ID),
						},
					},
					IsError: true,
				}
			}
		}

		signCfg, err := android.ResolveSigningConfig(ksPath, ksPass, keyAlias, keyPass)
		if err != nil {
			return errorResult(fmt.Sprintf("Signing configuration error: %v", err))
		}

		ext := strings.ToLower(filepath.Ext(filePath))
		if ext == ".aab" {
			target := filePath
			if outPath != "" && outPath != filePath {
				data, err := os.ReadFile(filePath)
				if err != nil {
					return errorResult(fmt.Sprintf("Failed reading input bundle: %v", err))
				}
				if err := os.WriteFile(outPath, data, 0o644); err != nil {
					return errorResult(fmt.Sprintf("Failed writing output bundle: %v", err))
				}
				target = outPath
			}
			if err := android.SignAAB(ctx, target, signCfg); err != nil {
				return errorResult(fmt.Sprintf("Failed signing App Bundle: %v", err))
			}
			return textResult(fmt.Sprintf("Successfully signed Android App Bundle: %s", target))
		}

		if outPath == "" {
			outPath = filePath
		}
		if err := android.SignAPK(ctx, filePath, outPath, signCfg); err != nil {
			return errorResult(fmt.Sprintf("Failed signing APK: %v", err))
		}
		return textResult(fmt.Sprintf("Successfully signed Android APK: %s", outPath))

	case "packets_android_verify":
		filePath, _ := params.Arguments["file_path"].(string)
		if filePath == "" {
			return errorResult("file_path is required")
		}

		ext := strings.ToLower(filepath.Ext(filePath))
		if ext == ".aab" {
			if err := android.VerifyAAB(ctx, filePath); err != nil {
				return errorResult(fmt.Sprintf("App Bundle verification failed: %v", err))
			}
			return textResult(fmt.Sprintf("App Bundle verification succeeded for %s (valid jarsigner signature)", filePath))
		}

		res, err := android.VerifyAPK(ctx, filePath)
		if err != nil {
			return errorResult(fmt.Sprintf("APK verification failed: %v\nRaw log:\n%s", err, res.RawLog))
		}

		msg := fmt.Sprintf("APK verification succeeded for %s\n"+
			"v1 scheme (JAR signing): %t\n"+
			"v2 scheme (APK Signature v2): %t\n"+
			"v3 scheme (APK Signature v3): %t\n"+
			"v4 scheme (APK Signature v4): %t",
			filePath, res.V1Scheme, res.V2Scheme, res.V3Scheme, res.V4Scheme)
		if len(res.Signers) > 0 {
			msg += "\nSigners:\n" + strings.Join(res.Signers, "\n")
		}
		return textResult(msg)

	case "packets_android_keystore_gen":
		path, _ := params.Arguments["path"].(string)
		if path == "" {
			return errorResult("path is required")
		}
		alias, _ := params.Arguments["alias"].(string)
		if alias == "" {
			alias = "release"
		}
		password, _ := params.Arguments["password"].(string)
		if password == "" {
			return errorResult("password is required")
		}
		validityDays := 10000
		if vd, ok := params.Arguments["validity_days"].(float64); ok && vd > 0 {
			validityDays = int(vd)
		}
		dname, _ := params.Arguments["dname"].(string)
		ticketID, _ := params.Arguments["approval_ticket"].(string)
		projectID := project.ResolveProjectID(dir)

		ec := policy.ExecutionContext{
			User:         "default",
			ProjectID:    projectID,
			WorkspaceID:  projectID,
			SnapshotHash: "",
			Command:      "android_keystore_gen",
			Args:         []string{path, alias},
			Action:       "KEYSTORE_GEN",
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
							Text: fmt.Sprintf("APPROVAL_REQUIRED: Human approval is required before generating keystore.\n"+
								"Pending Request ID: %s\n"+
								"Action: KEYSTORE_GEN\n"+
								"Path: %s\n"+
								"Alias: %s\n"+
								"Project ID: %s\n\n"+
								"Approve via 'packets_approve' (or CLI 'packets approve %s'). Then retry with {\"approval_ticket\": \"<ticket>\"}.",
								pending.ID, path, alias, projectID, pending.ID),
						},
					},
					IsError: true,
				}
			}
		}

		opts := android.KeystoreGenOpts{
			Path:         path,
			Alias:        alias,
			Password:     password,
			ValidityDays: validityDays,
			DName:        dname,
		}
		if err := android.GenerateKeystore(ctx, opts); err != nil {
			return errorResult(fmt.Sprintf("Failed generating keystore: %v", err))
		}
		return textResult(fmt.Sprintf("Keystore generated successfully at %s (alias: %s)", path, alias))

	default:
		return errorResult(fmt.Sprintf("Unknown tool: %s", params.Name))
	}
}

func (s *Server) collectJobLogs(ctx context.Context, client pb.SchedulerClient, jobID string, progressToken interface{}) (string, error) {
	var lines []string
	var mu sync.Mutex
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
			mu.Lock()
			lines = append(lines, line.Content)
			count := len(lines)
			mu.Unlock()

			if progressToken != nil {
				s.SendProgress(progressToken, float64(count), 0, line.Content)
			}
		}
	}()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	timeout := time.After(60 * time.Second)
	var streamEndTimer <-chan time.Time

	drainLogs := func() {
		mu.Lock()
		defer mu.Unlock()
		if len(lines) == 0 {
			logCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			if s, err := client.StreamJobLogs(logCtx, &pb.StreamJobLogsRequest{JobId: jobID}); err == nil {
				for {
					l, err := s.Recv()
					if err != nil {
						break
					}
					lines = append(lines, l.Content)
				}
			}
		}
	}

	for {
		statusResp, err := client.GetJobStatus(ctx, &pb.GetJobStatusRequest{JobId: jobID})
		if err == nil {
			if statusResp.State == pb.JobState_JOB_STATE_SUCCEEDED {
				select {
				case <-logDone:
				case <-time.After(1 * time.Second):
				}
				drainLogs()
				mu.Lock()
				defer mu.Unlock()
				return strings.Join(lines, "\n"), nil
			}
			if statusResp.State == pb.JobState_JOB_STATE_FAILED {
				select {
				case <-logDone:
				case <-time.After(1 * time.Second):
				}
				drainLogs()
				mu.Lock()
				defer mu.Unlock()
				return strings.Join(lines, "\n"), fmt.Errorf("job failed: %s", statusResp.ErrorMessage)
			}
		}

		if streamEndTimer == nil {
			select {
			case <-logDone:
				streamEndTimer = time.After(10 * time.Second)
			default:
			}
		}

		select {
		case <-ctx.Done():
			cancelStream()
			select {
			case <-logDone:
			case <-time.After(1 * time.Second):
			}
			drainLogs()
			mu.Lock()
			defer mu.Unlock()
			return strings.Join(lines, "\n"), ctx.Err()
		case <-streamEndTimer:
			cancelStream()
			drainLogs()
			mu.Lock()
			defer mu.Unlock()
			if statusResp != nil && statusResp.State == pb.JobState_JOB_STATE_SUCCEEDED {
				return strings.Join(lines, "\n"), nil
			}
			return strings.Join(lines, "\n"), fmt.Errorf("job %s finished execution but status update timed out", jobID)
		case <-timeout:
			cancelStream()
			drainLogs()
			mu.Lock()
			defer mu.Unlock()
			return strings.Join(lines, "\n"), fmt.Errorf("job %s timed out waiting for completion", jobID)
		case <-ticker.C:
		}
	}
}

type mcpBearerTokenAuth struct {
	token string
}

func (b mcpBearerTokenAuth) GetRequestMetadata(ctx context.Context, uri ...string) (map[string]string, error) {
	return map[string]string{"authorization": "Bearer " + b.token}, nil
}

func (b mcpBearerTokenAuth) RequireTransportSecurity() bool {
	return false
}

func (s *Server) dialScheduler(ctx context.Context) (*grpc.ClientConn, error) {
	if s.conn != nil {
		return s.conn, nil
	}
	if s.cfg == nil {
		return nil, fmt.Errorf("packets configuration is not set")
	}

	host := s.cfg.OracleVMTailscaleHost
	var addr string
	if host == "" {
		addr = "127.0.0.1" + s.cfg.SchedulerAddr()
	} else if strings.Contains(host, ":") {
		addr = host
	} else {
		addr = host + s.cfg.SchedulerAddr()
	}

	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}

	if s.cfg.AuthToken != "" {
		opts = append(opts, grpc.WithPerRPCCredentials(mcpBearerTokenAuth{token: s.cfg.AuthToken}))
	}

	return grpc.DialContext(ctx, addr, opts...) //nolint:staticcheck
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

func (s *Server) validateSubspace(ctx context.Context, conn *grpc.ClientConn, subspaceID string) error {
	if subspaceID == "" {
		return nil
	}
	subClient := pb.NewSubspaceServiceClient(conn)
	subResp, err := subClient.GetSubspace(ctx, &pb.GetSubspaceRequest{Id: subspaceID})
	if err != nil {
		return fmt.Errorf("resolve subspace %s: %w", subspaceID, err)
	}
	if subResp.Subspace.State == "sleeping" {
		wakeResp, wakeErr := subClient.WakeSubspace(ctx, &pb.WakeSubspaceRequest{Id: subspaceID})
		if wakeErr != nil {
			return fmt.Errorf("auto-wake subspace %s: %w", subspaceID, wakeErr)
		}
		subResp = wakeResp
	}
	if subResp.Subspace.State != "ready" && subResp.Subspace.State != "busy" {
		return fmt.Errorf("subspace %s is not ready (state: %s)", subspaceID, subResp.Subspace.State)
	}
	return nil
}

func (s *Server) execRemote(ctx context.Context, conn *grpc.ClientConn, args ...string) (string, error) {
	client := pb.NewSchedulerClient(conn)
	cacheKey := fmt.Sprintf("exec:mcp-remote:%d", time.Now().UnixNano())
	resp, err := client.SubmitJob(ctx, &pb.SubmitJobRequest{
		CacheKey:    cacheKey,
		Toolchain:   string(apitypes.ToolchainExec),
		Runner:      string(apitypes.RunnerHost),
		CommandArgs: args,
		SourceMode:  string(apitypes.SourceModeWorkspace),
	})
	if err != nil {
		return "", err
	}
	return s.collectJobLogs(ctx, client, resp.JobId, nil)
}
