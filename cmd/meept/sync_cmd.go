package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/caimlas/meept/internal/backup"
	"github.com/caimlas/meept/internal/config"
	_ "modernc.org/sqlite"
)

func newSyncCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Manage peer sync for backup synchronization",
		Long:  "Trigger immediate peer sync or show sync status across devices.",
	}

	cmd.AddCommand(newSyncPullCmd())
	cmd.AddCommand(newSyncStatusCmd())

	return cmd
}

func newSyncPullCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pull",
		Short: "trigger immediate peer sync",
		Long:  "Fetch latest backups from the git repository and merge data from all configured peers.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadDefault()
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			syncCfg := cfg.PeerSync
			if !syncCfg.Enabled {
				fmt.Println("sync is not enabled. set peer_sync.enabled to true in your config.")
				return nil
			}

			dataDir := cfg.Daemon.DataDir
			if dataDir == "" {
				home, _ := os.UserHomeDir()
				dataDir = filepath.Join(home, ".meept")
			}

			dbPath := filepath.Join(dataDir, "local.db")
			db, err := sql.Open("sqlite", dbPath)
			if err != nil {
				return fmt.Errorf("failed to open database: %w", err)
			}
			defer db.Close()

			puller, err := backup.NewSyncPuller(syncCfg, db, db)
			if err != nil {
				return fmt.Errorf("failed to create sync puller: %w", err)
			}
			defer puller.Stop()

			fmt.Println("starting sync pull...")
			if err := puller.PullNow(); err != nil {
				return fmt.Errorf("sync failed: %w", err)
			}
			fmt.Println("sync pull completed successfully.")
			return nil
		},
	}

	_ = cmd.Flags().String("debug", "", "output debug logs to a file")

	return cmd
}

func newSyncStatusCmd() *cobra.Command {
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "show sync status",
		Long:  "display the current sync status for this node and all configured peers.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadDefault()
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			syncCfg := cfg.PeerSync
			hostname, _ := os.Hostname()

			dataDir := cfg.Daemon.DataDir
			if dataDir == "" {
				home, _ := os.UserHomeDir()
				dataDir = filepath.Join(home, ".meept")
			}

			dbPath := filepath.Join(dataDir, "local.db")
			db, dbErr := sql.Open("sqlite", dbPath)
			if dbErr != nil {
				// Best effort
				fmt.Printf("warning: could not open local db: %v\n\n", dbErr)
				db = nil
			}
			if db != nil {
				defer db.Close()
			}

			if outputJSON {
				status := map[string]interface{}{
					"node_id":      hostname,
					"sync_enabled": syncCfg.Enabled,
					"peers":        syncCfg.Peers,
				}
				if db != nil {
					store := backup.NewSyncMetadataStore(db)
					if err := store.EnsureTable(); err != nil {
						status["status_error"] = fmt.Sprintf("ensure sync_metadata table: %v", err)
					} else {
						peerStatus, serr := store.GetAllSyncStatus()
						if serr != nil {
							status["status_error"] = serr.Error()
						} else {
							status["peers"] = peerStatus
						}
					}
				}
				data, _ := json.MarshalIndent(status, "", "  ")
				fmt.Println(string(data))
				return nil
			}

			fmt.Println("sync status")
			fmt.Println("=========================================")
			fmt.Printf("this node: %s\n", hostname)
			fmt.Printf("sync enabled: %v\n", syncCfg.Enabled)
			if syncCfg.Enabled {
				fmt.Printf("known peers: %v\n", syncCfg.Peers)
				fmt.Printf("pull schedule: %s\n", syncCfg.PullSchedule)

				if db != nil {
					store := backup.NewSyncMetadataStore(db)
					if err := store.EnsureTable(); err != nil {
						fmt.Printf("\nerror initializing sync metadata: %v\n", err)
						return nil
					}
					peerStatus, serr := store.GetAllSyncStatus()
					if serr != nil {
						fmt.Printf("\nerror reading sync status: %v\n", serr)
						return nil
					}

					if len(peerStatus) == 0 {
						fmt.Println("\nno sync history found. run 'meept sync pull' to sync.")
						return nil
					}

					fmt.Println("\npeer synchronization:")
					for peerID, st := range peerStatus {
						fmt.Printf("  %s:\n", peerID)

						if !st.LastSync.IsZero() {
							fmt.Printf("    last sync: %s ago\n", formatDuration(time.Since(st.LastSync)))
						} else {
							fmt.Printf("    last sync: never\n")
						}

						if st.LastMergeStats != nil {
							s := st.LastMergeStats
							fmt.Printf("    rows received: %d sessions, %d turns, %d memories\n",
								s.SessionsMerged, s.TurnsMerged, s.MemoriesMerged)
						}

						if st.Error != "" {
							fmt.Printf("    errors: %s\n", st.Error)
						} else {
							fmt.Printf("    errors: none\n")
						}
						fmt.Println()
					}
				}
			} else {
				fmt.Println("enable sync in your config: set peer_sync.enabled to true")
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&outputJSON, "json", false, "output as json")

	return cmd
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.0fs", d.Seconds())
	}
	if d < time.Hour {
		return fmt.Sprintf("%.0fm", d.Minutes())
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%.0fh", d.Hours())
	}
	return fmt.Sprintf("%.0fd", d.Hours()/24)
}
