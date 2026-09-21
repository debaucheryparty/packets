package cli

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/debaucheryparty/packets/internal/config"
	"github.com/debaucheryparty/packets/internal/project"
	pb "github.com/debaucheryparty/packets/proto/v1"
	"github.com/spf13/cobra"
)

const (
	ansiReset = "\033[0m"
	ansiBold  = "\033[1m"
	ansiCream = "\033[38;2;234;219;182m"
	ansiSage  = "\033[38;2;127;169;155m"
	ansiCoral = "\033[38;2;255;77;54m"
	ansiRose  = "\033[38;2;199;130;131m"
	ansiTaupe = "\033[38;2;115;104;94m"
)

func shouldColor() bool {
	return os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"
}

func style(text, code string) string {
	if !shouldColor() {
		return text
	}
	return code + text + ansiReset
}

func formatEndpoint(port string) string {
	port = strings.TrimSpace(port)
	if port == "" {
		return ""
	}
	if strings.Contains(port, ":") {
		parts := strings.Split(port, ":")
		if parts[0] != "" {
			if strings.HasPrefix(parts[0], ":") {
				return parts[0]
			}
			return ":" + parts[0]
		}
		return port
	}
	if !strings.HasPrefix(port, ":") {
		return ":" + port
	}
	return port
}

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
			fmt.Println(style("✓ Subspace created successfully", ansiSage))
			fmt.Printf("  %s %s\n", style("ID:         ", ansiTaupe), style(sub.Id, ansiCoral))
			fmt.Printf("  %s %s\n", style("Project:    ", ansiTaupe), style(sub.ProjectId, ansiCream))
			fmt.Printf("  %s %s\n", style("Worker:     ", ansiTaupe), style(sub.WorkerId, ansiCream))
			fmt.Printf("  %s %s\n", style("Environment:", ansiTaupe), style(sub.EnvironmentId, ansiCream))
			fmt.Printf("  %s %s\n", style("State:      ", ansiTaupe), style(sub.State, ansiSage))
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
			_, _ = fmt.Fprintln(w, style("SUBSPACE ID\tPROJECT\tWORKER\tSTATE\tCREATED\tLAST USED", ansiBold+ansiCream))

			for _, s := range resp.Subspaces {
				stColor := ansiSage
				switch strings.ToUpper(s.State) {
				case "SLEEPING", "DESTROYED", "FAILED":
					stColor = ansiRose
				case "BUSY", "CREATING":
					stColor = ansiCoral
				}
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
					style(s.Id, ansiCoral),
					style(s.ProjectId, ansiCream),
					style(s.WorkerId, ansiCream),
					style(s.State, stColor),
					style(s.CreatedAt, ansiTaupe),
					style(s.LastUsedAt, ansiTaupe))
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
			stateColor := ansiSage
			switch strings.ToUpper(sub.State) {
			case "SLEEPING", "DESTROYED", "FAILED":
				stateColor = ansiRose
			case "BUSY", "CREATING":
				stateColor = ansiCoral
			}

			fmt.Printf("  %s %s\n", style("Subspace:   ", ansiTaupe), style(sub.Id, ansiCoral))
			fmt.Printf("  %s %s\n", style("Project:    ", ansiTaupe), style(sub.ProjectId, ansiCream))
			fmt.Printf("  %s %s\n", style("Owner:      ", ansiTaupe), style(sub.OwnerId, ansiCream))
			fmt.Printf("  %s %s\n", style("Worker:     ", ansiTaupe), style(sub.WorkerId, ansiCream))
			fmt.Printf("  %s %s\n", style("Workspace:  ", ansiTaupe), style(sub.WorkspaceId, ansiCream))
			fmt.Printf("  %s %s\n", style("Environment:", ansiTaupe), style(sub.EnvironmentId, ansiCream))
			fmt.Printf("  %s %s\n", style("State:      ", ansiTaupe), style(sub.State, stateColor))
			fmt.Printf("  %s %s\n", style("Created:    ", ansiTaupe), style(sub.CreatedAt, ansiTaupe))
			fmt.Printf("  %s %s\n", style("Last Used:  ", ansiTaupe), style(sub.LastUsedAt, ansiTaupe))
			if len(sub.Metadata) > 0 {
				fmt.Println(style("Metadata:", ansiTaupe))
				for k, v := range sub.Metadata {
					fmt.Printf("  %s %s\n", style(k+":", ansiTaupe), style(v, ansiCream))
				}
			}

			svcClient := pb.NewRemoteServiceServiceClient(conn)
			svcResp, _ := svcClient.ListServices(ctx, &pb.ListServicesRequest{SubspaceId: sub.Id})
			if svcResp != nil && len(svcResp.Services) > 0 {
				maxLen := 0
				for _, s := range svcResp.Services {
					if len(s.Name) > maxLen {
						maxLen = len(s.Name)
					}
				}

				fmt.Printf("\n%s\n", style("Services:", ansiBold+ansiCream))
				for _, s := range svcResp.Services {
					statusColor := ansiSage
					switch strings.ToUpper(s.Status) {
					case "STOPPED", "FAILED":
						statusColor = ansiRose
					case "STARTING", "BUSY":
						statusColor = ansiCoral
					}
					pad := maxLen - len(s.Name)
					if pad < 0 {
						pad = 0
					}
					paddedName := s.Name + strings.Repeat(" ", pad)
					fmt.Printf("  %s  %s\n", style(paddedName, ansiCream), style(s.Status, statusColor))
				}

				type endpointItem struct {
					name string
					port string
				}
				var endpoints []endpointItem
				for _, s := range svcResp.Services {
					for _, p := range s.Ports {
						ep := formatEndpoint(p)
						if ep != "" {
							endpoints = append(endpoints, endpointItem{name: s.Name, port: ep})
						}
					}
				}

				if len(endpoints) > 0 {
					fmt.Printf("\n%s\n", style("Endpoints:", ansiBold+ansiCream))
					for _, ep := range endpoints {
						pad := maxLen - len(ep.name)
						if pad < 0 {
							pad = 0
						}
						paddedName := ep.name + strings.Repeat(" ", pad)
						fmt.Printf("  %s  %s\n", style(paddedName, ansiCream), style(ep.port, ansiSage))
					}
				}
			}

			return nil
		},
	}
}

func newSubspaceSleepCommand(cfg *config.Config, _ *slog.Logger) *cobra.Command {
	var idleFlag string

	cmd := &cobra.Command{
		Use:   "sleep [subspace-id]",
		Short: "Put a remote Subspace or idle Subspaces to sleep to reclaim resources",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			conn, err := DialScheduler(ctx, cfg)
			if err != nil {
				return fmt.Errorf("connect to packetsd: %w", err)
			}
			defer func() { _ = conn.Close() }()

			client := pb.NewSubspaceServiceClient(conn)

			if len(args) == 1 {
				resp, err := client.SleepSubspace(ctx, &pb.SleepSubspaceRequest{Id: args[0]})
				if err != nil {
					return fmt.Errorf("sleep subspace %s: %w", args[0], err)
				}
				fmt.Println(style(fmt.Sprintf("✓ Subspace %s is now sleeping", resp.Subspace.Id), ansiRose))
				return nil
			}

			if idleFlag == "" {
				return fmt.Errorf("specify a subspace ID or use --idle <duration> (e.g. --idle 30m)")
			}

			d, err := time.ParseDuration(idleFlag)
			if err != nil {
				return fmt.Errorf("invalid idle duration %q: %w", idleFlag, err)
			}

			listResp, err := client.ListSubspaces(ctx, &pb.ListSubspacesRequest{})
			if err != nil {
				return fmt.Errorf("list subspaces: %w", err)
			}

			now := time.Now().UTC()
			var sleptCount int
			for _, s := range listResp.Subspaces {
				if s.State != "ready" {
					continue
				}
				lastUsed, parseErr := time.Parse(time.RFC3339, s.LastUsedAt)
				if parseErr != nil {
					continue
				}
				if now.Sub(lastUsed) >= d {
					resp, err := client.SleepSubspace(ctx, &pb.SleepSubspaceRequest{Id: s.Id})
					if err == nil {
						fmt.Println(style(fmt.Sprintf("✓ Subspace %s is now sleeping (idle for %s)", resp.Subspace.Id, now.Sub(lastUsed).Round(time.Second)), ansiRose))
						sleptCount++
					}
				}
			}

			if sleptCount == 0 {
				fmt.Println("No idle subspaces found matching threshold.")
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&idleFlag, "idle", "", "Put all subspaces idle for longer than duration to sleep (e.g. 15m, 1h)")
	return cmd
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

			fmt.Println(style(fmt.Sprintf("✓ Subspace %s is now ready", resp.Subspace.Id), ansiSage))
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

			fmt.Println(style(fmt.Sprintf("✓ Subspace %s destroyed", args[0]), ansiCoral))
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
