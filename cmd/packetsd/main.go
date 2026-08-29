package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lmittmann/tint"
	"github.com/waris4ly/packets/internal/config"
	"github.com/waris4ly/packets/internal/provider"
	"github.com/waris4ly/packets/internal/scheduler"
	"github.com/waris4ly/packets/internal/storage"
	"github.com/waris4ly/packets/internal/toolchain"
	"github.com/waris4ly/packets/internal/worker"
	"github.com/waris4ly/packets/internal/workspace"
	"github.com/waris4ly/packets/pkg/apitypes"
	pb "github.com/waris4ly/packets/proto/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

var version = "dev"

func main() {
	ctx := context.Background()

	cfg, err := config.LoadConfig(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	logger := slog.New(tint.NewHandler(os.Stderr, &tint.Options{
		Level:      cfg.ParseLogLevel(),
		TimeFormat: time.TimeOnly,
	}))

	if err := cfg.ValidateForDaemon(); err != nil {
		logger.Error("config validation failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	logger.Info("starting packetsd", slog.String("version", version))

	store, err := storage.NewJobStore(ctx, cfg.SQLiteDBPath)
	if err != nil {
		logger.Error("failed to init storage", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer store.Close()

	var objectStore storage.ObjectStore
	if cfg.ObjectStoreType != "" {
		objectStore, err = storage.NewS3ObjectStore(
			cfg.ObjectStoreEndpoint,
			cfg.ObjectStoreRegion,
			cfg.ObjectStoreAccessKey,
			cfg.ObjectStoreSecretKey,
			cfg.ObjectStoreBucket,
			cfg.ObjectStoreForcePathStyle,
		)
		if err != nil {
			logger.Error("failed to init object store", slog.String("error", err.Error()))
			os.Exit(1)
		}
	}

	logBroker := scheduler.NewLogBroker()
	quotaLimiter := scheduler.NewQuotaLimiter(cfg.MaxConcurrentJobsPerUser, cfg.MaxSubmissionsPerMinute)

	registry := toolchain.NewRegistry()
	dockerClient := worker.NewDockerClient(logger)
	executor := worker.NewExecutor(logger, dockerClient, objectStore, registry, logBroker, cfg.WorkspaceTempDir)

	providers := map[apitypes.ProviderName]provider.BuildProvider{
		apitypes.ProviderGitHubActions: provider.NewGitHubActions(logger, cfg.GitHubActionsToken, cfg.GitHubActionsRepo),
		apitypes.ProviderCircleCI:      provider.NewCircleCI(logger, cfg.CircleCIToken, cfg.CircleCIProjectSlug),
	}

	dispatcher := scheduler.NewDispatcher(logger, store, providers, executor, quotaLimiter, logBroker)

	if err := dispatcher.RecoverPendingJobs(ctx); err != nil {
		logger.Warn("job recovery failed", slog.String("error", err.Error()))
	}

	srv := scheduler.NewServer(dispatcher, store, logBroker, objectStore, providers)

	var serverOpts []grpc.ServerOption
	serverOpts = append(serverOpts,
		grpc.UnaryInterceptor(scheduler.TailscaleInterceptor()),
		grpc.StreamInterceptor(scheduler.TailscaleStreamInterceptor()),
	)

	if cfg.TLSCertFile != "" && cfg.TLSKeyFile != "" {
		creds, err := credentials.NewServerTLSFromFile(cfg.TLSCertFile, cfg.TLSKeyFile)
		if err != nil {
			logger.Error("failed to load TLS certificate", slog.String("error", err.Error()))
			os.Exit(1)
		}
		serverOpts = append(serverOpts, grpc.Creds(creds))
		logger.Info("TLS enabled for gRPC server")
	}

	grpcServer := grpc.NewServer(serverOpts...)
	pb.RegisterSchedulerServer(grpcServer, srv)

	if objectStore != nil {
		wsSrv := workspace.NewServer(objectStore, registry)
		pb.RegisterWorkspaceServer(grpcServer, wsSrv)
	}

	listener, err := net.Listen("tcp", cfg.SchedulerAddr())
	if err != nil {
		logger.Error("failed to listen", slog.String("error", err.Error()))
		os.Exit(1)
	}

	go func() {
		logger.Info("grpc server listening", slog.String("addr", cfg.SchedulerAddr()))
		if err := grpcServer.Serve(listener); err != nil {
			logger.Error("grpc server stopped", slog.String("error", err.Error()))
		}
	}()

	go func() {
		metricsAddr := ":9090"
		logger.Info("metrics server listening", slog.String("addr", metricsAddr))
		if err := http.ListenAndServe(metricsAddr, scheduler.MetricsHandler()); err != nil {
			logger.Error("metrics server stopped", slog.String("error", err.Error()))
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	logger.Info("shutting down gracefully...")
	stopped := make(chan struct{})
	go func() {
		grpcServer.GracefulStop()
		close(stopped)
	}()

	select {
	case <-stopped:
		logger.Info("shutdown complete")
	case <-time.After(10 * time.Second):
		logger.Warn("shutdown timed out, forcing exit")
		grpcServer.Stop()
	}
}
