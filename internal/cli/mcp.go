package cli

import (
	"log/slog"
	"os"

	"github.com/debaucheryparty/packets/internal/config"
	"github.com/debaucheryparty/packets/internal/mcp"
	"github.com/spf13/cobra"
)

func NewMCPCommand(cfg *config.Config, logger *slog.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Start the Packets Model Context Protocol (MCP) server for AI coding agents",
		Long: `Runs the Packets MCP server over standard input and output (stdio).
Compatible with Cursor, VS Code (Copilot / Claude Dev), Claude Desktop, and Antigravity.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			pwd, err := os.Getwd()
			if err != nil {
				return err
			}
			server := mcp.NewServer(cfg, logger, pwd)
			return server.Run(os.Stdin, os.Stdout)
		},
	}
}
