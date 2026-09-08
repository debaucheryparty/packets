package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/debaucheryparty/packets/internal/config"
	"github.com/debaucheryparty/packets/internal/project"
	"github.com/debaucheryparty/packets/internal/workspace"
	"github.com/debaucheryparty/packets/pkg/apitypes"
	pb "github.com/debaucheryparty/packets/proto/v1"
	"github.com/spf13/cobra"
)

func NewExecCommand(cfg *config.Config, logger *slog.Logger) *cobra.Command {
	var (
		runnerFlag   string
		noSyncFlag   bool
		artifactFlag []string
		providerFlag string
		timeoutFlag  time.Duration
	)

	cmd := &cobra.Command{
		Use:   "exec [flags] <command...>",
		Short: "Execute a command remotely on the persistent VPS workspace",
		Example: `  packets exec "ls -la"
  packets exec "./gradlew assembleDebug"
  packets exec -- uname -a
  packets exec --no-sync "cat /etc/os-release"`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if timeoutFlag > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, timeoutFlag)
				defer cancel()
			}

			pwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("failed to get working directory: %w", err)
			}

			conn, err := DialScheduler(ctx, cfg)
			if err != nil {
				return fmt.Errorf("connect to scheduler: %w", err)
			}
			defer conn.Close()

			var snapshotRef string
			if !noSyncFlag {
				logger.InfoContext(ctx, "synchronizing workspace...")
				ref, err := workspace.UploadWorkspace(ctx, conn, pwd, false)
				if err != nil {
					return fmt.Errorf("workspace sync failed: %w", err)
				}
				snapshotRef = ref
				logger.InfoContext(ctx, "workspace ready", slog.String("snapshot_ref", snapshotRef))
			}

			runner := apitypes.RunnerName(runnerFlag)
			if runner == "" {
				runner = apitypes.RunnerHost
			}

			cacheKey := fmt.Sprintf("exec:%s:%d", snapshotRef, time.Now().UnixNano())

			client := pb.NewSchedulerClient(conn)
			resp, err := client.SubmitJob(ctx, &pb.SubmitJobRequest{
				CacheKey:      cacheKey,
				Toolchain:     string(apitypes.ToolchainExec),
				Runner:        string(runner),
				SourceMode:    string(apitypes.SourceModeWorkspace),
				SnapshotRef:   snapshotRef,
				CommandArgs:   args,
				ArtifactPaths: artifactFlag,
				ProjectId:     project.ResolveProjectID(pwd),
			})
			if err != nil {
				return fmt.Errorf("SubmitJob: %w", err)
			}

			streamErrCh := make(chan error, 1)
			go func() {
				stream, err := client.StreamJobLogs(ctx, &pb.StreamJobLogsRequest{JobId: resp.JobId})
				if err != nil {
					streamErrCh <- err
					return
				}
				for {
					line, err := stream.Recv()
					if err != nil {
						break
					}
					fmt.Println(line.Content)
				}
				streamErrCh <- nil
			}()

			ticker := time.NewTicker(500 * time.Millisecond)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-ticker.C:
					statusResp, err := client.GetJobStatus(ctx, &pb.GetJobStatusRequest{JobId: resp.JobId})
					if err != nil {
						return fmt.Errorf("GetJobStatus: %w", err)
					}
					switch statusResp.State {
					case pb.JobState_JOB_STATE_SUCCEEDED:
						<-streamErrCh
						if len(artifactFlag) > 0 && statusResp.ArtifactRef != "" {
							if err := PullAndExtractArtifact(ctx, cfg, logger, resp.JobId, pwd); err != nil {
								logger.WarnContext(ctx, "pull artifacts failed", slog.String("err", err.Error()))
							}
						}
						return nil
					case pb.JobState_JOB_STATE_FAILED:
						<-streamErrCh
						return fmt.Errorf("remote command failed: %s", statusResp.ErrorMessage)
					}
				}
			}
		},
	}

	cmd.Flags().StringVar(&runnerFlag, "runner", "host", "Runner mode: host, docker, local")
	cmd.Flags().BoolVar(&noSyncFlag, "no-sync", false, "Skip synchronizing local workspace before execution")
	cmd.Flags().StringArrayVar(&artifactFlag, "artifact", nil, "Artifact paths to collect after execution")
	cmd.Flags().StringVar(&providerFlag, "provider", "", "Named provider from config")
	cmd.Flags().DurationVar(&timeoutFlag, "timeout", 0, "Command execution timeout (e.g. 5m, 30s)")

	return cmd
}
