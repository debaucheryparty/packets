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
		Use:   "status",
		Short: "View the status of recent builds",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("job ID required")
			}
			jobID := args[0]

			conn, err := DialScheduler(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			defer conn.Close() //nolint:errcheck

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
