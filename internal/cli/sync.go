package cli

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/debaucheryparty/packets/internal/config"
	"github.com/debaucheryparty/packets/internal/workspace"
	pb "github.com/debaucheryparty/packets/proto/v1"
	"github.com/spf13/cobra"
)

func NewSyncCommand(cfg *config.Config, logger *slog.Logger) *cobra.Command {
	var (
		fullFlag   bool
		dryRunFlag bool
	)

	cmd := &cobra.Command{
		Use:   "sync [dir]",
		Short: "Synchronize local workspace to the remote VPS",
		Long: `Synchronizes local project files to the remote persistent workspace.
Supports incremental chunk synchronization and full synchronization with deletion detection.`,
		Example: `  packets sync .
  packets sync --full .
  packets sync --dry-run .`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			dir := "."
			if len(args) > 0 {
				dir = args[0]
			}

			absDir, err := filepath.Abs(dir)
			if err != nil {
				return fmt.Errorf("resolve directory: %w", err)
			}

			manifest, err := workspace.ScanWorkspace(absDir, nil)
			if err != nil {
				return fmt.Errorf("scan workspace: %w", err)
			}

			var totalSize int64
			fileCount := 0
			for _, f := range manifest.Files {
				if !f.IsDir {
					fileCount++
					totalSize += f.Size
				}
			}

			if dryRunFlag {
				fmt.Printf("Workspace Scan (Dry-Run):\n")
				fmt.Printf("  Directory: %s\n", absDir)
				fmt.Printf("  Files:     %d\n", fileCount)
				fmt.Printf("  Total:     %.2f MB\n", float64(totalSize)/(1024*1024))
				fmt.Printf("  Root Hash: %s\n\n", manifest.RootHash)
				for _, f := range manifest.Files {
					if !f.IsDir {
						fmt.Printf("  %s (%d bytes, hash: %s)\n", f.Path, f.Size, f.Hash[:12])
					}
				}
				return nil
			}

			start := time.Now()
			syncMode := "incremental"
			if fullFlag {
				syncMode = "full (with deletion detection)"
			}

			fmt.Printf("Synchronizing workspace (%s)...\n", syncMode)

			conn, err := DialScheduler(ctx, cfg)
			if err != nil {
				return fmt.Errorf("connect to scheduler: %w", err)
			}
			defer func() { _ = conn.Close() }()

			snapshotRef, err := workspace.UploadWorkspace(ctx, conn, absDir, fullFlag)
			if err != nil {
				return fmt.Errorf("workspace upload: %w", err)
			}

			wsClient := pb.NewWorkspaceClient(conn)
			verifyResp, err := wsClient.VerifySnapshot(ctx, &pb.VerifySnapshotRequest{
				SnapshotRef: snapshotRef,
			})
			if err != nil {
				return fmt.Errorf("verify snapshot: %w", err)
			}
			if !verifyResp.Exists {
				return fmt.Errorf("snapshot %s was not verified on remote workspace", snapshotRef)
			}

			duration := time.Since(start).Round(time.Millisecond)
			fmt.Printf("packets :: workspace synced [%s, %d files, %s]\n", snapshotRef[:12], fileCount, duration)

			return nil
		},
	}

	cmd.Flags().BoolVar(&fullFlag, "full", false, "Force full synchronization with remote deletion detection")
	cmd.Flags().BoolVar(&dryRunFlag, "dry-run", false, "Preview files that would be synchronized without uploading")

	return cmd
}
