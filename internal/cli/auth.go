package cli

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/debaucheryparty/packets/internal/config"
	"github.com/spf13/cobra"
)

func NewAuthCommand(cfg *config.Config, logger *slog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage authentication and remote daemon connection profiles",
	}

	cmd.AddCommand(
		newAuthLoginCommand(cfg),
		newAuthSetTokenCommand(cfg),
		newAuthStatusCommand(cfg, logger),
		newAuthLogoutCommand(cfg),
	)

	return cmd
}

func newAuthLoginCommand(cfg *config.Config) *cobra.Command {
	var (
		serverFlag string
		tokenFlag  string
		tlsFlag    bool
	)

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Configure connection credentials and save to ~/.packets/config.yaml",
		RunE: func(cmd *cobra.Command, args []string) error {
			prof, err := config.LoadProfile()
			if err != nil {
				prof = &config.Profile{}
			}

			if serverFlag != "" {
				prof.ServerAddr = serverFlag
			}
			if tokenFlag != "" {
				prof.AuthToken = tokenFlag
			}
			if tlsFlag {
				prof.TLSEnabled = true
			}

			if prof.ServerAddr == "" {
				return fmt.Errorf("server address is required (use --server <host:port>)")
			}

			if err := config.SaveProfile(prof); err != nil {
				return fmt.Errorf("save profile: %w", err)
			}

			fmt.Println("✓ Credentials successfully saved to ~/.packets/config.yaml")
			fmt.Printf("  Server Address: %s\n", prof.ServerAddr)
			if prof.AuthToken != "" {
				masked := maskToken(prof.AuthToken)
				fmt.Printf("  Auth Token:     %s\n", masked)
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&serverFlag, "server", "s", "", "Remote Packets daemon address (e.g. 100.64.0.1:50051 or vps.domain.com:50051)")
	cmd.Flags().StringVarP(&tokenFlag, "token", "t", "", "Authentication token")
	cmd.Flags().BoolVar(&tlsFlag, "tls", false, "Enable TLS transport encryption")

	return cmd
}

func newAuthSetTokenCommand(_ *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "set-token <token>",
		Short: "Set the authentication token for remote daemon requests",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			token := strings.TrimSpace(args[0])
			if token == "" {
				return fmt.Errorf("token cannot be empty")
			}

			prof, err := config.LoadProfile()
			if err != nil {
				prof = &config.Profile{}
			}

			prof.AuthToken = token
			if err := config.SaveProfile(prof); err != nil {
				return fmt.Errorf("save profile: %w", err)
			}

			fmt.Printf("✓ Authentication token updated: %s\n", maskToken(token))
			return nil
		},
	}
}

func newAuthStatusCommand(cfg *config.Config, logger *slog.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Display active authentication and profile status",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			prof, err := config.LoadProfile()
			if err != nil {
				prof = &config.Profile{}
			}

			path, _ := config.GetProfilePath()
			fmt.Printf("Profile Config:   %s\n", path)

			server := cfg.OracleVMTailscaleHost
			if server == "" {
				server = prof.ServerAddr
			}
			if server == "" {
				server = "127.0.0.1:50051 (default local)"
			}
			fmt.Printf("Server Address:   %s\n", server)

			token := cfg.AuthToken
			if token == "" {
				token = prof.AuthToken
			}
			if token != "" {
				fmt.Printf("Auth Token:       %s (Configured)\n", maskToken(token))
			} else {
				fmt.Println("Auth Token:       None (Unauthenticated)")
			}

			tlsStatus := "Disabled"
			if cfg.TLSEnabled || prof.TLSEnabled {
				tlsStatus = "Enabled"
			}
			fmt.Printf("TLS Encryption:   %s\n\n", tlsStatus)

			fmt.Println("Testing connectivity to Packets daemon...")
			conn, err := DialScheduler(ctx, cfg)
			if err != nil {
				fmt.Printf("✗ Connection failed: %v\n", err)
				return nil
			}
			defer conn.Close()
			fmt.Println("✓ Successfully connected to Packets daemon!")
			return nil
		},
	}
}

func newAuthLogoutCommand(_ *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Clear stored authentication token",
		RunE: func(cmd *cobra.Command, args []string) error {
			prof, err := config.LoadProfile()
			if err != nil {
				prof = &config.Profile{}
			}

			prof.AuthToken = ""
			if err := config.SaveProfile(prof); err != nil {
				return fmt.Errorf("save profile: %w", err)
			}

			fmt.Println("✓ Auth token cleared from profile.")
			return nil
		},
	}
}

func maskToken(token string) string {
	if len(token) <= 8 {
		return "********"
	}
	return token[:4] + "..." + token[len(token)-4:]
}
