package main

import (
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/caimlas/meept/internal/config"
)

func newConfigSyncCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Configuration synchronization",
		Long:  "Manage configuration synchronization via git.",
	}

	cmd.AddCommand(newConfigSyncStatusCmd())
	cmd.AddCommand(newConfigSyncPullCmd())

	return cmd
}

func newConfigSyncStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show config sync status",
		Long:  "Show current configuration sync status including repo URL, last pull, and applied files.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadDefault()
			if err != nil {
				// Try JSON5
				homeDir, _ := os.UserHomeDir()
				json5Path := filepath.Join(homeDir, ".meept", "meept.json5")
				if _, statErr := os.Stat(json5Path); statErr == nil {
					if json5Cfg, loadErr := config.LoadJSON5Config(json5Path); loadErr == nil {
						cfg = json5Cfg
					}
				}
			}

			syncCfg := cfg.ConfigSync
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)

			fmt.Fprintln(w, "Key\tValue")
			fmt.Fprintln(w, "---\t---")
			fmt.Fprintf(w, "Enabled\t%v\n", syncCfg.Enabled)
			fmt.Fprintf(w, "Repo\t%s\n", syncCfg.RepoURL)
			fmt.Fprintf(w, "Node\t%s\n", cfg.Backup.NodeID) // NodeID lives on Backup config for now
			fmt.Fprintf(w, "Pull rate\t%s\n", syncCfg.PullSchedule.String())
			fmt.Fprintf(w, "Checkout\t%s/.config-sync\n", cfg.Daemon.DataDir)

			// Try RPC sync status
			client, rpcErr := connectDaemon()
			if rpcErr == nil {
				defer client.Close()
				result, callErr := client.Call("config_sync.status", nil)
				if callErr == nil && result != nil && len(result) > 0 && string(result) != "null" {
					fmt.Fprintf(w, "\nLast commit\t%s\n", string(result))
				}
			}

			w.Flush()
			return nil
		},
	}
}

func newConfigSyncPullCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pull",
		Short: "Force config refresh",
		Long:  "Force an immediate pull and merge of configurations from the git repo.",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			result, err := client.Call("config_sync.pull", nil)
			if err != nil {
				return fmt.Errorf("config sync pull failed: %w", err)
			}

			fmt.Fprintln(os.Stdout, string(result))
			return nil
		},
	}
}
