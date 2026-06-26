package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/caimlas/meept/internal/config"
)

func newBackupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Manage database backups",
		Long:  "Manage git-backed backups of local SQLite databases.",
	}

	cmd.AddCommand(newBackupListCmd())
	cmd.AddCommand(newBackupPushCmd())

	return cmd
}

func newBackupPushCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "push",
		Short: "Trigger an immediate backup push",
		Long:  "Trigger an immediate backup to the configured git repository.",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			payload := map[string]interface{}{"force": force}
			result, err := client.Call("backup.push", payload)
			if err != nil {
				return fmt.Errorf("backup push failed: %w", err)
			}

			var resultMap map[string]interface{}
			if err := json.Unmarshal(result, &resultMap); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if errMsg, ok := resultMap["error"].(string); ok && errMsg != "" {
				return fmt.Errorf("backup push error: %s", errMsg)
			}

			if outputFile, ok := resultMap["output"].(string); ok && outputFile != "" {
				fmt.Println(outputFile)
			}

			fmt.Println("Backup push completed successfully.")
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Force push even if no changes")

	return cmd
}

func newBackupListCmd() *cobra.Command {
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List available backups",
		Long:  "List available backups for this node.",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			result, err := client.Call("backup.list", nil)
			if err != nil {
				// Fallback: try local listing from data dir
				return runLocalBackupList()
			}

			var resultMap map[string]interface{}
			if err := json.Unmarshal(result, &resultMap); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if errMsg, ok := resultMap["error"].(string); ok && errMsg != "" {
				return fmt.Errorf("backup list error: %s", errMsg)
			}

			if outputJSON {
				out, err := json.MarshalIndent(resultMap, "", "  ")
				if err != nil {
					return fmt.Errorf("failed to marshal JSON: %w", err)
				}
				fmt.Println(string(out))
				return nil
			}

			backups, ok := resultMap["backups"].([]interface{})
			if !ok || len(backups) == 0 {
				fmt.Println("No backups found.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "DATE\tDATABASE\tCOMPRESSED\tUNCOMPRESSED\tSHA256")

			for _, bk := range backups {
				bm, ok := bk.(map[string]interface{})
				if !ok {
					continue
				}
				fmt.Fprintf(w, "%v\t%.1f MB\t%.1f MB\t%.8s\n",
					bm["date"],
					float64(toInt64(bm["compressed"]))/(1024*1024),
					float64(toInt64(bm["uncompressed"]))/(1024*1024),
					fmt.Sprintf("%v", bm["sha256"]),
				)
			}
			w.Flush()
			return nil
		},
	}

	cmd.Flags().BoolVar(&outputJSON, "json", false, "Output as JSON")

	return cmd
}

// runLocalBackupList lists backups from the local data directory.
func runLocalBackupList() error {
	cfg, err := config.LoadDefault()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	dataDir := cfg.Daemon.DataDir
	if dataDir == "" {
		home, _ := os.UserHomeDir()
		dataDir = filepath.Join(home, ".meept")
	}

	backupsDir := filepath.Join(dataDir, "backups")
	if _, err := os.Stat(backupsDir); os.IsNotExist(err) {
		fmt.Println("No backups found (no backups directory).")
		return nil
	}

	dirs, err := os.ReadDir(backupsDir)
	if err != nil {
		return fmt.Errorf("failed to read backups directory: %w", err)
	}

	type backupEntry struct {
		date string
		info map[string]interface{}
	}

	var entries []backupEntry
	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		manifestPath := filepath.Join(backupsDir, dir.Name(), "manifest.json")
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			continue
		}

		var manifest map[string]interface{}
		if err := json.Unmarshal(data, &manifest); err != nil {
			continue
		}

		databases, _ := manifest["databases"].([]interface{})

		for _, db := range databases {
			dbm, ok := db.(map[string]interface{})
			if !ok {
				continue
			}
			entries = append(entries, backupEntry{
				date: dir.Name(),
				info: dbm,
			})
		}
	}

	if len(entries) == 0 {
		fmt.Println("No backups found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "DATE\tNODE\tDATABASE\tCOMPRESSED\tUNCOMPRESSED\tSHA256")

	for _, e := range entries {
		fmt.Fprintf(w, "%s\t%s\t%s\t%.1f MB\t%.1f MB\t%.8s\n",
			e.date,
			"local",
			e.info["name"],
			float64(toInt64(e.info["compressed_size"]))/(1024*1024),
			float64(toInt64(e.info["uncompressed_size"]))/(1024*1024),
			fmt.Sprintf("%v", e.info["sha256"]),
		)
	}
	w.Flush()

	return nil
}

func toInt64(v interface{}) int64 {
	switch val := v.(type) {
	case float64:
		return int64(val)
	case int:
		return int64(val)
	case int64:
		return val
	default:
		return 0
	}
}
