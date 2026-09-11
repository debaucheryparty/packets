package cli

import (
	"fmt"
	"log/slog"

	"github.com/debaucheryparty/packets/internal/config"
	pb "github.com/debaucheryparty/packets/proto/v1"
	"github.com/spf13/cobra"
)

func NewStatusCommand(cfg *config.Config, logger *slog.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "status [job-id]",
		Short: "View the status of the Packets daemon or a specific job",
		Example: `  packets status
  packets status j_12345678`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			conn, err := DialScheduler(cmd.Context(), cfg)
			if err != nil {
				return fmt.Errorf("packetsd unreachable: %w", err)
			}
			defer func() { _ = conn.Close() }()

			if len(args) == 0 {
				fmt.Printf("✓ Packets scheduler daemon is active, healthy, and reachable via gRPC.\n")
				fmt.Printf("  Address: %s\n", cfg.SchedulerAddr())
				fmt.Printf("  Use 'packets status <job-id>' for details on a specific job.\n")
				return nil
			}

			jobID := args[0]

			client := pb.NewSchedulerClient(conn)
			resp, err := client.GetJobStatus(cmd.Context(), &pb.GetJobStatusRequest{JobId: jobID})
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
