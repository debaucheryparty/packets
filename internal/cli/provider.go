package cli

import (
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"
	"github.com/waris4ly/packets/internal/config"
)

func NewProviderCommand(cfg *config.Config, logger *slog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "provider",
		Short: "Manage build providers",
	}

	addCmd := &cobra.Command{
		Use:   "add <type> <name>",
		Short: "Add a new provider (types: vps, github, local)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			providerType := args[0]
			name := args[1]

			host, _ := cmd.Flags().GetString("host")
			port, _ := cmd.Flags().GetInt("port")
			repo, _ := cmd.Flags().GetString("repo")
			sourceRepo, _ := cmd.Flags().GetString("source-repo")
			token, _ := cmd.Flags().GetString("token")
			tailscale, _ := cmd.Flags().GetBool("tailscale")

			globalCfg, err := config.LoadGlobalProviders()
			if err != nil {
				return err
			}

			p := config.ProviderConfig{}
			switch providerType {
			case "vps", "ssh-docker":
				p.Type = "ssh-docker"
				p.Host = host
				p.Port = port
				p.Tailscale = tailscale
			case "github", "github-actions":
				p.Type = "github-actions"
				p.Repository = repo
				p.SourceRepository = sourceRepo
				p.GitHubToken = token
			case "local":
				p.Type = "local"
			default:
				return fmt.Errorf("unsupported provider type: %s", providerType)
			}

			globalCfg.Providers[name] = p
			if err := config.SaveGlobalProviders(globalCfg); err != nil {
				return err
			}

			fmt.Printf("Provider %q added successfully\n", name)
			return nil
		},
	}
	addCmd.Flags().String("host", "", "Host address")
	addCmd.Flags().Int("port", 50051, "Port number")
	addCmd.Flags().String("repo", "", "Runner repository (for github)")
	addCmd.Flags().String("source-repo", "", "Source repository (for github)")
	addCmd.Flags().String("token", "", "GitHub token")
	addCmd.Flags().Bool("tailscale", false, "Use Tailscale")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List configured providers",
		RunE: func(cmd *cobra.Command, args []string) error {
			globalCfg, err := config.LoadGlobalProviders()
			if err != nil {
				return err
			}

			if len(globalCfg.Providers) == 0 {
				fmt.Println("No providers configured.")
				return nil
			}

			for name, p := range globalCfg.Providers {
				fmt.Printf("%s:\n", name)
				fmt.Printf("  Type: %s\n", p.Type)
				if p.Host != "" {
					fmt.Printf("  Host: %s:%d\n", p.Host, p.Port)
				}
				if p.Repository != "" {
					fmt.Printf("  Repository: %s\n", p.Repository)
				}
			}
			return nil
		},
	}

	removeCmd := &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a provider",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			globalCfg, err := config.LoadGlobalProviders()
			if err != nil {
				return err
			}

			if _, exists := globalCfg.Providers[name]; !exists {
				return fmt.Errorf("provider %q not found", name)
			}

			delete(globalCfg.Providers, name)
			if err := config.SaveGlobalProviders(globalCfg); err != nil {
				return err
			}

			fmt.Printf("Provider %q removed\n", name)
			return nil
		},
	}

	showCmd := &cobra.Command{
		Use:   "show <name>",
		Short: "Show details for a provider",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			globalCfg, err := config.LoadGlobalProviders()
			if err != nil {
				return err
			}

			p, exists := globalCfg.Providers[name]
			if !exists {
				return fmt.Errorf("provider %q not found", name)
			}

			fmt.Printf("Name: %s\n", name)
			fmt.Printf("Type: %s\n", p.Type)
			if p.Host != "" {
				fmt.Printf("Host: %s:%d\n", p.Host, p.Port)
			}
			if p.Repository != "" {
				fmt.Printf("Repository: %s\n", p.Repository)
			}
			if p.SourceRepository != "" {
				fmt.Printf("Source Repository: %s\n", p.SourceRepository)
			}
			return nil
		},
	}

	cmd.AddCommand(addCmd, listCmd, removeCmd, showCmd)
	return cmd
}
