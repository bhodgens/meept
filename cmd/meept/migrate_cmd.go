package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/memory"
)

//nolint:unused // called via rootCmd.AddCommand()
func newMigrateCmd() *cobra.Command {
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Migrate local data stores to dual-DB layout",
		Long: `Migrate legacy single-DB layout (sessions.db, memory.db) to the
dual-store layout (local.db, sync-gossip.db).

A backup of all pre-migration files is created in migration-backup/ inside
the data directory before any files are modified.

With --dry-run, the command shows what would happen without making changes.

Examples:

  # Preview the migration
  meept migrate --dry-run

  # Perform the migration
  meept migrate
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadDefault()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			dataDir := cfg.Daemon.DataDir
			if dataDir == "" {
				home, _ := os.UserHomeDir()
				dataDir = filepath.Join(home, ".meept")
			}

			nodeID := cfg.Cluster.NodeID
			if nodeID == "" {
				nodeID = "local"
			}

			if dryRun {
				return runMigrateDryRun(dataDir)
			}

			fmt.Printf("Migrating data directory: %s\n", dataDir)

			if err := memory.MigrateToDualDB(dataDir, nodeID, nil); err != nil {
				return fmt.Errorf("migration failed: %w", err)
			}

			fmt.Println("Migration complete!")
			return nil
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false,
		"Preview migration without making changes")

	return cmd
}

//nolint:unused // called from newMigrateCmd
func runMigrateDryRun(dataDir string) error {
	sessionsPath := filepath.Join(dataDir, "sessions.db")
	memoryPath := filepath.Join(dataDir, "memory.db")

	fmt.Println("Migration preview (no changes will be made):")
	fmt.Println()

	if _, err := os.Stat(sessionsPath); err == nil {
		info, _ := os.Stat(sessionsPath)
		fmt.Printf("  [MIRROR] sessions.db (%s) -> local.db\n", humanSize(info.Size()))
	} else {
		fmt.Println("  [SKIP] sessions.db (not found)")
	}

	if _, err := os.Stat(memoryPath); err == nil {
		info, _ := os.Stat(memoryPath)
		fmt.Printf("  [MERGE] memory.db (%s) -> local.db tables\n", humanSize(info.Size()))
	} else {
		fmt.Println("  [SKIP] memory.db (not found)")
	}

	fmt.Println()
	fmt.Println("Target files:")
	fmt.Println("  local.db           – renamed sessions.db or memory.db")
	fmt.Println("  sync-gossip.db     – new gossip schema (empty)")
	fmt.Println("  migration-backup/  – backup of all pre-migration files")

	return nil
}

// humanSize formats bytes to a human-readable string.
//nolint:unused // called from runMigrateDryRun
func humanSize(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	}
}
