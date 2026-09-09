package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/debaucheryparty/packets/internal/config"
	"github.com/debaucheryparty/packets/internal/workspace"
	pb "github.com/debaucheryparty/packets/proto/v1"
)

func PullAndExtractArtifact(ctx context.Context, cfg *config.Config, logger *slog.Logger, jobID, destDir string) error {
	if destDir == "" {
		destDir = "."
	}

	conn, err := DialScheduler(ctx, cfg)
	if err != nil {
		return err
	}
	defer conn.Close() //nolint:errcheck

	client := pb.NewSchedulerClient(conn)
	stream, err := client.DownloadArtifact(ctx, &pb.DownloadArtifactRequest{JobId: jobID})
	if err != nil {
		return fmt.Errorf("DownloadArtifact: %w", err)
	}

	var buf bytes.Buffer
	for {
		chunk, err := stream.Recv()
		if err == io.EOF { //nolint:errorlint
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
	if err := workspace.ExtractArtifact(data, destDir, fmt.Sprintf("artifact_%s.bin", jobID)); err != nil {
		return err
	}
	logger.InfoContext(ctx, "artifact extracted successfully", slog.String("dest", destDir))
	return nil
}
