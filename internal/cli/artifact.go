package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/debaucheryparty/packets/internal/config"
	"github.com/debaucheryparty/packets/internal/workspace"
	pb "github.com/debaucheryparty/packets/proto/v1"
	"github.com/spf13/cobra"
)

// NewArtifactCommand creates the command to manage and download build artifacts.
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

// PullAndExtractArtifact fetches the remote artifact stream for jobID and extracts it into destDir.
func PullAndExtractArtifact(ctx context.Context, cfg *config.Config, logger *slog.Logger, jobID, destDir string) error {
	if destDir == "" {
		destDir = "."
	}

	conn, err := DialScheduler(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	client := pb.NewSchedulerClient(conn)
	stream, err := client.DownloadArtifact(ctx, &pb.DownloadArtifactRequest{JobId: jobID})
	if err != nil {
		return fmt.Errorf("DownloadArtifact: %w", err)
	}

	var buf bytes.Buffer
	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("stream chunk: %w", err)
		}
		buf.Write(chunk.Data)
	}

	if buf.Len() == 0 {
		return fmt.Errorf("received empty artifact")
	}

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("create dest dir: %w", err)
	}

	data := buf.Bytes()
	count, err := workspace.ExtractArtifactCount(data, destDir, fmt.Sprintf("artifact_%s.bin", jobID))
	if err != nil {
		return err
	}
	if count == 0 {
		logger.WarnContext(ctx, "artifact archive contained 0 files", slog.String("dest", destDir))
		fmt.Println("⚠ Note: The remote build completed, but no files matched the artifact patterns.")
	} else {
		logger.InfoContext(ctx, "artifact extracted successfully", slog.String("dest", destDir), slog.Int("files", count))
		fmt.Printf("✓ %d artifact file(s) extracted into %s\n", count, destDir)
	}
	return nil
}
