package main

// meept knowledge — operator visibility into the knowledge-asset store
// (tree 04). Thin and DIRECT: read-only SQL over the store's sqlite file
// without touching the daemon (the daemon holds the store open in WAL
// mode, so a concurrent read is safe).

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/caimlas/meept/internal/config"
	"github.com/spf13/cobra"
	_ "modernc.org/sqlite" //nolint:revive // blank import for the "sqlite" driver
)

func newKnowledgeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "knowledge",
		Short: "knowledge-asset store visibility",
		Long: `Inspect the knowledge-asset store directly (no daemon required).

Examples:
  meept knowledge stats    # page count, code-page count, call-edge count`,
	}
	cmd.AddCommand(knowledgeStatsCmd())
	return cmd
}

// knowledgeStatsCmd prints store counts in lowercase key: value lines:
// total pages, code pages (source LIKE 'code:%'), and stored call edges.
// Read-only direct SQL — no daemon connection, no store writes.
func knowledgeStatsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stats",
		Short: "print knowledge store counts",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadDefault()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			if !cfg.Memory.Knowledge.Enabled {
				fmt.Println("knowledge store disabled (memory.knowledge.enabled)")
				return nil
			}
			dbPath := filepath.Join(cfg.Daemon.DataDir, "knowledge", "knowledge.db")
			db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
			if err != nil {
				return fmt.Errorf("open knowledge database: %w", err)
			}
			defer db.Close() //nolint:errcheck // read-only stats peek

			scanCount := func(query string, args ...any) (int, error) {
				var n int
				if err := db.QueryRow(query, args...).Scan(&n); err != nil {
					return 0, err
				}
				return n, nil
			}

			pages, err := scanCount(`SELECT COUNT(*) FROM knowledge_pages`)
			if err != nil {
				return fmt.Errorf("count pages: %w", err)
			}
			// The tables this report reads exist before their feature
			// ever runs (code_edges is created lazily by the first
			// edge write/read), so a missing table is a zero, not an
			// error.
			codePages, codeErr := scanCount(`SELECT COUNT(*) FROM knowledge_pages WHERE source LIKE 'code:%'`)
			if codeErr != nil && !isNoSuchTable(codeErr) {
				return fmt.Errorf("count code pages: %w", codeErr)
			}
			edges, edgeErr := scanCount(`SELECT COUNT(*) FROM code_edges`)
			if edgeErr != nil && !isNoSuchTable(edgeErr) {
				return fmt.Errorf("count call edges: %w", edgeErr)
			}

			fmt.Printf("pages: %d\n", pages)
			fmt.Printf("code pages: %d\n", codePages)
			fmt.Printf("call edges: %d\n", edges)
			return nil
		},
	}
}

// isNoSuchTable reports whether err is sqlite's "no such table" — the
// lazy-table case a zero-count answer covers honestly.
func isNoSuchTable(err error) bool {
	if err == nil {
		return false
	}
	// Contains, not prefix: the caller wraps the scan error
	// ("count call edges: ..."), so the sentinel sits mid-string.
	return strings.Contains(err.Error(), "no such table:")
}
