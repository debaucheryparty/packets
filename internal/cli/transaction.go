package cli

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"text/tabwriter"

	"github.com/debaucheryparty/packets/internal/config"
	"github.com/debaucheryparty/packets/internal/project"
	"github.com/debaucheryparty/packets/internal/workspace"
	pb "github.com/debaucheryparty/packets/proto/v1"
	"github.com/spf13/cobra"
)

func NewTransactionCommand(cfg *config.Config, logger *slog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "transaction",
		Short: "Manage safe workspace modification transactions",
		Long:  "Create, monitor, commit, or rollback workspace modification transactions for AI agents or safe builds.",
	}

	cmd.AddCommand(
		newTransactionCreateCommand(cfg, logger),
		newTransactionCommitCommand(cfg, logger),
		newTransactionRollbackCommand(cfg, logger),
		newTransactionStatusCommand(cfg, logger),
		newTransactionListCommand(cfg, logger),
	)

	return cmd
}

func newTransactionCreateCommand(cfg *config.Config, _ *slog.Logger) *cobra.Command {
	var (
		projectFlag  string
		subspaceFlag string
		descFlag     string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Open a new workspace transaction snapshotting current state",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			pwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getwd: %w", err)
			}

			projectID := projectFlag
			if projectID == "" {
				projectID = project.ResolveProjectID(pwd)
			}

			conn, err := DialScheduler(ctx, cfg)
			if err != nil {
				return fmt.Errorf("connect to packetsd: %w", err)
			}
			defer func() { _ = conn.Close() }()

			snapshotRef, err := workspace.UploadWorkspace(ctx, conn, pwd, false)
			if err != nil {
				return fmt.Errorf("workspace snapshot: %w", err)
			}

			client := pb.NewTransactionServiceClient(conn)
			resp, err := client.CreateTransaction(ctx, &pb.CreateTransactionRequest{
				ProjectId:       projectID,
				SubspaceId:      subspaceFlag,
				BaseSnapshotRef: snapshotRef,
				Description:     descFlag,
			})
			if err != nil {
				return fmt.Errorf("create transaction: %w", err)
			}

			tx := resp.Transaction
			fmt.Println("Transaction opened successfully")
			fmt.Printf("  ID:            %s\n", tx.Id)
			fmt.Printf("  Project:       %s\n", tx.ProjectId)
			if tx.SubspaceId != "" {
				fmt.Printf("  Subspace:      %s\n", tx.SubspaceId)
			}
			fmt.Printf("  Base Snapshot: %s\n", tx.BaseSnapshotRef)
			fmt.Printf("  Status:        %s\n", tx.Status)
			return nil
		},
	}

	cmd.Flags().StringVar(&projectFlag, "project", "", "Project ID (defaults to current directory)")
	cmd.Flags().StringVar(&subspaceFlag, "subspace", "", "Optional target Subspace ID")
	cmd.Flags().StringVar(&descFlag, "desc", "", "Optional transaction description")

	return cmd
}

func newTransactionCommitCommand(cfg *config.Config, _ *slog.Logger) *cobra.Command {
	var snapshotFlag string

	cmd := &cobra.Command{
		Use:   "commit <id>",
		Short: "Commit an open workspace transaction",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			id := args[0]

			conn, err := DialScheduler(ctx, cfg)
			if err != nil {
				return fmt.Errorf("connect to packetsd: %w", err)
			}
			defer func() { _ = conn.Close() }()

			snapshotRef := snapshotFlag
			if snapshotRef == "" {
				pwd, getErr := os.Getwd()
				if getErr == nil {
					if snap, upErr := workspace.UploadWorkspace(ctx, conn, pwd, false); upErr == nil {
						snapshotRef = snap
					}
				}
			}

			client := pb.NewTransactionServiceClient(conn)
			resp, err := client.CommitTransaction(ctx, &pb.CommitTransactionRequest{
				Id:                 id,
				WorkingSnapshotRef: snapshotRef,
			})
			if err != nil {
				return fmt.Errorf("commit transaction %s: %w", id, err)
			}

			fmt.Printf("Transaction %s committed (snapshot: %s)\n", resp.Transaction.Id, resp.Transaction.WorkingSnapshotRef)
			return nil
		},
	}

	cmd.Flags().StringVar(&snapshotFlag, "snapshot", "", "Explicit working snapshot ref to commit")

	return cmd
}

func newTransactionRollbackCommand(cfg *config.Config, _ *slog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rollback <id>",
		Short: "Rollback an open workspace transaction to its base snapshot",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			id := args[0]

			conn, err := DialScheduler(ctx, cfg)
			if err != nil {
				return fmt.Errorf("connect to packetsd: %w", err)
			}
			defer func() { _ = conn.Close() }()

			client := pb.NewTransactionServiceClient(conn)
			resp, err := client.RollbackTransaction(ctx, &pb.RollbackTransactionRequest{
				Id: id,
			})
			if err != nil {
				return fmt.Errorf("rollback transaction %s: %w", id, err)
			}

			fmt.Printf("Transaction %s rolled back to %s\n", resp.Transaction.Id, resp.Transaction.BaseSnapshotRef)
			return nil
		},
	}

	return cmd
}

func newTransactionStatusCommand(cfg *config.Config, _ *slog.Logger) *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "status <id>",
		Short: "Inspect the details and status of a transaction",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			id := args[0]

			conn, err := DialScheduler(ctx, cfg)
			if err != nil {
				return fmt.Errorf("connect to packetsd: %w", err)
			}
			defer func() { _ = conn.Close() }()

			client := pb.NewTransactionServiceClient(conn)
			resp, err := client.GetTransaction(ctx, &pb.GetTransactionRequest{Id: id})
			if err != nil {
				return fmt.Errorf("get transaction %s: %w", id, err)
			}

			tx := resp.Transaction
			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(tx)
			}

			fmt.Printf("ID:                 %s\n", tx.Id)
			fmt.Printf("Project:            %s\n", tx.ProjectId)
			if tx.SubspaceId != "" {
				fmt.Printf("Subspace:           %s\n", tx.SubspaceId)
			}
			fmt.Printf("Base Snapshot:      %s\n", tx.BaseSnapshotRef)
			fmt.Printf("Working Snapshot:   %s\n", tx.WorkingSnapshotRef)
			fmt.Printf("Status:             %s\n", tx.Status)
			if tx.Description != "" {
				fmt.Printf("Description:        %s\n", tx.Description)
			}
			fmt.Printf("Created:            %s\n", tx.CreatedAt)
			fmt.Printf("Updated:            %s\n", tx.UpdatedAt)
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")

	return cmd
}

func newTransactionListCommand(cfg *config.Config, _ *slog.Logger) *cobra.Command {
	var (
		projectFlag string
		jsonOutput  bool
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List transactions for a project",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			projectID := projectFlag
			if projectID == "" {
				if pwd, err := os.Getwd(); err == nil {
					projectID = project.ResolveProjectID(pwd)
				}
			}

			conn, err := DialScheduler(ctx, cfg)
			if err != nil {
				return fmt.Errorf("connect to packetsd: %w", err)
			}
			defer func() { _ = conn.Close() }()

			client := pb.NewTransactionServiceClient(conn)
			resp, err := client.ListTransactions(ctx, &pb.ListTransactionsRequest{ProjectId: projectID})
			if err != nil {
				return fmt.Errorf("list transactions: %w", err)
			}

			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(resp.Transactions)
			}

			if len(resp.Transactions) == 0 {
				fmt.Println("No transactions found.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
			_, _ = fmt.Fprintln(w, "ID\tPROJECT\tSTATUS\tBASE_SNAPSHOT\tCREATED")
			for _, tx := range resp.Transactions {
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
					tx.Id, tx.ProjectId, tx.Status, tx.BaseSnapshotRef, tx.CreatedAt)
			}
			return w.Flush()
		},
	}

	cmd.Flags().StringVar(&projectFlag, "project", "", "Filter by project ID")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")

	return cmd
}
