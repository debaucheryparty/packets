package cli

import (
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/debaucheryparty/packets/internal/config"
	"github.com/debaucheryparty/packets/internal/environment"
	"github.com/debaucheryparty/packets/internal/project"
	"github.com/spf13/cobra"
)

func NewInitCommand(_ *config.Config, _ *slog.Logger) *cobra.Command {
	var projectIDFlag string

	cmd := &cobra.Command{
		Use:   "init [dir]",
		Short: "Initialize a new Packets project configuration",
		Long: `Initializes .packets/project.json in the specified directory.
Automatically detects toolchains and components in the workspace.`,
		Example: `  packets init
  packets init .
  packets init ./my-project --id my-custom-app`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) > 0 {
				dir = args[0]
			}

			absDir, err := filepath.Abs(dir)
			if err != nil {
				return fmt.Errorf("resolve directory: %w", err)
			}

			// Check if already initialized
			if existing, err := project.LoadConfig(absDir); err == nil && existing != nil {
				fmt.Printf("Project already initialized in %s (Project ID: %s)\n", absDir, existing.ProjectID)
				return nil
			}

			// Detect topology
			mgr := environment.NewManager()
			topo, _ := mgr.Detect(absDir)

			projectID := projectIDFlag
			if projectID == "" {
				projectID = project.ResolveProjectID(absDir)
			}

			cfg := &project.Config{
				ProjectID: projectID,
				Name:      filepath.Base(absDir),
			}

			if topo != nil {
				for _, comp := range topo.Components {
					cfg.Components = append(cfg.Components, project.ComponentConfig{
						Type: string(comp.Type),
						Path: comp.Path,
					})
				}
			}

			if err := project.SaveConfig(absDir, cfg); err != nil {
				return fmt.Errorf("failed to save project configuration: %w", err)
			}

			fmt.Printf("packets :: project initialized [%s] in %s\n", cfg.ProjectID, absDir)
			if len(cfg.Components) > 0 {
				for _, comp := range cfg.Components {
					fmt.Printf("packets :: component detected [%s] at %s\n", comp.Type, comp.Path)
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&projectIDFlag, "id", "", "Custom project ID")
	return cmd
}
