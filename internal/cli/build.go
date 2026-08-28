package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/waris4ly/packets/internal/config"
	"github.com/waris4ly/packets/internal/provider"
	"github.com/waris4ly/packets/internal/shim"
	"github.com/waris4ly/packets/internal/storage"
	"github.com/waris4ly/packets/internal/toolchain"
	"github.com/waris4ly/packets/pkg/apitypes"
	pb "github.com/waris4ly/packets/proto/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func NewBuildCommand(cfg *config.Config, logger *slog.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "build",
		Short: "Execute a remote build",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			pwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("failed to get working directory: %w", err)
			}

			registry := toolchain.NewRegistry()
			detector := shim.NewDetector(registry)
			router := shim.NewRouter(logger, detector)
			runner := shim.NewFallbackRunner(logger)

			def, err := detector.DetectToolchain(pwd)
			if err != nil {
				return fmt.Errorf("build failed: %w", err)
			}

			target, err := router.Route(ctx, pwd)
			if err != nil {
				return fmt.Errorf("routing failed: %w", err)
			}

			remoteCall := func(callCtx context.Context) error {
				if cfg.DirectCIMode {
					logger.InfoContext(callCtx, "Direct-CI mode enabled, bypassing scheduler and dispatching directly to GitHub Actions")
					if cfg.GitHubActionsToken == "" || cfg.GitHubActionsRepo == "" {
						return fmt.Errorf("GitHub Actions credentials missing for Direct-CI mode")
					}
					cacheKey, _ := GenerateCacheKey(callCtx, pwd, string(def.Name))
					job := apitypes.Job{
						ID:          apitypes.JobID(fmt.Sprintf("job_%d", time.Now().UnixNano())),
						Toolchain:   def.Name,
						CacheKey:    cacheKey,
						Provider:    apitypes.ProviderGitHubActions,
						State:       apitypes.JobStatePending,
					}
					
					ghProvider := provider.NewGitHubActions(logger, cfg.GitHubActionsToken, cfg.GitHubActionsRepo)
					_, err := ghProvider.Dispatch(callCtx, job)
					if err != nil {
						return fmt.Errorf("direct CI dispatch failed: %w", err)
					}
					
					logger.InfoContext(callCtx, "Direct CI job dispatched successfully (fire-and-forget)")
					return nil
				}

				if target.Backend == apitypes.BackendScheduler {
					return executeViaScheduler(callCtx, cfg, logger, def, pwd, args)
				}
				// other backends bypass scheduler (e.g. sccache-dist, gradle cache)
				// in MVP if it's not scheduler we simulate remote action
				// or let fallback catch it and run locally
				return shim.ErrRemoteUnreachable
			}

			return runner.ExecuteWithFallback(ctx, def, pwd, args, remoteCall)
		},
	}
}

func executeViaScheduler(ctx context.Context, cfg *config.Config, logger *slog.Logger, def apitypes.ToolchainDef, dir string, args []string) error {
	addr := cfg.OracleVMTailscaleHost + cfg.SchedulerAddr()

	remoteURI := fmt.Sprintf("ubuntu@%s:/tmp/packets-builds/%s", cfg.OracleVMTailscaleHost, filepath.Base(dir))
	sync := storage.NewMutagenSync(logger, dir, remoteURI)

	logger.Info("starting mutagen sync", slog.String("remote", remoteURI))
	if err := sync.Start(ctx); err != nil {
		logger.Warn("mutagen sync failed to start, build will proceed but may lack local changes", slog.String("error", err.Error()))
	}
	defer func() {
		logger.Info("terminating mutagen sync")
		_ = sync.Stop(context.Background())
	}()

	dialCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(dialCtx, addr, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	if err != nil {
		return fmt.Errorf("dial scheduler: %w", err)
	}
	defer conn.Close()

	client := pb.NewSchedulerClient(conn)

	cacheKey, err := GenerateCacheKey(ctx, dir, string(def.Name))
	if err != nil {
		logger.WarnContext(ctx, "failed to generate precise cache key, caching might be suboptimal", slog.String("error", err.Error()))
		cacheKey = "fallback_key_" + string(def.Name)
	}

	resp, err := client.SubmitJob(ctx, &pb.SubmitJobRequest{
		CacheKey:    cacheKey,
		Toolchain:   string(def.Name),
		DockerImage: def.DockerImage,
	})
	if err != nil {
		return fmt.Errorf("SubmitJob: %w", err)
	}

	logger.InfoContext(ctx, "job submitted",
		slog.String("job_id", resp.JobId),
		slog.Bool("cache_hit", resp.CacheHit),
	)

	return nil
}
