package cli

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"text/tabwriter"
	"time"

	"github.com/debaucheryparty/packets/internal/config"
	pb "github.com/debaucheryparty/packets/proto/v1"
	"github.com/spf13/cobra"
)

func NewJobsCommand(cfg *config.Config, _ *slog.Logger) *cobra.Command {
	var limit int
	var stateFilter string
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "jobs",
		Short: "List recent and active build jobs",
		Example: `  packets jobs
  packets jobs --limit 10
  packets jobs --state RUNNING
  packets jobs --json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			conn, err := DialScheduler(cmd.Context(), cfg)
			if err != nil {
				return fmt.Errorf("packetsd unreachable: %w", err)
			}
			defer func() { _ = conn.Close() }()

			client := pb.NewSchedulerClient(conn)
			resp, err := client.ListJobs(cmd.Context(), &pb.ListJobsRequest{
				Limit: int32(limit),
				State: stateFilter,
			})
			if err != nil {
				return fmt.Errorf("list jobs: %w", err)
			}

			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(resp.Jobs)
			}

			if len(resp.Jobs) == 0 {
				fmt.Println("packets :: no jobs found")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
			_, _ = fmt.Fprintln(w, "JOB ID\tTOOLCHAIN\tRUNNER\tSTATE\tAGE\tOWNER")

			now := time.Now().Unix()
			for _, j := range resp.Jobs {
				stateStr := j.State.String()
				if len(stateStr) > 10 && stateStr[:10] == "JOB_STATE_" {
					stateStr = stateStr[10:]
				}

				ageStr := "-"
				if j.SubmittedAtUnix > 0 {
					d := time.Duration(now-j.SubmittedAtUnix) * time.Second
					if d < time.Minute {
						ageStr = fmt.Sprintf("%ds ago", int(d.Seconds()))
					} else if d < time.Hour {
						ageStr = fmt.Sprintf("%dm ago", int(d.Minutes()))
					} else {
						ageStr = fmt.Sprintf("%dh ago", int(d.Hours()))
					}
				}

				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
					j.JobId, j.Toolchain, j.Runner, stateStr, ageStr, j.Owner)
			}
			return w.Flush()
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 20, "maximum number of jobs to return")
	cmd.Flags().StringVar(&stateFilter, "state", "", "filter by job state (PENDING, RUNNING, SUCCEEDED, FAILED)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output in JSON format")

	return cmd
}
