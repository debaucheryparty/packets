package cli

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/debaucheryparty/packets/internal/config"
	"github.com/debaucheryparty/packets/internal/project"
	"github.com/debaucheryparty/packets/internal/shim"
	"github.com/debaucheryparty/packets/internal/toolchain"
	"github.com/debaucheryparty/packets/internal/workspace"
	"github.com/debaucheryparty/packets/pkg/apitypes"
	pb "github.com/debaucheryparty/packets/proto/v1"
	"github.com/spf13/cobra"
)

func NewTestCommand(cfg *config.Config, logger *slog.Logger) *cobra.Command {
	var (
		runnerFlag   string
		forceFlag    bool
		waitFlag     bool
		providerFlag string
	)

	cmd := &cobra.Command{
		Use:   "test [dir] [-- args...]",
		Short: "Execute remote tests on the scheduler",
		Long: `Runs automated tests remotely for the detected toolchain or specified directory.
Streams test execution logs and reports exit code.`,
		Example: `  packets test
  packets test .
  packets test ./mobile
  packets test --runner docker
  packets test -- cargo test --verbose`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			dir := "."
			var cmdArgs []string

			if len(args) > 0 && !stringsHasDashPrefix(args[0]) {
				dir = args[0]
				cmdArgs = args[1:]
			} else {
				cmdArgs = args
			}

			absDir, err := filepath.Abs(dir)
			if err != nil {
				return fmt.Errorf("resolve directory: %w", err)
			}

			registry := toolchain.NewRegistry()
			detector := shim.NewDetector(registry)
			def, err := detector.DetectToolchain(absDir)
			if err != nil {
				def = apitypes.ToolchainDef{
					Name: apitypes.ToolchainExec,
				}
			}

			if len(cmdArgs) == 0 {
				if len(def.DefaultArgs) > 0 {
					cmdArgs = []string{"test"}
				} else {
					cmdArgs = []string{"test"}
				}
			}

			runner := apitypes.RunnerName(runnerFlag)
			if runner == "" {
				runner = apitypes.RunnerName(cfg.DefaultRunner)
			}
			if runner == "" {
				runner = apitypes.RunnerHost
			}

			conn, err := DialScheduler(ctx, cfg)
			if err != nil {
				return fmt.Errorf("connect to scheduler: %w", err)
			}
			defer func() { _ = conn.Close() }()

			logger.InfoContext(ctx, "uploading workspace for test...")
			snapshotRef, err := workspace.UploadWorkspace(ctx, conn, absDir, forceFlag)
			if err != nil {
				return fmt.Errorf("workspace upload: %w", err)
			}

			projectID := project.ResolveProjectID(absDir)
			cacheKey := fmt.Sprintf("test:%s:%s:%d", projectID, snapshotRef, time.Now().UnixNano())

			client := pb.NewSchedulerClient(conn)
			resp, err := client.SubmitJob(ctx, &pb.SubmitJobRequest{
				CacheKey:    cacheKey,
				Toolchain:   string(def.Name),
				Runner:      string(runner),
				SourceMode:  string(apitypes.SourceModeWorkspace),
				SnapshotRef: snapshotRef,
				CommandArgs: cmdArgs,
				ProjectId:   projectID,
			})
			if err != nil {
				return fmt.Errorf("SubmitJob failed: %w", err)
			}

			logger.InfoContext(ctx, "test job submitted", slog.String("job_id", resp.JobId))

			if waitFlag {
				return streamAndWatchJob(ctx, client, resp.JobId)
			}

			fmt.Printf("packets :: test queued [%s] (stream with 'packets logs %s')\n", resp.JobId, resp.JobId)
			return nil
		},
	}

	cmd.Flags().StringVar(&runnerFlag, "runner", "", "Runner: host, docker, github, local")
	cmd.Flags().BoolVar(&forceFlag, "force", false, "Force re-upload of workspace")
	cmd.Flags().BoolVar(&waitFlag, "wait", true, "Wait and stream test logs (default: true)")
	cmd.Flags().StringVar(&providerFlag, "provider", "", "Named provider from config")

	return cmd
}

func stringsHasDashPrefix(s string) bool {
	return len(s) > 0 && s[0] == '-'
}

func streamAndWatchJob(ctx context.Context, client pb.SchedulerClient, jobID string) error {
	streamCtx, cancelStream := context.WithCancel(ctx)
	defer cancelStream()

	logStream, err := client.StreamJobLogs(streamCtx, &pb.StreamJobLogsRequest{JobId: jobID})
	if err == nil {
		go func() {
			for {
				line, err := logStream.Recv()
				if err != nil {
					break
				}
				fmt.Println(line.Content)
			}
		}()
	}

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			statusResp, err := client.GetJobStatus(ctx, &pb.GetJobStatusRequest{JobId: jobID})
			if err != nil {
				continue
			}
			if statusResp.State == pb.JobState_JOB_STATE_SUCCEEDED {
				cancelStream()
				time.Sleep(100 * time.Millisecond)
				fmt.Printf("\npackets :: tests passed [%s]\n", jobID)
				return nil
			}
			if statusResp.State == pb.JobState_JOB_STATE_FAILED {
				cancelStream()
				time.Sleep(100 * time.Millisecond)
				errMsg := statusResp.ErrorMessage
				if errMsg == "" {
					errMsg = "tests failed"
				}
				return fmt.Errorf("packets :: tests failed [%s]: %s", jobID, errMsg)
			}
		}
	}
}
