package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/lmittmann/tint"
	"github.com/spf13/cobra"
	"github.com/waris4ly/packets/internal/cli"
	"github.com/waris4ly/packets/internal/config"
)

var version = "dev"

func main() {
	ctx := context.Background()
	logger := slog.New(tint.NewHandler(os.Stderr, &tint.Options{
		Level:      slog.LevelInfo,
		TimeFormat: time.TimeOnly,
	}))

	cfg, err := config.LoadConfig(ctx)
	if err != nil {
		logger.Error("failed to load config", slog.String("error", err.Error()))
		os.Exit(1)
	}

	rootCmd := &cobra.Command{
		Use:     "packets",
		Short:   "Packets remote build execution system",
		Version: version,
	}

	rootCmd.AddCommand(
		cli.NewBuildCommand(cfg, logger),
		cli.NewStatusCommand(cfg, logger),
		cli.NewCacheCommand(cfg, logger),
		cli.NewProviderCommand(cfg, logger),
		cli.NewLogsCommand(cfg, logger),
		cli.NewArtifactCommand(cfg, logger),
	)

	if err := rootCmd.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
