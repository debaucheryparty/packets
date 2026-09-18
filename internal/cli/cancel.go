package cli

import (
	"fmt"
	"log/slog"

	"github.com/debaucheryparty/packets/internal/config"
	pb "github.com/debaucheryparty/packets/proto/v1"
	"github.com/spf13/cobra"
)

func NewCancelCommand(cfg *config.Config, _ *slog.Logger) *cobra.Command {
	return &cobra.Command{
		Use:     "cancel <job-id>",
		Short:   "Cancel a running or pending build job",
		Example: `  packets cancel j_12345678`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			conn, err := DialScheduler(cmd.Context(), cfg)
			if err != nil {
				return fmt.Errorf("packetsd unreachable: %w", err)
			}
			defer func() { _ = conn.Close() }()

			jobID := args[0]
			client := pb.NewSchedulerClient(conn)
			resp, err := client.CancelJob(cmd.Context(), &pb.CancelJobRequest{JobId: jobID})
			if err != nil {
				return fmt.Errorf("cancel job %s: %w", jobID, err)
			}

			if !resp.Cancelled {
				fmt.Printf("packets :: could not cancel job %s: %s\n", jobID, resp.Message)
				return nil
			}

			fmt.Printf("packets :: job %s cancelled successfully\n", jobID)
			return nil
		},
	}
}
