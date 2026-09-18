package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/debaucheryparty/packets/internal/config"
	"github.com/debaucheryparty/packets/internal/environment"
	"github.com/debaucheryparty/packets/internal/policy"
	"github.com/debaucheryparty/packets/internal/provider"
	"github.com/debaucheryparty/packets/internal/scheduler"
	"github.com/debaucheryparty/packets/internal/storage"
	"github.com/debaucheryparty/packets/internal/toolchain"
	"github.com/debaucheryparty/packets/internal/worker"
	"github.com/debaucheryparty/packets/internal/workspace"
	"github.com/debaucheryparty/packets/pkg/apitypes"
	pb "github.com/debaucheryparty/packets/proto/v1"
	"github.com/lmittmann/tint"
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

	logger := slog.New(tint.NewTextHandler(os.Stderr, &tint.Options{
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
	defer func() { _ = store.Close() }()

	var objectStore storage.ObjectStore
	var diskStore *storage.DiskStore
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
	} else {
		storageDir := filepath.Join(cfg.WorkspaceTempDir, "packets-storage")
		ds, err := storage.NewDiskObjectStore(storageDir, "http://127.0.0.1:9090")
		if err != nil {
			logger.Error("failed to init local disk store", slog.String("error", err.Error()))
			os.Exit(1)
		}
		objectStore = ds
		diskStore = ds
		logger.Info("initialized local disk object store", slog.String("dir", storageDir))
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
	workerPool := scheduler.NewWorkerPool(logger, dispatcher, 20)
	dispatcher.SetWorkerPool(workerPool)

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		reapTicker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		defer reapTicker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				stale := workerPool.CleanupStaleWorkers(45 * time.Second)
				if len(stale) > 0 {
					logger.Info("cleaned up stale peer workers", slog.Int("count", len(stale)))
				}
			case <-reapTicker.C:
				if reaped, err := dispatcher.ReapStaleJobs(ctx, 1*time.Hour); err == nil && reaped > 0 {
					logger.Info("reaped timed out jobs", slog.Int("count", reaped))
				}
			}
		}
	}()

	if err := dispatcher.RecoverPendingJobs(ctx); err != nil {
		logger.Warn("job recovery failed", slog.String("error", err.Error()))
	}

	friendMode := os.Getenv("PACKETS_ROLE") == "friend"
	autoApprove := os.Getenv("PACKETS_AUTO_APPROVE") == "true" || os.Getenv("PACKETS_AUTO_APPROVE") == "1"
	for _, arg := range os.Args[1:] {
		if arg == "--friend" {
			friendMode = true
		}
		if arg == "--auto-approve" || arg == "--no-approval" || arg == "--trust-all" {
			autoApprove = true
		}
	}

	approvalMode := policy.ApprovalMode(os.Getenv("PACKETS_APPROVAL_MODE"))
	if approvalMode == "" {
		if autoApprove {
			approvalMode = policy.ApprovalNever
		} else if friendMode {
			approvalMode = policy.ApprovalAlways
		} else {
			approvalMode = policy.ApprovalNever
		}
	}

	policyEngine := policy.NewPolicyEngineWithStore(approvalMode, store)
	if friendMode && !autoApprove {
		policyEngine.SetApprover(policy.NewConsoleApprover())
		logger.Info("running in friend peer mode", slog.String("approval", "interactive"))
	} else if autoApprove {
		logger.Info("running with auto-approval enabled", slog.String("approval", "disabled"))
	} else {
		logger.Info("running in vps node mode", slog.String("approval", "auto"))
	}
	srv := scheduler.NewServer(dispatcher, store, logBroker, objectStore, providers)
	srv.SetPolicyEngine(policyEngine)

	var serverOpts []grpc.ServerOption
	serverOpts = append(serverOpts,
		grpc.UnaryInterceptor(scheduler.AuthInterceptor(cfg.AuthToken)),
		grpc.StreamInterceptor(scheduler.AuthStreamInterceptor(cfg.AuthToken)),
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
	pb.RegisterEnvironmentServer(grpcServer, environment.NewServer(nil))

	wsSrv := workspace.NewServer(objectStore, registry)
	pb.RegisterWorkspaceServer(grpcServer, wsSrv)

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
		mux := http.NewServeMux()
		mux.Handle("/metrics", scheduler.MetricsHandler())
		if diskStore != nil {
			mux.HandleFunc("/storage/", diskStore.HTTPHandler())
		}
		logger.Info("http server listening", slog.String("addr", metricsAddr))
		if err := http.ListenAndServe(metricsAddr, mux); err != nil {
			logger.Error("http server stopped", slog.String("error", err.Error()))
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
