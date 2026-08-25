package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/caimlas/meept/internal/tools/builtin"
	"github.com/spf13/cobra"
)

// changesJSONEntry is the JSON projection of a journal entry for
// `meept changes list --json` / `meept changes revert <id> --json`.
type changesJSONEntry struct {
	ID         string   `json:"id"`
	SessionID  string   `json:"session_id,omitempty"`
	File       string   `json:"file"`
	AppliedAt  string   `json:"applied_at"`
	SizeBytes  int64    `json:"size_bytes"`
	Revertable bool     `json:"revertable"`
	ChangeIDs  []string `json:"change_ids,omitempty"`
	Reverted   bool     `json:"reverted,omitempty"`
	Error      string   `json:"error,omitempty"`
}

func newChangesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "changes",
		Short: "Inspect and revert applied file changes",
		Long: `List applied file changes and revert them to their pre-change content.

Every accepted staged change is recorded in the change journal
(<state-dir>/changes.db) with a checksum of the applied content. Reverting
restores the original bytes — unless the file has changed since it was
applied, in which case revert refuses rather than clobbering newer edits.

Examples:
  meept changes list                       # Recent applied changes (all sessions)
  meept changes list --session sess-abc123 # Changes from one session
  meept changes list --limit 50            # More history
  meept changes list --json                # Machine-readable output
  meept changes revert change-1a2b3c4d     # Restore one file to its pre-change state`,
	}

	cmd.AddCommand(newChangesListCmd())
	cmd.AddCommand(newChangesRevertCmd())

	return cmd
}

func newChangesListCmd() *cobra.Command {
	var (
		sessionID string
		limit     int
		asJSON    bool
	)

	cmd := &cobra.Command{
		Use:   cmdList,
		Short: "list applied file changes",
		Long: `List recently applied (accepted) file changes, newest first.

The size column is the journaled pre-image size; entries marked
"no (size cap)" exceeded the pre-image cap and cannot be reverted.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runChangesList(sessionID, limit, asJSON)
		},
	}

	cmd.Flags().StringVar(&sessionID, "session", "", "Filter by session ID (default: all sessions)")
	cmd.Flags().IntVar(&limit, "limit", 25, "Maximum number of entries to show")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")

	return cmd
}

func newChangesRevertCmd() *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "revert <id>",
		Short: "restore a file to its state before an applied change",
		Long: `Restore the file touched by an applied change to its journaled pre-image.

Refuses when the on-disk file no longer matches what was applied ("file
changed since apply") so newer edits are never silently overwritten.
Reverting an already-reverted entry succeeds without rewriting the file.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runChangesRevert(args[0], asJSON)
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")

	return cmd
}

// openJournalForCmd opens the change journal at <stateDir>/changes.db,
// following the same default-state-dir convention as the daemon commands.
func openJournalForCmd() (*builtin.Journal, error) {
	dbPath := filepath.Join(stateDir, "changes.db")
	return builtin.NewJournal(builtin.JournalConfig{DBPath: dbPath}, nil)
}

func runChangesList(sessionID string, limit int, asJSON bool) error {
	journal, err := openJournalForCmd()
	if err != nil {
		return err
	}
	defer journal.Close()

	entries, err := journal.List(sessionID, limit)
	if err != nil {
		return err
	}

	if asJSON {
		out := make([]changesJSONEntry, 0, len(entries))
		for _, e := range entries {
			out = append(out, changesJSONEntryFromEntry(journal, e))
		}
		data, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return fmt.Errorf("encode json: %w", err)
		}
		fmt.Println(string(data))
		return nil
	}

	if len(entries) == 0 {
		fmt.Println("no applied changes journaled")
		return nil
	}

	fmt.Printf("%-22s %-40s %-20s %10s %s\n", "id", "file", "applied", "size", "revertable")
	for _, e := range entries {
		size, serr := journal.PreImageSize(e.ID)
		if serr != nil {
			size = 0
		}
		fmt.Printf("%-22s %-40s %-20s %10s %s\n",
			e.ID,
			truncateForDisplay(e.FilePath, 40),
			e.AppliedAt.Format("2006-01-02 15:04:05"),
			humanSize(size),
			revertableLabel(size),
		)
	}
	return nil
}

func runChangesRevert(entryID string, asJSON bool) error {
	journal, err := openJournalForCmd()
	if err != nil {
		return err
	}
	defer journal.Close()

	path, revertErr := journal.Revert(entryID, nil)

	if asJSON {
		entry := changesJSONEntry{ID: entryID, Reverted: revertErr == nil}
		if path != "" {
			entry.File = path
		}
		if revertErr != nil {
			entry.Error = revertErr.Error()
		}
		data, err := json.MarshalIndent(entry, "", "  ")
		if err != nil {
			return fmt.Errorf("encode json: %w", err)
		}
		fmt.Println(string(data))
		if revertErr != nil {
			os.Exit(1)
		}
		return nil
	}

	if revertErr != nil {
		return revertErr
	}
	fmt.Printf("reverted %s -> %s\n", entryID, path)
	return nil
}

func changesJSONEntryFromEntry(journal *builtin.Journal, e builtin.JournalEntry) changesJSONEntry {
	size, _ := journal.PreImageSize(e.ID)
	return changesJSONEntry{
		ID:         e.ID,
		SessionID:  e.SessionID,
		File:       e.FilePath,
		AppliedAt:  e.AppliedAt.UTC().Format("2006-01-02T15:04:05Z"),
		SizeBytes:  size,
		Revertable: size > 0,
		ChangeIDs:  e.ChangeIDs,
	}
}

func revertableLabel(preImageSize int64) string {
	if preImageSize > 0 {
		return "yes"
	}
	return "no (size cap)"
}

func truncateForDisplay(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return "…" + s[len(s)-max+3:]
}
