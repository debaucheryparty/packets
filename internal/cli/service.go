package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/debaucheryparty/packets/internal/config"
	"github.com/debaucheryparty/packets/internal/project"
	"github.com/debaucheryparty/packets/pkg/devfile"
	pb "github.com/debaucheryparty/packets/proto/v1"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
)

func NewServiceCommand(cfg *config.Config, logger *slog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service",
		Short: "Manage background services within remote Subspaces",
		Long:  "Run and manage databases, caches, and background services inside persistent Subspaces.",
	}

	cmd.AddCommand(
		newServiceListCommand(cfg, logger),
		newServiceStartCommand(cfg, logger),
		newServiceStopCommand(cfg, logger),
		newServiceRestartCommand(cfg, logger),
		newServiceLogsCommand(cfg, logger),
		newServiceUpCommand(cfg, logger),
		newServiceDownCommand(cfg, logger),
	)

	return cmd
}

func newServiceListCommand(cfg *config.Config, _ *slog.Logger) *cobra.Command {
	var subspaceFlag string
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List services running in a Subspace",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			conn, err := DialScheduler(ctx, cfg)
			if err != nil {
				return fmt.Errorf("connect to packetsd: %w", err)
			}
			defer func() { _ = conn.Close() }()

			subID, err := resolveSubspaceID(ctx, conn, subspaceFlag)
			if err != nil {
				return err
			}

			client := pb.NewRemoteServiceServiceClient(conn)
			resp, err := client.ListServices(ctx, &pb.ListServicesRequest{SubspaceId: subID})
			if err != nil {
				return fmt.Errorf("list services: %w", err)
			}

			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(resp.Services)
			}

			if len(resp.Services) == 0 {
				fmt.Printf("No services found in subspace %s\n", subID)
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
			_, _ = fmt.Fprintln(w, "NAME\tSTATUS\tDRIVER\tIMAGE\tPORTS\tCONTAINER_ID")
			for _, s := range resp.Services {
				ports := strings.Join(s.Ports, ", ")
				if ports == "" {
					ports = "-"
				}
				cid := s.ContainerId
				if cid == "" {
					cid = "-"
				}
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
					s.Name, s.Status, s.Driver, s.Image, ports, cid)
			}
			return w.Flush()
		},
	}

	cmd.Flags().StringVar(&subspaceFlag, "subspace", "", "Target remote Subspace ID")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output results as JSON")

	return cmd
}

func newServiceStartCommand(cfg *config.Config, _ *slog.Logger) *cobra.Command {
	var (
		subspaceFlag string
		imageFlag    string
		driverFlag   string
		cmdFlag      []string
		portsFlag    []string
	)

	cmd := &cobra.Command{
		Use:   "start <name>",
		Short: "Start a service in a Subspace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			name := args[0]

			conn, err := DialScheduler(ctx, cfg)
			if err != nil {
				return fmt.Errorf("connect to packetsd: %w", err)
			}
			defer func() { _ = conn.Close() }()

			subID, err := resolveSubspaceID(ctx, conn, subspaceFlag)
			if err != nil {
				return err
			}

			img := imageFlag
			if img == "" && len(cmdFlag) == 0 {
				img = name
			}

			client := pb.NewRemoteServiceServiceClient(conn)
			resp, err := client.StartService(ctx, &pb.StartServiceRequest{
				SubspaceId: subID,
				Name:       name,
				Image:      img,
				Command:    cmdFlag,
				Ports:      portsFlag,
				Driver:     driverFlag,
			})
			if err != nil {
				return fmt.Errorf("start service %s: %w", name, err)
			}

			s := resp.Service
			fmt.Println("✓ Service started successfully")
			fmt.Printf("  Name:        %s\n", s.Name)
			fmt.Printf("  Subspace:    %s\n", s.SubspaceId)
			fmt.Printf("  Status:      %s\n", s.Status)
			fmt.Printf("  Driver:      %s\n", s.Driver)
			if s.ContainerId != "" {
				fmt.Printf("  Container:   %s\n", s.ContainerId)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&subspaceFlag, "subspace", "", "Target remote Subspace ID")
	cmd.Flags().StringVar(&imageFlag, "image", "", "Container image or command string")
	cmd.Flags().StringVar(&driverFlag, "driver", "process", "Service driver backend (process, docker)")
	cmd.Flags().StringSliceVar(&cmdFlag, "cmd", nil, "Command arguments to run")
	cmd.Flags().StringSliceVar(&portsFlag, "port", nil, "Ports exposed by the service")

	return cmd
}

func newServiceStopCommand(cfg *config.Config, _ *slog.Logger) *cobra.Command {
	var subspaceFlag string

	cmd := &cobra.Command{
		Use:   "stop <name>",
		Short: "Stop a service in a Subspace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			name := args[0]

			conn, err := DialScheduler(ctx, cfg)
			if err != nil {
				return fmt.Errorf("connect to packetsd: %w", err)
			}
			defer func() { _ = conn.Close() }()

			subID, err := resolveSubspaceID(ctx, conn, subspaceFlag)
			if err != nil {
				return err
			}

			client := pb.NewRemoteServiceServiceClient(conn)
			resp, err := client.StopService(ctx, &pb.StopServiceRequest{
				SubspaceId: subID,
				NameOrId:   name,
			})
			if err != nil {
				return fmt.Errorf("stop service %s: %w", name, err)
			}

			fmt.Printf("✓ Service %s stopped (status: %s)\n", resp.Service.Name, resp.Service.Status)
			return nil
		},
	}

	cmd.Flags().StringVar(&subspaceFlag, "subspace", "", "Target remote Subspace ID")

	return cmd
}

func newServiceRestartCommand(cfg *config.Config, _ *slog.Logger) *cobra.Command {
	var subspaceFlag string

	cmd := &cobra.Command{
		Use:   "restart <name>",
		Short: "Restart a service in a Subspace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			name := args[0]

			conn, err := DialScheduler(ctx, cfg)
			if err != nil {
				return fmt.Errorf("connect to packetsd: %w", err)
			}
			defer func() { _ = conn.Close() }()

			subID, err := resolveSubspaceID(ctx, conn, subspaceFlag)
			if err != nil {
				return err
			}

			client := pb.NewRemoteServiceServiceClient(conn)
			resp, err := client.RestartService(ctx, &pb.RestartServiceRequest{
				SubspaceId: subID,
				NameOrId:   name,
			})
			if err != nil {
				return fmt.Errorf("restart service %s: %w", name, err)
			}

			fmt.Printf("✓ Service %s restarted (status: %s)\n", resp.Service.Name, resp.Service.Status)
			return nil
		},
	}

	cmd.Flags().StringVar(&subspaceFlag, "subspace", "", "Target remote Subspace ID")

	return cmd
}

func newServiceLogsCommand(cfg *config.Config, _ *slog.Logger) *cobra.Command {
	var (
		subspaceFlag string
		linesFlag    int32
	)

	cmd := &cobra.Command{
		Use:   "logs <name>",
		Short: "Print logs from a service in a Subspace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			name := args[0]

			conn, err := DialScheduler(ctx, cfg)
			if err != nil {
				return fmt.Errorf("connect to packetsd: %w", err)
			}
			defer func() { _ = conn.Close() }()

			subID, err := resolveSubspaceID(ctx, conn, subspaceFlag)
			if err != nil {
				return err
			}

			client := pb.NewRemoteServiceServiceClient(conn)
			resp, err := client.GetServiceLogs(ctx, &pb.GetServiceLogsRequest{
				SubspaceId: subID,
				NameOrId:   name,
				Lines:      linesFlag,
			})
			if err != nil {
				return fmt.Errorf("get service logs %s: %w", name, err)
			}

			fmt.Print(resp.Logs)
			return nil
		},
	}

	cmd.Flags().StringVar(&subspaceFlag, "subspace", "", "Target remote Subspace ID")
	cmd.Flags().Int32Var(&linesFlag, "lines", 50, "Number of log lines to show")

	return cmd
}

func resolveSubspaceID(ctx context.Context, conn *grpc.ClientConn, subspaceFlag string) (string, error) {
	var subID string
	if subspaceFlag != "" {
		subID = subspaceFlag
	} else {
		pwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("getwd: %w", err)
		}

		projectID := project.ResolveProjectID(pwd)
		subClient := pb.NewSubspaceServiceClient(conn)
		listResp, err := subClient.ListSubspaces(ctx, &pb.ListSubspacesRequest{
			ProjectId: projectID,
		})
		if err == nil && len(listResp.Subspaces) > 0 {
			subID = listResp.Subspaces[0].Id
		} else {
			allResp, err := subClient.ListSubspaces(ctx, &pb.ListSubspacesRequest{})
			if err == nil && len(allResp.Subspaces) == 1 {
				subID = allResp.Subspaces[0].Id
			}
		}

		if subID == "" {
			return "", fmt.Errorf("no subspace specified and none found for project %q; specify --subspace <id> or create one with 'packets subspace create'", projectID)
		}
	}

	subClient := pb.NewSubspaceServiceClient(conn)
	subResp, err := subClient.GetSubspace(ctx, &pb.GetSubspaceRequest{Id: subID})
	if err == nil && subResp != nil && subResp.Subspace != nil && subResp.Subspace.State == "sleeping" {
		wakeResp, wakeErr := subClient.WakeSubspace(ctx, &pb.WakeSubspaceRequest{Id: subID})
		if wakeErr == nil && wakeResp != nil {
			fmt.Printf("✓ Auto-woke sleeping subspace %s\n", wakeResp.Subspace.Id)
		}
	}

	return subID, nil
}

func newServiceUpCommand(cfg *config.Config, _ *slog.Logger) *cobra.Command {
	var (
		subspaceFlag string
		driverFlag   string
	)

	cmd := &cobra.Command{
		Use:   "up [dir]",
		Short: "Start all services declared in packets.yaml for a Subspace",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			dir := "."
			if len(args) > 0 {
				dir = args[0]
			}

			df, err := devfile.Load(dir)
			if err != nil {
				return fmt.Errorf("load devfile: %w", err)
			}
			if df == nil || len(df.Services) == 0 {
				return fmt.Errorf("no services defined in devfile in %s", dir)
			}

			conn, err := DialScheduler(ctx, cfg)
			if err != nil {
				return fmt.Errorf("connect to packetsd: %w", err)
			}
			defer func() { _ = conn.Close() }()

			subID, err := resolveSubspaceID(ctx, conn, subspaceFlag)
			if err != nil {
				return err
			}

			if driverFlag == "" {
				driverFlag = "process"
			}

			client := pb.NewRemoteServiceServiceClient(conn)
			for _, s := range df.Services {
				var cmdArgs []string
				if s.Command != "" {
					cmdArgs = strings.Fields(s.Command)
				}
				req := &pb.StartServiceRequest{
					SubspaceId:  subID,
					Name:        s.Name,
					Image:       s.Image,
					Command:     cmdArgs,
					Ports:       s.Ports,
					Environment: s.Environment,
					Driver:      driverFlag,
				}
				resp, err := client.StartService(ctx, req)
				if err != nil {
					return fmt.Errorf("start service %s: %w", s.Name, err)
				}
				fmt.Printf("✓ Service %s started (status: %s, driver: %s)\n", resp.Service.Name, resp.Service.Status, resp.Service.Driver)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&subspaceFlag, "subspace", "", "Target remote Subspace ID")
	cmd.Flags().StringVar(&driverFlag, "driver", "process", "Service driver backend (process, docker)")

	return cmd
}

func newServiceDownCommand(cfg *config.Config, _ *slog.Logger) *cobra.Command {
	var (
		subspaceFlag string
		allFlag      bool
	)

	cmd := &cobra.Command{
		Use:   "down [dir]",
		Short: "Stop services in a Subspace",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			dir := "."
			if len(args) > 0 {
				dir = args[0]
			}

			conn, err := DialScheduler(ctx, cfg)
			if err != nil {
				return fmt.Errorf("connect to packetsd: %w", err)
			}
			defer func() { _ = conn.Close() }()

			subID, err := resolveSubspaceID(ctx, conn, subspaceFlag)
			if err != nil {
				return err
			}

			client := pb.NewRemoteServiceServiceClient(conn)

			var serviceNames []string
			if !allFlag {
				df, err := devfile.Load(dir)
				if err == nil && df != nil && len(df.Services) > 0 {
					for _, s := range df.Services {
						serviceNames = append(serviceNames, s.Name)
					}
				}
			}

			if len(serviceNames) == 0 {
				listResp, err := client.ListServices(ctx, &pb.ListServicesRequest{SubspaceId: subID})
				if err != nil {
					return fmt.Errorf("list services: %w", err)
				}
				for _, s := range listResp.Services {
					if s.Status == "running" || s.Status == "starting" {
						serviceNames = append(serviceNames, s.Name)
					}
				}
			}

			if len(serviceNames) == 0 {
				fmt.Printf("No running services to stop in subspace %s\n", subID)
				return nil
			}

			for _, name := range serviceNames {
				resp, err := client.StopService(ctx, &pb.StopServiceRequest{
					SubspaceId: subID,
					NameOrId:   name,
				})
				if err != nil {
					return fmt.Errorf("stop service %s: %w", name, err)
				}
				fmt.Printf("✓ Service %s stopped (status: %s)\n", resp.Service.Name, resp.Service.Status)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&subspaceFlag, "subspace", "", "Target remote Subspace ID")
	cmd.Flags().BoolVar(&allFlag, "all", false, "Stop all running services in the subspace")

	return cmd
}
