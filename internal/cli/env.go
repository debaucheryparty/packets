package cli

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/debaucheryparty/packets/internal/config"
	"github.com/debaucheryparty/packets/internal/environment"
	"github.com/debaucheryparty/packets/internal/project"
	"github.com/spf13/cobra"
)

func NewEnvCommand(cfg *config.Config, logger *slog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "env",
		Short: "Inspect, check, and prepare project development environments",
	}

	cmd.AddCommand(
		newEnvDetectCommand(cfg, logger),
		newEnvCheckCommand(cfg, logger),
		newEnvPrepareCommand(cfg, logger),
		newEnvSetCommand(cfg, logger),
		newEnvRemoveCommand(cfg, logger),
		newEnvResetCommand(cfg, logger),
	)

	return cmd
}

func newEnvDetectCommand(cfg *config.Config, logger *slog.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "detect [dir]",
		Short: "Detect project components and toolchains",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) > 0 {
				dir = args[0]
			}

			mgr := environment.NewManager()
			topo, err := mgr.Detect(dir)
			if err != nil {
				return err
			}

			fmt.Printf("Project Topology:\n")
			fmt.Printf("  Root:       %s\n", topo.RootPath)
			fmt.Printf("  Is Hybrid:  %t\n", topo.IsHybrid)
			fmt.Printf("  Components: %d\n\n", len(topo.Components))

			if len(topo.Components) == 0 {
				fmt.Println("  No supported project components detected.")
				return nil
			}

			for i, comp := range topo.Components {
				fmt.Printf("[%d] %s (%s)\n", i+1, comp.Name, comp.Type)
				fmt.Printf("    Path:       %s\n", comp.Path)
				fmt.Printf("    Confidence: %s\n", comp.Confidence)
				for k, v := range comp.Metadata {
					fmt.Printf("    %s: %s\n", k, v)
				}
				fmt.Println()
			}

			return nil
		},
	}
}

func newEnvCheckCommand(cfg *config.Config, logger *slog.Logger) *cobra.Command {
	var local bool

	cmd := &cobra.Command{
		Use:   "check [dir]",
		Short: "Verify required toolchains and SDKs for the project",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			dir := "."
			if len(args) > 0 {
				dir = args[0]
			}

			mgr := environment.NewManager()
			topo, err := mgr.Detect(dir)
			if err != nil {
				return err
			}

			var report *environment.EnvironmentReport
			if local {
				report, err = mgr.CheckComponents(ctx, topo.Components)
				if err != nil {
					return err
				}
				report.ProjectRoot = topo.RootPath
			} else {
				conn, err := DialScheduler(ctx, cfg)
				if err != nil {
					return fmt.Errorf("connect to remote worker (use --local to check locally): %w", err)
				}
				defer conn.Close()

				projectID := project.ResolveProjectID(dir)
				remote := environment.NewRemoteClient(conn)
				report, err = remote.Check(ctx, projectID, topo.RootPath, topo.Components)
				if err != nil {
					return err
				}
			}

			targetEnv := "Remote Worker"
			if local {
				targetEnv = "Local Machine"
			}
			fmt.Printf("Environment Status for %s (%s):\n\n", report.ProjectRoot, targetEnv)

			if len(report.Components) == 0 {
				fmt.Println("No project components detected to check.")
				return nil
			}

			for compType, reqs := range report.Components {
				fmt.Printf("Component: %s\n", compType)
				for _, req := range reqs {
					statusSymbol := "✓"
					if req.Status == environment.StatusMissing {
						statusSymbol = "✗"
					} else if req.Status == environment.StatusWarning {
						statusSymbol = "!"
					}

					details := ""
					if req.Details != "" {
						details = fmt.Sprintf("(%s)", req.Details)
					}

					fmt.Printf("  %s %-25s %-8s %s\n", statusSymbol, req.Name, req.Status, details)
				}
				fmt.Println()
			}

			if report.AllReady {
				fmt.Println("✓ All required environment toolchains are ready!")
			} else {
				fmt.Println("✗ Some requirements are missing. Run 'packets env prepare .' to provision them.")
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&local, "local", false, "Run check on the local machine instead of remote worker")
	return cmd
}

func newEnvPrepareCommand(cfg *config.Config, logger *slog.Logger) *cobra.Command {
	var local bool

	cmd := &cobra.Command{
		Use:   "prepare [dir]",
		Short: "Provision missing development toolchains and SDKs",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			dir := "."
			if len(args) > 0 {
				dir = args[0]
			}

			mgr := environment.NewManager()
			topo, err := mgr.Detect(dir)
			if err != nil {
				return err
			}

			if local {
				return mgr.PrepareComponents(ctx, topo.Components, func(msg string) {
					fmt.Println(msg)
				})
			}

			conn, err := DialScheduler(ctx, cfg)
			if err != nil {
				return fmt.Errorf("connect to remote worker (use --local to prepare locally): %w", err)
			}
			defer conn.Close()

			projectID := project.ResolveProjectID(dir)
			remote := environment.NewRemoteClient(conn)
			return remote.Prepare(ctx, projectID, topo.RootPath, topo.Components, func(msg string) {
				fmt.Println(msg)
			})
		},
	}

	cmd.Flags().BoolVar(&local, "local", false, "Run preparation on the local machine instead of remote worker")
	return cmd
}

func newEnvSetCommand(cfg *config.Config, logger *slog.Logger) *cobra.Command {
	var dir string

	cmd := &cobra.Command{
		Use:   "set <type> [path]",
		Short: "Manually configure or override a project component",
		Long: `Sets a component override in .packets/project.json.
Supported types: android, zephyr, rust, go, node, python, cmake, java, swift, ruby, php, zig, dotnet, flutter, dart, elixir, generic`,
		Example: `  packets env set android
  packets env set rust backend
  packets env set zephyr firmware --dir ./my-project`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			compType := strings.ToLower(args[0])
			relPath := ""
			if len(args) > 1 {
				relPath = filepath.Clean(args[1])
				if relPath == "." {
					relPath = ""
				}
			}

			projectCfg, err := project.LoadConfig(dir)
			if err != nil {
				projectCfg = &project.Config{
					ProjectID: project.ResolveProjectID(dir),
				}
			}

			found := false
			for i, c := range projectCfg.Components {
				if strings.ToLower(c.Type) == compType {
					projectCfg.Components[i].Path = relPath
					found = true
					break
				}
			}
			if !found {
				projectCfg.Components = append(projectCfg.Components, project.ComponentConfig{
					Type: compType,
					Path: relPath,
				})
			}

			if err := project.SaveConfig(dir, projectCfg); err != nil {
				return fmt.Errorf("failed saving component override: %w", err)
			}

			fmt.Printf("✓ Configured component: %s", compType)
			if relPath != "" {
				fmt.Printf(" [path: %s]", relPath)
			}
			fmt.Printf(" in .packets/project.json\n")
			fmt.Println("Run 'packets env detect' or 'packets env check' to verify.")
			return nil
		},
	}

	cmd.Flags().StringVar(&dir, "dir", ".", "Project root directory")
	return cmd
}

func newEnvRemoveCommand(cfg *config.Config, logger *slog.Logger) *cobra.Command {
	var dir string

	cmd := &cobra.Command{
		Use:     "remove <type>",
		Aliases: []string{"rm", "unset"},
		Short:   "Remove a manually configured component",
		Example: `  packets env remove android
  packets env remove rust --dir ./my-project`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			compType := strings.ToLower(args[0])

			projectCfg, err := project.LoadConfig(dir)
			if err != nil || len(projectCfg.Components) == 0 {
				fmt.Printf("No manual component overrides found in %s\n", dir)
				return nil
			}

			var remaining []project.ComponentConfig
			found := false
			for _, c := range projectCfg.Components {
				if strings.ToLower(c.Type) == compType {
					found = true
				} else {
					remaining = append(remaining, c)
				}
			}

			if !found {
				fmt.Printf("Component %q was not manually configured.\n", compType)
				return nil
			}

			projectCfg.Components = remaining
			if err := project.SaveConfig(dir, projectCfg); err != nil {
				return fmt.Errorf("failed updating .packets/project.json: %w", err)
			}

			fmt.Printf("✓ Removed component %q from .packets/project.json\n", compType)
			return nil
		},
	}

	cmd.Flags().StringVar(&dir, "dir", ".", "Project root directory")
	return cmd
}

func newEnvResetCommand(cfg *config.Config, logger *slog.Logger) *cobra.Command {
	return &cobra.Command{
		Use:     "reset [dir]",
		Aliases: []string{"clear"},
		Short:   "Clear manual component overrides and restore auto-detection",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) > 0 {
				dir = args[0]
			}

			projectCfg, err := project.LoadConfig(dir)
			if err != nil || len(projectCfg.Components) == 0 {
				fmt.Println("No manual component overrides to reset.")
				return nil
			}

			projectCfg.Components = nil
			if err := project.SaveConfig(dir, projectCfg); err != nil {
				return fmt.Errorf("failed resetting .packets/project.json: %w", err)
			}

			fmt.Println("✓ Cleared manual component overrides. Heuristic auto-detection restored.")
			return nil
		},
	}
}
