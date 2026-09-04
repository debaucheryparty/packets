package cli

import (
	"fmt"
	"log/slog"

	"github.com/debaucheryparty/packets/internal/config"
	pb "github.com/debaucheryparty/packets/proto/v1"
	"github.com/spf13/cobra"
)

func NewLogsCommand(cfg *config.Config, logger *slog.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "logs <job-id>",
		Short: "Stream logs for a job",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			jobID := args[0]

			conn, err := DialScheduler(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			defer conn.Close()

			client := pb.NewSchedulerClient(conn)
			stream, err := client.StreamJobLogs(cmd.Context(), &pb.StreamJobLogsRequest{JobId: jobID})
			if err != nil {
				return fmt.Errorf("StreamJobLogs: %w", err)
			}

			for {
				line, err := stream.Recv()
				if err != nil {
					break
				}
				fmt.Println(line.Content)
			}
			return nil
		},
	}
}

func NewArtifactCommand(cfg *config.Config, logger *slog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "artifact",
		Short: "Manage build artifacts",
	}

	pullCmd := &cobra.Command{
		Use:   "pull <job-id>",
		Short: "Download artifacts for a job",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			jobID := args[0]
			output, _ := cmd.Flags().GetString("output")

			return PullAndExtractArtifact(cmd.Context(), cfg, logger, jobID, output)
		},
	}
	pullCmd.Flags().StringP("output", "o", ".", "Output directory (defaults to current directory)")
	cmd.AddCommand(pullCmd)
	return cmd
}
