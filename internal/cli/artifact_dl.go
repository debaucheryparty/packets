package cli

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

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
	defer conn.Close()

	client := pb.NewSchedulerClient(conn)
	stream, err := client.DownloadArtifact(ctx, &pb.DownloadArtifactRequest{JobId: jobID})
	if err != nil {
		return fmt.Errorf("DownloadArtifact: %w", err)
	}

	var buf bytes.Buffer
	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
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
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		if err := workspace.ExtractTarGz(bytes.NewReader(data), destDir); err != nil {
			return fmt.Errorf("extract tar.gz artifact: %w", err)
		}
		logger.InfoContext(ctx, "artifact extracted successfully", slog.String("dest", destDir))
		return nil
	}

	if len(data) >= 4 && data[0] == 0x50 && data[1] == 0x4b && data[2] == 0x03 && data[3] == 0x04 {
		zipReader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return fmt.Errorf("open zip artifact: %w", err)
		}
		for _, zf := range zipReader.File {
			outPath := filepath.Join(destDir, filepath.FromSlash(zf.Name))
			if zf.FileInfo().IsDir() {
				_ = os.MkdirAll(outPath, 0o755)
				continue
			}
			_ = os.MkdirAll(filepath.Dir(outPath), 0o755)
			rc, err := zf.Open()
			if err != nil {
				continue
			}
			outFile, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, zf.Mode())
			if err == nil {
				_, _ = io.Copy(outFile, rc)
				outFile.Close()
			}
			rc.Close()
		}
		logger.InfoContext(ctx, "zip artifact extracted successfully", slog.String("dest", destDir))
		return nil
	}

	rawFile := filepath.Join(destDir, fmt.Sprintf("artifact_%s.bin", jobID))
	if err := os.WriteFile(rawFile, data, 0o644); err != nil {
		return fmt.Errorf("write raw artifact: %w", err)
	}
	logger.InfoContext(ctx, "raw artifact saved", slog.String("file", rawFile))
	return nil
}
