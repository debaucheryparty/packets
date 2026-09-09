package cli

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/debaucheryparty/packets/internal/config"
	"github.com/debaucheryparty/packets/internal/project"
	"github.com/spf13/cobra"
)

func NewPullCommand(cfg *config.Config, logger *slog.Logger) *cobra.Command {
	var (
		jobIDFlag     string
		outputFlag    string
		artifactsOnly bool
	)

	cmd := &cobra.Command{
		Use:   "pull [dir]",
		Short: "Pull remote build outputs and artifacts back into the local workspace",
		Long: `Downloads and extracts remote build artifacts (e.g. APKs, firmware binaries,
generated code stubs, or build outputs) directly into your local project directory.`,
		Example: `  packets pull .
  packets pull --job job-12345
  packets pull --output ./dist`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			dir := "."
			if len(args) > 0 {
				dir = args[0]
			}

			absDir, err := filepath.Abs(dir)
			if err != nil {
				return fmt.Errorf("resolve directory: %w", err)
			}

			destDir := absDir
			if outputFlag != "" {
				destDir, err = filepath.Abs(outputFlag)
				if err != nil {
					return fmt.Errorf("resolve output directory: %w", err)
				}
			}

			if jobIDFlag != "" {
				fmt.Printf("Pulling artifacts for job %s into %s...\n", jobIDFlag, destDir)
				if err := PullAndExtractArtifact(ctx, cfg, logger, jobIDFlag, destDir); err != nil {
					return fmt.Errorf("pull artifact: %w", err)
				}
				fmt.Println("✓ Artifacts downloaded and extracted successfully.")
				return nil
			}

			projectID := project.ResolveProjectID(absDir)
			fmt.Printf("Pulling latest build artifacts for project %s into %s...\n", projectID, destDir)

			if err := pullLatestProjectArtifacts(ctx, cfg, logger, projectID, destDir); err != nil {
				return err
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&jobIDFlag, "job", "j", "", "Specific job ID to pull artifacts from")
	cmd.Flags().StringVarP(&outputFlag, "output", "o", "", "Destination directory (default: project root)")
	cmd.Flags().BoolVar(&artifactsOnly, "artifacts-only", true, "Pull only generated build artifacts")

	return cmd
}

func pullLatestProjectArtifacts(ctx context.Context, cfg *config.Config, logger *slog.Logger, projectID, destDir string) error {
	conn, err := DialScheduler(ctx, cfg)
	if err != nil {
		return fmt.Errorf("connect to scheduler: %w", err)
	}
	defer conn.Close()

	if err := PullAndExtractArtifact(ctx, cfg, logger, projectID, destDir); err != nil {

		return fmt.Errorf("no specific job specified; please pass --job <job-id> (e.g. packets pull --job <id>) or run after a build: %w", err)
	}
	return nil
}
