package cli

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/debaucheryparty/packets/internal/config"
	"github.com/debaucheryparty/packets/internal/environment"
	pb "github.com/debaucheryparty/packets/proto/v1"
	"github.com/spf13/cobra"
)

func NewTUICommand(cfg *config.Config, logger *slog.Logger) *cobra.Command {
	var watch bool

	cmd := &cobra.Command{
		Use:   "tui",
		Short: "Interactive terminal dashboard for Packets",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			render := func() error {
				fmt.Print("\033[H\033[2J")
				fmt.Println("============================================================")
				fmt.Println("PACKETS STATUS DASHBOARD")
				fmt.Println("============================================================")

				conn, err := DialScheduler(ctx, cfg)
				if err != nil {
					fmt.Printf("Scheduler: DISCONNECTED (%v)\n\n", err)
				} else {
					defer func() { _ = conn.Close() }()
					client := pb.NewSchedulerClient(conn)
					statusCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
					defer cancel()

					_, err := client.GetJobStatus(statusCtx, &pb.GetJobStatusRequest{JobId: "ping"})
					if err != nil && !containsNotFound(err) {
						fmt.Printf("Scheduler: UNREACHABLE (%v)\n\n", err)
					} else {
						fmt.Println("Scheduler: ONLINE")
						fmt.Printf("Target:    %s\n\n", cfg.SchedulerAddr())
					}
				}

				fmt.Println("------------------------------------------------------------")
				fmt.Println("LOCAL ENVIRONMENT")
				fmt.Println("------------------------------------------------------------")
				mgr := environment.NewManager()
				topo, err := mgr.Detect(".")
				if err != nil {
					fmt.Printf("Detection: %v\n", err)
				} else {
					fmt.Printf("Project Root: %s\n", topo.RootPath)
					fmt.Printf("Components:   %d\n", len(topo.Components))
					for _, c := range topo.Components {
						fmt.Printf("  * %s (%s)\n", c.Name, c.Type)
					}
				}
				fmt.Println("============================================================")
				return nil
			}

			if !watch {
				return render()
			}

			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()

			if err := render(); err != nil {
				return err
			}

			for {
				select {
				case <-ctx.Done():
					return nil
				case <-ticker.C:
					if err := render(); err != nil {
						return err
					}
				}
			}
		},
	}

	cmd.Flags().BoolVarP(&watch, "watch", "w", false, "Watch and refresh status continuously")
	return cmd
}

func containsNotFound(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return len(s) > 0
}
