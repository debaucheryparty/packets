package cli

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/debaucheryparty/packets/internal/config"
	"github.com/debaucheryparty/packets/internal/project"
	"github.com/debaucheryparty/packets/internal/workspace"
	"github.com/debaucheryparty/packets/pkg/apitypes"
	pb "github.com/debaucheryparty/packets/proto/v1"
	"github.com/spf13/cobra"
)

func NewFirmwareCommand(cfg *config.Config, logger *slog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "firmware",
		Short: "Remote firmware development (Zephyr RTOS)",
	}

	cmd.AddCommand(newFirmwareBuildCommand(cfg, logger))
	return cmd
}

func newFirmwareBuildCommand(cfg *config.Config, logger *slog.Logger) *cobra.Command {
	var (
		boardFlag    string
		waitFlag     bool
		forceFlag    bool
		pristineFlag bool
	)

	cmd := &cobra.Command{
		Use:   "build [dir]",
		Short: "Run a remote Zephyr west build and retrieve firmware artifacts",
		Example: `  packets firmware build .
  packets firmware build . --board halo
  packets firmware build . --board nrf52840dk/nrf52840 --pristine`,
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

			hasWest := fileExists(filepath.Join(absDir, "west.yml")) || fileExists(filepath.Join(absDir, ".west", "config"))
			hasPrj := fileExists(filepath.Join(absDir, "prj.conf"))
			if !hasWest && !hasPrj {
				return fmt.Errorf("no Zephyr project found in %s\n  Required: west.yml or prj.conf", absDir)
			}

			board := boardFlag
			if board == "" {
				board = "halo"
			}

			fmt.Printf("✓ Zephyr project detected (board: %s)\n", board)

			conn, err := DialScheduler(ctx, cfg)
			if err != nil {
				return fmt.Errorf("connect to scheduler: %w", err)
			}
			defer conn.Close()

			fmt.Println("Uploading workspace to remote persistent node...")
			uploadStart := time.Now()
			snapshotRef, err := workspace.UploadWorkspace(ctx, conn, absDir, forceFlag)
			if err != nil {
				return fmt.Errorf("workspace sync: %w", err)
			}
			fmt.Printf("✓ Workspace uploaded (ref: %s, duration: %s)\n", snapshotRef, time.Since(uploadStart).Round(time.Millisecond))

			buildArgs := []string{"build"}
			if board != "" {
				buildArgs = append(buildArgs, "-b", board)
			}
			if pristineFlag {
				buildArgs = append(buildArgs, "-p", "always")
			} else {
				buildArgs = append(buildArgs, "-p", "auto")
			}

			artifacts := []string{
				"build/zephyr/zephyr.bin",
				"build/zephyr/zephyr.hex",
				"build/zephyr/zephyr.elf",
				"build/zephyr/zephyr.map",
			}

			cacheKey := fmt.Sprintf("zephyr:%s:%s:%s", board, snapshotRef, time.Now().Truncate(time.Minute).Format("1504"))

			projectID := project.ResolveProjectID(absDir)
			client := pb.NewSchedulerClient(conn)
			resp, err := client.SubmitJob(ctx, &pb.SubmitJobRequest{
				CacheKey:      cacheKey,
				Toolchain:     string(apitypes.ToolchainZephyr),
				Runner:        string(apitypes.RunnerHost),
				SourceMode:    string(apitypes.SourceModeWorkspace),
				SnapshotRef:   snapshotRef,
				CommandArgs:   append([]string{"west"}, buildArgs...),
				ArtifactPaths: artifacts,
				ProjectId:     projectID,
			})
			if err != nil {
				return fmt.Errorf("submit job: %w", err)
			}

			fmt.Printf("Building firmware (west %s)...\n", board)

			if waitFlag {
				err := pollJobStatus(ctx, cfg, client, resp.JobId, absDir, logger)
				if err != nil {
					return err
				}
				fmt.Println("✓ Firmware build completed. Artifacts extracted to build/zephyr/")
				return nil
			}

			fmt.Printf("Job submitted: %s\n", resp.JobId)
			return nil
		},
	}

	cmd.Flags().StringVarP(&boardFlag, "board", "b", "halo", "Target Zephyr board (e.g. halo, nrf52840dk/nrf52840)")
	cmd.Flags().BoolVar(&waitFlag, "wait", true, "Wait for build to complete and download firmware")
	cmd.Flags().BoolVar(&forceFlag, "force", false, "Force re-upload of workspace")
	cmd.Flags().BoolVarP(&pristineFlag, "pristine", "p", false, "Pristine build (clean build directory before compiling)")

	return cmd
}

func fileExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}
