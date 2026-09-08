package instructions

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const AgentsTemplate = `# Packets Remote AI Development Instructions

This project uses **Packets** for remote development, persistent builds, and toolchain execution.

## Core Rules for AI Coding Agents

1. **Local Machine is the Interface**: The local machine is reserved for editing source code, reading files, and project configuration.
2. **Never Run Heavy Commands Locally**: Do NOT execute heavy compilation commands (e.g. ./gradlew assembleDebug, west build, cargo build, go build, make), long test suites, emulators, or SDK installations locally.
3. **Use Packets MCP Tools**: Always use Packets MCP tools for remote execution and verification:
   - packets_workspace_info: Inspect detected project components, toolchains, and topology.
   - packets_env_check: Remotely verify toolchain and SDK readiness on the VPS worker.
   - packets_env_prepare: Idempotently provision missing development toolchains and SDKs on the VPS worker.
   - packets_sync: Synchronize modified local files incrementally to the remote persistent workspace before running jobs.
   - packets_sync_full: Perform full synchronization with remote deletion detection.
   - packets_pull: Pull newly created/modified build outputs, stubs, or code back to the local workspace.
   - packets_build: Trigger remote builds (e.g. Gradle, Zephyr, Cargo, Go, CMake).
   - packets_test: Trigger remote unit or integration tests.
   - packets_exec: Execute arbitrary shell commands in the persistent VPS workspace.
   - packets_logs: Retrieve execution logs for a specific remote job.
   - packets_artifacts: Download and extract artifacts produced by a remote build into the workspace.
   - packets_status: Check health of the remote Packets daemon and active jobs.
   - packets_approve: Approve a pending execution ticket if policy requires confirmation.
4. **Always Sync Before Building**: Before executing a remote build, test, or command, run packets_sync to ensure the remote persistent workspace matches local source changes.
5. **Pull Artifacts When Done**: When a build or code generator produces new files (APKs, firmware binaries, OpenAPI stubs), run packets_pull or packets_artifacts to bring outputs into the local workspace.
6. **Request Approval for Dangerous Operations**: For destructive file edits or production-facing operations, inform the user and request confirmation.
`

func GenerateInstructions(projectDir string) error {
	absDir, err := filepath.Abs(projectDir)
	if err != nil {
		return err
	}

	// 1. Write AGENTS.md
	agentsPath := filepath.Join(absDir, "AGENTS.md")
	if err := os.WriteFile(agentsPath, []byte(AgentsTemplate), 0o644); err != nil {
		return fmt.Errorf("write AGENTS.md: %w", err)
	}

	// 2. Write .cursor/rules/packets.mdc
	cursorDir := filepath.Join(absDir, ".cursor", "rules")
	_ = os.MkdirAll(cursorDir, 0o755)
	cursorRule := "---\ndescription: Packets Remote Execution Rules\nglobs: *\n---\n" + AgentsTemplate
	if err := os.WriteFile(filepath.Join(cursorDir, "packets.mdc"), []byte(cursorRule), 0o644); err != nil {
		return fmt.Errorf("write packets.mdc: %w", err)
	}

	// 3. Write .vscode/mcp.json snippet
	vscodeDir := filepath.Join(absDir, ".vscode")
	_ = os.MkdirAll(vscodeDir, 0o755)
	mcpConfig := map[string]interface{}{
		"mcpServers": map[string]interface{}{
			"packets": map[string]interface{}{
				"command": "packets",
				"args":    []string{"mcp"},
			},
		},
	}
	mcpData, err := json.MarshalIndent(mcpConfig, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal mcp.json: %w", err)
	}
	if err := os.WriteFile(filepath.Join(vscodeDir, "mcp.json"), mcpData, 0o644); err != nil {
		return fmt.Errorf("write mcp.json: %w", err)
	}

	return nil
}
