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
				fmt.Printf("packets :: daemon online (%s)\n", cfg.SchedulerAddr())
				fmt.Printf("packets :: status ready (query jobs via 'packets status <job-id>')\n")
				return nil
			}

			jobID := args[0]

			client := pb.NewSchedulerClient(conn)
			resp, err := client.GetJobStatus(cmd.Context(), &pb.GetJobStatusRequest{JobId: jobID})
			if err != nil {
				return fmt.Errorf("packets :: job %s: %w", jobID, err)
			}

			fmt.Printf("packets :: job %s [%s]\n", jobID, resp.State.String())
			if resp.ArtifactRef != "" {
				fmt.Printf("packets :: artifact: %s\n", resp.ArtifactRef)
			}
			if resp.ErrorMessage != "" {
				fmt.Printf("packets :: error: %s\n", resp.ErrorMessage)
			}

			return nil
		},
	}
}
