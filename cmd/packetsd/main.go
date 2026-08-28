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
	"github.com/waris4ly/packets/internal/worker"
	"github.com/waris4ly/packets/pkg/apitypes"
	pb "github.com/waris4ly/packets/proto/v1"
	"google.golang.org/grpc"
)

var version = "dev"

func main() {
	ctx := context.Background()

	// load config eagerly per Hard Rule 15
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

	// init storage
	store, err := storage.NewJobStore(ctx, cfg.SQLiteDBPath)
	if err != nil {
		logger.Error("failed to init storage", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer store.Close()

	// init NATS queue
	queue, err := storage.NewNATSQueue(ctx, logger, cfg.NATSUrl)
	if err != nil {
		logger.Error("failed to init nats", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer queue.Close()

	// init docker worker
	dockerClient := worker.NewDockerClient(logger)
	executor := worker.NewExecutor(logger, dockerClient, queue)

	// init CI providers
	providers := map[apitypes.ProviderName]provider.BuildProvider{
		apitypes.ProviderGitHubActions: provider.NewGitHubActions(logger, cfg.GitHubActionsToken, cfg.GitHubActionsRepo),
		apitypes.ProviderCircleCI:      provider.NewCircleCI(logger, cfg.CircleCIToken, cfg.CircleCIProjectSlug),
		apitypes.ProviderDockerWorker:  executor,
	}

	// init scheduler components
	dispatcher := scheduler.NewDispatcher(logger, store, queue, providers)

	// start worker pool
	workerPool := scheduler.NewWorkerPool(logger, queue, dispatcher, 4) // max 4 concurrent builds
	if err := workerPool.Start(ctx); err != nil {
		logger.Error("failed to start worker pool", slog.String("error", err.Error()))
		os.Exit(1)
	}

	srv := scheduler.NewServer(dispatcher, store, queue)

	grpcServer := grpc.NewServer(
		grpc.UnaryInterceptor(scheduler.TailscaleInterceptor()),
		grpc.StreamInterceptor(scheduler.TailscaleStreamInterceptor()),
	)
	pb.RegisterSchedulerServer(grpcServer, srv)

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

	// start metrics server
	go func() {
		metricsAddr := ":9090"
		logger.Info("metrics server listening", slog.String("addr", metricsAddr))
		if err := http.ListenAndServe(metricsAddr, scheduler.MetricsHandler()); err != nil {
			logger.Error("metrics server stopped", slog.String("error", err.Error()))
		}
	}()

	// graceful shutdown
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
