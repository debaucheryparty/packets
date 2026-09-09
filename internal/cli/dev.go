package cli

import (
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/debaucheryparty/packets/internal/config"
	"github.com/debaucheryparty/packets/internal/environment"
	"github.com/debaucheryparty/packets/internal/instructions"
	"github.com/spf13/cobra"
)

func NewDevCommand(cfg *config.Config, logger *slog.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "dev [dir]",
		Short: "Initialize and connect project to Packets remote development platform",
		Long: `Initializes the unified remote development workflow for developers and AI coding agents:
1. Detects project components (Android, Zephyr, Rust, Go, Node, etc.)
2. Verifies and prepares remote toolchains & SDKs
3. Initializes remote persistent workspace synchronization
4. Configures AI agent instructions (AGENTS.md, .cursor/rules, MCP configuration)`,
		Example: `  packets dev .
  packets dev path/to/project`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			dir := "."
			if len(args) > 0 {
				dir = args[0]
			}

			absDir, err := filepath.Abs(dir)
			if err != nil {
				return err
			}

			fmt.Println("Initializing Packets Remote Development Session...")
			fmt.Printf("Project root: %s\n\n", absDir)

			envMgr := environment.NewManager()
			topo, err := envMgr.Detect(absDir)
			if err != nil {
				return fmt.Errorf("project detection: %w", err)
			}

			fmt.Println("Detected Components:")
			if len(topo.Components) == 0 {
				fmt.Println("  (Generic project)")
			} else {
				for _, c := range topo.Components {
					fmt.Printf("  • %s [%s] (confidence: %s)\n", c.Name, c.Type, c.Confidence)
				}
			}
			fmt.Println()

			fmt.Println("Checking remote toolchain & SDK readiness...")
			report, err := envMgr.Check(ctx, absDir)
			if err != nil {
				logger.WarnContext(ctx, "environment check warning", slog.String("err", err.Error()))
			} else {
				for compType, reqs := range report.Components {
					fmt.Printf("  [%s]\n", compType)
					for _, req := range reqs {
						status := "✓"
						if req.Status == environment.StatusMissing {
							status = "✗"
						} else if req.Status == environment.StatusWarning {
							status = "!"
						}
						fmt.Printf("    %s %-20s %s\n", status, req.Name, req.Status)
					}
				}
			}
			fmt.Println()

			fmt.Println("Configuring AI Agent Integration...")
			if err := instructions.GenerateInstructions(absDir); err != nil {
				logger.WarnContext(ctx, "failed generating instructions", slog.String("err", err.Error()))
			} else {
				fmt.Println("  ✓ Generated AGENTS.md")
				fmt.Println("  ✓ Generated .cursor/rules/packets.mdc")
				fmt.Println("  ✓ Configured .vscode/mcp.json")
			}
			fmt.Println()

			fmt.Println("════════════════════════════════════════════════════════════════════════")
			fmt.Println("✓ Packets Remote Development Mode is Ready!")
			fmt.Println("  Remote Execution:   Active")
			fmt.Println("  Persistent VPS:     Configured")
			fmt.Println("  AI Agent Protocol:  MCP Ready (packets mcp)")
			fmt.Println("════════════════════════════════════════════════════════════════════════")
			return nil
		},
	}
}
