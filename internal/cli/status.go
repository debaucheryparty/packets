package cli

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/spf13/cobra"
	"github.com/waris4ly/packets/internal/config"
	pb "github.com/waris4ly/packets/proto/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func NewStatusCommand(cfg *config.Config, logger *slog.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "View the status of recent builds",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("job ID required")
			}
			jobID := args[0]

			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()

			addr := cfg.OracleVMTailscaleHost + cfg.SchedulerAddr()
			conn, err := grpc.DialContext(ctx, addr, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
			if err != nil {
				return fmt.Errorf("dial scheduler: %w", err)
			}
			defer conn.Close()

			client := pb.NewSchedulerClient(conn)
			resp, err := client.GetJobStatus(ctx, &pb.GetJobStatusRequest{JobId: jobID})
			if err != nil {
				return fmt.Errorf("GetJobStatus: %w", err)
			}

			fmt.Printf("Job ID: %s\n", jobID)
			fmt.Printf("State: %s\n", resp.State.String())
			if resp.ArtifactRef != "" {
				fmt.Printf("Artifact: %s\n", resp.ArtifactRef)
			}
			if resp.ErrorMessage != "" {
				fmt.Printf("Error: %s\n", resp.ErrorMessage)
			}

			return nil
		},
	}
}
