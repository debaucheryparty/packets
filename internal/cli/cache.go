package cli

import (
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"
	"github.com/waris4ly/packets/internal/config"
	pb "github.com/waris4ly/packets/proto/v1"
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
			conn, err := DialScheduler(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			defer conn.Close()

			client := pb.NewSchedulerClient(conn)
			resp, err := client.ClearCache(cmd.Context(), &pb.ClearCacheRequest{Toolchain: "all"})
			if err != nil {
				return fmt.Errorf("ClearCache: %w", err)
			}

			fmt.Printf("Cleared %d cached entries\n", resp.ClearedCount)
			return nil
		},
	})

	return cmd
}
