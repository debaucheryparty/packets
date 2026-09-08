package instructions

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const AgentsTemplate = `# Packets Remote AI Development Instructions

This project uses **Packets** for remote development execution.

## Core Rules for AI Coding Agents

1. **Local Machine is the Interface**: The local machine is reserved for editing source code, reviewing files, and configuring settings.
2. **Never Run Heavy Commands Locally**: Do NOT execute Gradle builds, compiler invocations (cargo, go build, cmake), tests, emulators, or SDK management commands on the local machine.
3. **Use Packets MCP Tools**: Always use Packets MCP tools for remote execution:
   - packets_workspace_info: inspect workspace and components.
   - packets_env_check: verify remote toolchains and SDKs.
   - packets_env_prepare: provision missing dependencies.
   - packets_sync: synchronize local changes to remote persistent workspace before execution.
   - packets_build: trigger remote builds (e.g. ./gradlew assembleDebug, west build, cargo build).
   - packets_test: run remote test suites.
   - packets_exec: execute arbitrary commands on the VPS workspace.
   - packets_artifacts: fetch build outputs (APKs, firmware binaries, etc.).
4. **Synchronize Workspace**: Before requesting execution, ensure the workspace has been synchronized via packets_sync.
5. **Request Approval**: When running remote commands or builds, confirm with the user before execution.
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
	_ = os.WriteFile(filepath.Join(cursorDir, "packets.mdc"), []byte(cursorRule), 0o644)

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
	mcpData, _ := json.MarshalIndent(mcpConfig, "", "  ")
	_ = os.WriteFile(filepath.Join(vscodeDir, "mcp.json"), mcpData, 0o644)

	return nil
}
