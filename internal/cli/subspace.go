package cli

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/debaucheryparty/packets/internal/config"
	"github.com/debaucheryparty/packets/internal/project"
	pb "github.com/debaucheryparty/packets/proto/v1"
	"github.com/spf13/cobra"
)

func NewSubspaceCommand(cfg *config.Config, logger *slog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "subspace",
		Short: "Manage persistent remote development Subspaces",
		Long:  "Subspaces provide persistent remote development environments grouping a workspace, environment, and worker assignment.",
	}

	cmd.AddCommand(
		newSubspaceCreateCommand(cfg, logger),
		newSubspaceListCommand(cfg, logger),
		newSubspaceStatusCommand(cfg, logger),
		newSubspaceSleepCommand(cfg, logger),
		newSubspaceWakeCommand(cfg, logger),
		newSubspaceDestroyCommand(cfg, logger),
		newSubspaceShellCommand(cfg, logger),
	)

	return cmd
}

func newSubspaceCreateCommand(cfg *config.Config, _ *slog.Logger) *cobra.Command {
	var projectFlag, workerFlag, envFlag, wsFlag string

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new persistent remote Subspace",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			pwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getwd: %w", err)
			}

			projectID := projectFlag
			if projectID == "" {
				projectID = project.ResolveProjectID(pwd)
			}

			conn, err := DialScheduler(ctx, cfg)
			if err != nil {
				return fmt.Errorf("connect to packetsd: %w", err)
			}
			defer func() { _ = conn.Close() }()

			client := pb.NewSubspaceServiceClient(conn)
			resp, err := client.CreateSubspace(ctx, &pb.CreateSubspaceRequest{
				ProjectId:     projectID,
				WorkerId:      workerFlag,
				EnvironmentId: envFlag,
				WorkspaceId:   wsFlag,
			})
			if err != nil {
				return fmt.Errorf("create subspace: %w", err)
			}

			sub := resp.Subspace
			fmt.Println("✓ Subspace created successfully")
			fmt.Printf("  ID:          %s\n", sub.Id)
			fmt.Printf("  Project:     %s\n", sub.ProjectId)
			fmt.Printf("  Worker:      %s\n", sub.WorkerId)
			fmt.Printf("  Environment: %s\n", sub.EnvironmentId)
			fmt.Printf("  State:       %s\n", sub.State)
			return nil
		},
	}

	cmd.Flags().StringVar(&projectFlag, "project", "", "Project identifier (defaults to directory)")
	cmd.Flags().StringVar(&workerFlag, "worker", "local", "Target worker ID")
	cmd.Flags().StringVar(&envFlag, "env", "default", "Environment profile ID")
	cmd.Flags().StringVar(&wsFlag, "ws", "", "Explicit workspace snapshot ID")

	return cmd
}

func newSubspaceListCommand(cfg *config.Config, _ *slog.Logger) *cobra.Command {
	var projectFlag, ownerFlag string
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all persistent Subspaces",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			conn, err := DialScheduler(ctx, cfg)
			if err != nil {
				return fmt.Errorf("connect to packetsd: %w", err)
			}
			defer func() { _ = conn.Close() }()

			client := pb.NewSubspaceServiceClient(conn)
			resp, err := client.ListSubspaces(ctx, &pb.ListSubspacesRequest{
				ProjectId: projectFlag,
				OwnerId:   ownerFlag,
			})
			if err != nil {
				return fmt.Errorf("list subspaces: %w", err)
			}

			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(resp.Subspaces)
			}

			if len(resp.Subspaces) == 0 {
				fmt.Println("packets :: no subspaces found")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
			_, _ = fmt.Fprintln(w, "SUBSPACE ID\tPROJECT\tWORKER\tSTATE\tCREATED\tLAST USED")

			for _, s := range resp.Subspaces {
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
					s.Id, s.ProjectId, s.WorkerId, s.State, s.CreatedAt, s.LastUsedAt)
			}
			return w.Flush()
		},
	}

	cmd.Flags().StringVar(&projectFlag, "project", "", "Filter by project ID")
	cmd.Flags().StringVar(&ownerFlag, "owner", "", "Filter by owner ID")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	return cmd
}

func newSubspaceStatusCommand(cfg *config.Config, _ *slog.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "status <subspace-id>",
		Short: "Inspect details and health of a Subspace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			conn, err := DialScheduler(ctx, cfg)
			if err != nil {
				return fmt.Errorf("connect to packetsd: %w", err)
			}
			defer func() { _ = conn.Close() }()

			client := pb.NewSubspaceServiceClient(conn)
			resp, err := client.GetSubspace(ctx, &pb.GetSubspaceRequest{Id: args[0]})
			if err != nil {
				return fmt.Errorf("get subspace %s: %w", args[0], err)
			}

			sub := resp.Subspace
			fmt.Printf("Subspace:    %s\n", sub.Id)
			fmt.Printf("Project:     %s\n", sub.ProjectId)
			fmt.Printf("Owner:       %s\n", sub.OwnerId)
			fmt.Printf("Worker:      %s\n", sub.WorkerId)
			fmt.Printf("Workspace:   %s\n", sub.WorkspaceId)
			fmt.Printf("Environment: %s\n", sub.EnvironmentId)
			fmt.Printf("State:       %s\n", sub.State)
			fmt.Printf("Created:     %s\n", sub.CreatedAt)
			fmt.Printf("Last Used:   %s\n", sub.LastUsedAt)
			if len(sub.Metadata) > 0 {
				fmt.Println("Metadata:")
				for k, v := range sub.Metadata {
					fmt.Printf("  %s: %s\n", k, v)
				}
			}
			return nil
		},
	}
}

func newSubspaceSleepCommand(cfg *config.Config, _ *slog.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "sleep <subspace-id>",
		Short: "Put a remote Subspace to sleep to reclaim resources",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			conn, err := DialScheduler(ctx, cfg)
			if err != nil {
				return fmt.Errorf("connect to packetsd: %w", err)
			}
			defer func() { _ = conn.Close() }()

			client := pb.NewSubspaceServiceClient(conn)
			resp, err := client.SleepSubspace(ctx, &pb.SleepSubspaceRequest{Id: args[0]})
			if err != nil {
				return fmt.Errorf("sleep subspace %s: %w", args[0], err)
			}

			fmt.Printf("✓ Subspace %s is now sleeping\n", resp.Subspace.Id)
			return nil
		},
	}
}

func newSubspaceWakeCommand(cfg *config.Config, _ *slog.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "wake <subspace-id>",
		Short: "Wake a sleeping remote Subspace back to ready state",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			conn, err := DialScheduler(ctx, cfg)
			if err != nil {
				return fmt.Errorf("connect to packetsd: %w", err)
			}
			defer func() { _ = conn.Close() }()

			client := pb.NewSubspaceServiceClient(conn)
			resp, err := client.WakeSubspace(ctx, &pb.WakeSubspaceRequest{Id: args[0]})
			if err != nil {
				return fmt.Errorf("wake subspace %s: %w", args[0], err)
			}

			fmt.Printf("✓ Subspace %s is now ready\n", resp.Subspace.Id)
			return nil
		},
	}
}

func newSubspaceDestroyCommand(cfg *config.Config, _ *slog.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "destroy <subspace-id>",
		Short: "Destroy and remove a remote Subspace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			conn, err := DialScheduler(ctx, cfg)
			if err != nil {
				return fmt.Errorf("connect to packetsd: %w", err)
			}
			defer func() { _ = conn.Close() }()

			client := pb.NewSubspaceServiceClient(conn)
			_, err = client.DestroySubspace(ctx, &pb.DestroySubspaceRequest{Id: args[0]})
			if err != nil {
				return fmt.Errorf("destroy subspace %s: %w", args[0], err)
			}

			fmt.Printf("✓ Subspace %s destroyed\n", args[0])
			return nil
		},
	}
}

func newSubspaceShellCommand(cfg *config.Config, logger *slog.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "shell <subspace-id> [-- <cmd>...]",
		Short: "Execute interactive shell commands in the Subspace workspace",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			subID := args[0]
			var execArgs []string
			if len(args) > 1 {
				execArgs = args[1:]
			} else {
				execArgs = []string{"sh"}
			}

			execCmd := NewExecCommand(cfg, logger)
			flags := []string{"--subspace", subID, strings.Join(execArgs, " ")}
			execCmd.SetArgs(flags)
			return execCmd.ExecuteContext(cmd.Context())
		},
	}
}
