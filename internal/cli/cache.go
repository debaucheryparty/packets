package cli

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/spf13/cobra"
	"github.com/waris4ly/packets/internal/config"
	pb "github.com/waris4ly/packets/proto/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func NewCacheCommand(cfg *config.Config, logger *slog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Manage local and remote build caches",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "clear",
		Short: "Clear the build cache",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()

			addr := cfg.OracleVMTailscaleHost + cfg.SchedulerAddr()
			conn, err := grpc.DialContext(ctx, addr, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
			if err != nil {
				return fmt.Errorf("dial scheduler: %w", err)
			}
			defer conn.Close()

			client := pb.NewSchedulerClient(conn)
			resp, err := client.ClearCache(ctx, &pb.ClearCacheRequest{Toolchain: "all"})
			if err != nil {
				return fmt.Errorf("ClearCache: %w", err)
			}

			fmt.Printf("Cleared %d cached entries\n", resp.ClearedCount)
			return nil
		},
	})

	return cmd
}
