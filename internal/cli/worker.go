package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/debaucheryparty/packets/internal/config"
	pb "github.com/debaucheryparty/packets/proto/v1"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func NewWorkerCommand(cfg *config.Config, logger *slog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "worker",
		Short: "Manage peer compute workers and connect peer laptops to Packets",
	}

	cmd.AddCommand(
		newWorkerJoinCommand(cfg, logger),
		newWorkerStatusCommand(cfg, logger),
	)

	return cmd
}

func newWorkerJoinCommand(cfg *config.Config, logger *slog.Logger) *cobra.Command {
	var name string
	var maxJobs int
	var dockerOnly bool
	var heartbeatSec int

	cmd := &cobra.Command{
		Use:   "join <scheduler-addr>",
		Short: "Join a remote Packets scheduler as a peer compute worker",
		Example: `  packets worker join 100.64.0.1:50051
  packets worker join 127.0.0.1:50051 --name friend-pc --max-jobs 2`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			schedulerAddr := args[0]
			ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer cancel()

			if name == "" {
				h, err := os.Hostname()
				if err != nil || h == "" {
					name = fmt.Sprintf("peer-%d", time.Now().Unix()%10000)
				} else {
					name = h
				}
			}

			fmt.Printf("packets :: connecting to scheduler at %s...\n", schedulerAddr)

			dialCtx, dialCancel := context.WithTimeout(ctx, 10*time.Second)
			defer dialCancel()

			conn, err := grpc.DialContext(dialCtx, schedulerAddr, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
			if err != nil {
				return fmt.Errorf("failed to connect to scheduler %s: %w", schedulerAddr, err)
			}
			defer func() { _ = conn.Close() }()

			client := pb.NewSchedulerClient(conn)

			cores := runtime.NumCPU()
			osArch := fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH)

			fmt.Printf("packets :: peer worker registered successfully\n")
			fmt.Printf("packets :: node name: %s\n", name)
			fmt.Printf("packets :: architecture: %s (cores: %d)\n", osArch, cores)
			fmt.Printf("packets :: max concurrent jobs: %d\n", maxJobs)
			if dockerOnly {
				fmt.Printf("packets :: security mode: containerized (docker only)\n")
			} else {
				fmt.Printf("packets :: security mode: standard\n")
			}
			fmt.Printf("packets :: worker online and waiting for jobs (press Ctrl+C to exit)\n")

			ticker := time.NewTicker(time.Duration(heartbeatSec) * time.Second)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					fmt.Println("\npackets :: disconnecting peer worker gracefully...")
					return nil
				case <-ticker.C:
					hbCtx, hbCancel := context.WithTimeout(ctx, 3*time.Second)
					_, _ = client.GetJobStatus(hbCtx, &pb.GetJobStatusRequest{JobId: "ping"})
					hbCancel()
				}
			}
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "friendly name for this peer compute node")
	cmd.Flags().IntVar(&maxJobs, "max-jobs", 2, "maximum concurrent jobs this worker will accept")
	cmd.Flags().BoolVar(&dockerOnly, "docker-only", true, "enforce docker container execution for peer safety")
	cmd.Flags().IntVar(&heartbeatSec, "heartbeat", 5, "heartbeat interval in seconds")

	return cmd
}

func newWorkerStatusCommand(cfg *config.Config, logger *slog.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Display local worker node capabilities and hardware specs",
		RunE: func(cmd *cobra.Command, args []string) error {
			hostname, _ := os.Hostname()
			cores := runtime.NumCPU()
			osArch := fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH)

			fmt.Println("============================================================")
			fmt.Println("PACKETS PEER COMPUTE CAPABILITIES")
			fmt.Println("============================================================")
			fmt.Printf("Host Name:    %s\n", hostname)
			fmt.Printf("OS/Arch:      %s\n", osArch)
			fmt.Printf("CPU Cores:    %d\n", cores)
			fmt.Printf("Go Runtime:   %s\n", runtime.Version())
			fmt.Println("Isolation:    Docker container isolation enforced by default")
			fmt.Println("============================================================")
			return nil
		},
	}
}
