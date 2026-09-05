package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/debaucheryparty/packets/internal/config"
	"github.com/debaucheryparty/packets/internal/shim"
	"github.com/debaucheryparty/packets/internal/toolchain"
	"github.com/debaucheryparty/packets/internal/workspace"
	"github.com/debaucheryparty/packets/pkg/apitypes"
	pb "github.com/debaucheryparty/packets/proto/v1"
	"github.com/spf13/cobra"
)

func NewBuildCommand(cfg *config.Config, logger *slog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "build",
		Short: "Execute a remote build",
	}

	cmd.Flags().String("runner", "", "Runner: docker, github, local")
	cmd.Flags().String("source", "", "Source mode: workspace, git")
	cmd.Flags().String("ref", "", "Git ref for --source=git")
	cmd.Flags().StringArray("artifact", nil, "Artifact paths to collect")
	cmd.Flags().Bool("wait", false, "Wait for build completion")
	cmd.Flags().String("provider", "", "Named provider from config")
	cmd.Flags().Bool("force", false, "Re-upload entire workspace")
	cmd.Flags().Bool("dry-run", false, "Show what would be uploaded without building")

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		runnerFlag, _ := cmd.Flags().GetString("runner")
		sourceFlag, _ := cmd.Flags().GetString("source")
		artifactFlag, _ := cmd.Flags().GetStringArray("artifact")
		waitFlag, _ := cmd.Flags().GetBool("wait")
		forceFlag, _ := cmd.Flags().GetBool("force")
		dryRunFlag, _ := cmd.Flags().GetBool("dry-run")
		providerFlag, _ := cmd.Flags().GetString("provider")

		pwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get working directory: %w", err)
		}

		projCfg, _ := config.LoadProjectConfig(pwd)
		if providerFlag == "" && projCfg != nil && projCfg.Provider != "" {
			providerFlag = projCfg.Provider
		}

		if runnerFlag == "" && providerFlag != "" {
			globalProviders, err := config.LoadGlobalProviders()
			if err == nil {
				if p, exists := globalProviders.Providers[providerFlag]; exists {
					switch p.Type {
					case "ssh-docker":
						runnerFlag = string(apitypes.RunnerDocker)
					case "github-actions":
						runnerFlag = string(apitypes.RunnerGitHub)
					case "local":
						runnerFlag = string(apitypes.RunnerLocal)
					}
				}
			}
		}

		registry := toolchain.NewRegistry()
		detector := shim.NewDetector(registry)

		def, err := detector.DetectToolchain(pwd)
		if err != nil {
			return fmt.Errorf("build failed: %w", err)
		}

		if len(artifactFlag) == 0 && projCfg != nil && len(projCfg.Build.Artifacts) > 0 {
			artifactFlag = projCfg.Build.Artifacts
		}
		if len(artifactFlag) == 0 && len(def.DefaultArtifacts) > 0 {
			artifactFlag = def.DefaultArtifacts
		}

		runner := apitypes.RunnerName(runnerFlag)
		if runner == "" {
			runner = apitypes.RunnerName(cfg.DefaultRunner)
		}
		if runner == "" {
			runner = apitypes.RunnerDocker
		}

		sourceMode := apitypes.SourceMode(sourceFlag)
		if sourceMode == "" {
			sourceMode = apitypes.SourceModeWorkspace
		}

		if dryRunFlag {
			manifest, err := workspace.ScanWorkspace(pwd, nil)
			if err != nil {
				return fmt.Errorf("dry-run scan: %w", err)
			}
			fmt.Printf("Dry-run: detected %d files (root hash: %s)\n", len(manifest.Files), manifest.RootHash)
			for _, f := range manifest.Files {
				if !f.IsDir {
					fmt.Printf("  %s (size: %d bytes, hash: %s)\n", f.Path, f.Size, f.Hash)
				}
			}
			return nil
		}

		return executeViaScheduler(ctx, cfg, logger, def, pwd, args, runner, sourceMode, artifactFlag, waitFlag, forceFlag)
	}

	return cmd
}

func executeViaScheduler(
	ctx context.Context,
	cfg *config.Config,
	logger *slog.Logger,
	def apitypes.ToolchainDef,
	dir string,
	args []string,
	runner apitypes.RunnerName,
	sourceMode apitypes.SourceMode,
	artifactPaths []string,
	wait bool,
	force bool,
) error {
	conn, err := DialScheduler(ctx, cfg)
	if err != nil {
		return err
	}
	defer conn.Close() //nolint:errcheck

	var snapshotRef string
	if sourceMode == apitypes.SourceModeWorkspace {
		logger.InfoContext(ctx, "uploading workspace", slog.String("runner", string(runner)))
		snapshotRef, err = workspace.UploadWorkspace(ctx, conn, dir, force)
		if err != nil {
			return fmt.Errorf("workspace upload: %w", err)
		}
		logger.InfoContext(ctx, "workspace ready", slog.String("snapshot_ref", snapshotRef))
	}

	cacheKey, err := GenerateCacheKey(ctx, dir, string(def.Name), string(runner), string(sourceMode), snapshotRef)
	if err != nil {
		logger.WarnContext(ctx, "cache key generation failed, using fallback", slog.String("error", err.Error()))
		cacheKey = fmt.Sprintf("%s:%s:%s", def.Name, runner, snapshotRef)
	}

	client := pb.NewSchedulerClient(conn)

	resp, err := client.SubmitJob(ctx, &pb.SubmitJobRequest{
		CacheKey:      cacheKey,
		Toolchain:     string(def.Name),
		DockerImage:   def.DockerImage,
		Runner:        string(runner),
		SourceMode:    string(sourceMode),
		SnapshotRef:   snapshotRef,
		CommandArgs:   args,
		ArtifactPaths: artifactPaths,
	})
	if err != nil {
		return fmt.Errorf("SubmitJob: %w", err)
	}

	logger.InfoContext(ctx, "job submitted",
		slog.String("job_id", resp.JobId),
		slog.Bool("cache_hit", resp.CacheHit),
	)

	if wait {
		return pollJobStatus(ctx, cfg, client, resp.JobId, dir, logger)
	}

	return nil
}

func pollJobStatus(ctx context.Context, cfg *config.Config, client pb.SchedulerClient, jobID, dir string, logger *slog.Logger) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			resp, err := client.GetJobStatus(ctx, &pb.GetJobStatusRequest{JobId: jobID})
			if err != nil {
				return fmt.Errorf("GetJobStatus: %w", err)
			}
			switch resp.State {
			case pb.JobState_JOB_STATE_SUCCEEDED:
				logger.Info("build succeeded", slog.String("artifact_ref", resp.ArtifactRef))
				if resp.ArtifactRef != "" {
					logger.Info("automatically pulling build artifacts into local workspace...")
					if pullErr := PullAndExtractArtifact(ctx, cfg, logger, jobID, dir); pullErr != nil {
						logger.Warn("auto-pull artifact failed", slog.String("error", pullErr.Error()))
					}
				}
				return nil
			case pb.JobState_JOB_STATE_FAILED:
				return fmt.Errorf("build failed: %s", resp.ErrorMessage)
			default:
				logger.Info("build in progress", slog.String("state", resp.State.String()))
			}
		}
	}
}
