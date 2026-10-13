package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/memory"
	"github.com/spf13/cobra"
)

func newMemoryCmd() *cobra.Command {
	var limit int
	var bridgesOnly bool

	cmd := &cobra.Command{
		Use:   "memory [query]",
		Short: "Search memories",
		Long: `Search the memory store.

Without arguments, lists recent memories.
With a query, searches for matching memories.
With --bridges, lists bridging facts instead of memories.

Examples:
  meept memory                    # List recent memories
  meept memory "project setup"    # Search for specific memories
  meept memory -n 50 "config"     # Search with custom limit
  meept memory --bridges          # List bridging facts`,
	}

	cmd.AddCommand(newMemoryExportCmd())
	cmd.AddCommand(newMemoryVectorCmd())
	cmd.AddCommand(newMemoryReviewCmd())
	cmd.AddCommand(newMemorySupersedeCmd())
	cmd.AddCommand(newMemoryPromoteCmd())
	cmd.AddCommand(newMemoryRejectCmd())
	cmd.AddCommand(newMemoryExpiredCmd())
	cmd.AddCommand(newMemoryL0Cmd())
	cmd.AddCommand(newMemoryConflictsCmd())

	cmd.Flags().IntVarP(&limit, "limit", "n", 10, "Maximum number of results")
	cmd.Flags().BoolVar(&bridgesOnly, "bridges", false, "List bridging facts instead of searching memories")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		query := ""
		if len(args) > 0 {
			query = strings.Join(args, " ")
		}
		if bridgesOnly {
			return runMemoryBridges(limit)
		}
		return runMemorySearch(query, limit)
	}

	return cmd
}

func newMemoryExportCmd() *cobra.Command {
	var format, category string

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export memories",
		Long: `Export memories in a specified format.

Formats: json, markdown
Categories: episodic, task, personality (or omit for all)

Examples:
  meept memory export                  # Export all memories as JSON
  meept memory export -f markdown      # Export in markdown format
  meept memory export -c episodic      # Export only episodic memories`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			params := map[string]string{
				"format":   format,
				"category": category,
			}

			rawResult, err := client.Call("memory.export", params)
			if err != nil {
				return fmt.Errorf("failed to export memories: %w", err)
			}

			var result map[string]any
			if err := json.Unmarshal(rawResult, &result); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if errMsg, ok := result["error"].(string); ok && errMsg != "" {
				return fmt.Errorf("%s", errMsg)
			}

			count, _ := result["count"].(float64)
			if count == 0 {
				// Newer daemons wrap the payload as {"data": [...], "format": ...}.
				if data, ok := result["data"].([]any); ok {
					count = float64(len(data))
				}
			}
			fmt.Printf("Exported %d memories\n", int(count))
			return nil
		},
	}

	cmd.Flags().StringVarP(&format, "format", "f", "json", "Export format: json, markdown")
	cmd.Flags().StringVarP(&category, "category", "c", "", "Filter by category")

	return cmd
}

func runMemorySearch(query string, limit int) error {
	client, err := connectDaemon()
	if err != nil {
		return fmt.Errorf("failed to connect to daemon: %w\n\nMake sure the daemon is running:\n  meept daemon start", err)
	}
	defer client.Close()

	// Use a broad query if none provided
	if query == "" {
		query = "*"
	}

	resp, err := client.QueryMemory(query, limit)
	if err != nil {
		return fmt.Errorf("failed to query memory: %w", err)
	}

	items := resp.GetItems()
	if len(items) == 0 {
		fmt.Println("no memories found")
		return nil
	}

	for i, item := range items {
		// Type badge
		memType := item.GetType()
		typeColor := getTypeColor(memType)

		// Truncate content
		content := item.Content
		content = strings.ReplaceAll(content, "\n", " ")
		runes := []rune(content)
		if len(runes) > 80 {
			content = string(runes[:77]) + "..."
		}

		fmt.Printf("\n%s[%d] %s%s\n", typeColor, i+1, memType, "\033[0m")
		fmt.Printf("    %s\n", content)
		fmt.Printf("    \033[90mRelevance: %.2f", item.RelevanceScore)
		if item.CreatedAt != "" {
			fmt.Printf(" | Created: %s", item.CreatedAt)
		}
		// Bridge-tagged rows (metadata unit_type=bridge, written by the
		// BridgeStore) surface their verified flag + evidence count so the
		// derived nature stays visible next to source memories.
		if meta := item.Metadata; meta != nil {
			if unitType, _ := meta["unit_type"].(string); unitType == "bridge" {
				verified, _ := meta["verified"].(string)
				evCount := bridgeEvidenceCount(meta)
				fmt.Printf(" | verified: %s | evidence: %d", bridgeVerifiedText(verified), evCount)
			}
		}
		fmt.Printf("\033[0m\n")
	}

	fmt.Printf("\n%d result(s)\n", len(items))
	return nil
}

// runMemoryBridges lists persisted bridging facts via the memory.bridges RPC
// (branch 09 B06). Lowercase output, one row per bridge: id, verified flag,
// evidence count, entity, content.
func runMemoryBridges(limit int) error {
	client, err := connectDaemon()
	if err != nil {
		return fmt.Errorf("failed to connect to daemon: %w\n\nMake sure the daemon is running:\n  meept daemon start", err)
	}
	defer client.Close()

	rawResult, err := client.Call("memory.bridges", map[string]any{})
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unavailable") {
			fmt.Println("no bridges (bridge generation is disabled; enable memory.bridges.enabled)")
			return nil
		}
		return fmt.Errorf("failed to list bridges: %w", err)
	}

	var result struct {
		Bridges []struct {
			ID            string         `json:"id"`
			Content       string         `json:"content"`
			Verified      bool           `json:"verified"`
			EvidenceCount int            `json:"evidence_count"`
			CreatedAt     string         `json:"created_at"`
			Entity        string         `json:"entity"`
			Metadata      map[string]any `json:"metadata"`
		} `json:"bridges"`
	}
	if err := json.Unmarshal(rawResult, &result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if len(result.Bridges) == 0 {
		fmt.Println("no bridges (bridge generation is disabled or has produced none yet; enable memory.bridges.enabled)")
		return nil
	}

	shown := result.Bridges
	if limit > 0 && len(shown) > limit {
		shown = shown[:limit]
	}
	for i, b := range shown {
		verified := "no"
		if b.Verified {
			verified = "yes"
		}
		content := strings.ReplaceAll(b.Content, "\n", " ")
		runes := []rune(content)
		if len(runes) > 80 {
			content = string(runes[:77]) + "..."
		}
		entity := b.Entity
		if entity == "" {
			entity = "-"
		}
		fmt.Printf("\n%s[bridge %d] %s%s\n", "\033[35m", i+1, "verified: "+verified, "\033[0m")
		fmt.Printf("    %s\n", content)
		fmt.Printf("    \033[90mid: %s | evidence: %d | entity: %s", b.ID, b.EvidenceCount, entity)
		if b.CreatedAt != "" {
			fmt.Printf(" | created: %s", b.CreatedAt)
		}
		fmt.Printf("\033[0m\n")
	}
	fmt.Printf("\n%d bridge(s)\n", len(shown))
	return nil
}

// bridgeEvidenceCount reads evidence_ids out of bridge metadata, tolerating
// both the JSON-string and pre-decoded array shapes.
func bridgeEvidenceCount(meta map[string]any) int {
	switch ev := meta["evidence_ids"].(type) {
	case string:
		var ids []string
		if err := json.Unmarshal([]byte(ev), &ids); err == nil {
			return len(ids)
		}
	case []any:
		return len(ev)
	}
	return 0
}

// bridgeVerifiedText renders the verified metadata flag ("true"/"false")
// as a lowercase word; unknown values render as "unknown".
func bridgeVerifiedText(verified string) string {
	switch verified {
	case "true":
		return "yes"
	case "false":
		return "no"
	default:
		return "unknown"
	}
}

func getTypeColor(memType string) string {
	switch strings.ToLower(memType) {
	case "episodic":
		return "\033[36m" // Cyan
	case "task":
		return "\033[33m" // Yellow
	case "personality":
		return "\033[35m" // Magenta
	default:
		return "\033[37m" // White
	}
}

// newMemoryVectorCmd creates the vector search subcommand.
func newMemoryVectorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vector",
		Short: "Vector search operations",
		Long: `Vector search operations using semantic embeddings.

Examples:
  meept memory vector search "query string"    # Search using vector similarity
  meept memory vector stats                    # Show vector shard statistics`,
	}

	cmd.AddCommand(newMemoryVectorSearchCmd())
	cmd.AddCommand(newMemoryVectorStatsCmd())

	return cmd
}

// newMemoryVectorSearchCmd creates the vector search subcommand.
func newMemoryVectorSearchCmd() *cobra.Command {
	var limit int

	cmd := &cobra.Command{
		Use:   "search [query]",
		Short: "Search using vector similarity",
		Long: `Search memories using semantic vector similarity.

Examples:
  meept memory vector search "Go concurrency patterns"
  meept memory vector search "database optimization" -n 20`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("query is required")
			}
			query := strings.Join(args, " ")
			return runVectorSearch(query, limit)
		},
	}

	cmd.Flags().IntVarP(&limit, "limit", "n", 10, "Maximum number of results")
	return cmd
}

// newMemoryVectorStatsCmd creates the vector stats subcommand.
func newMemoryVectorStatsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show vector shard statistics",
		Long: `Display statistics about vector shards including:
- Number of loaded shards
- LRU cache statistics
- Per-shard vector counts and dimensions`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runVectorStats()
		},
	}
	return cmd
}

// runVectorSearch performs vector similarity search.
func runVectorSearch(query string, limit int) error {
	client, err := connectDaemon()
	if err != nil {
		return fmt.Errorf("failed to connect to daemon: %w\n\nMake sure the daemon is running:\n  meept daemon start", err)
	}
	defer client.Close()

	// Call vector search endpoint
	params := map[string]any{
		"query": query,
		"limit": limit,
	}

	rawResult, err := client.Call("memory.vector.search", params)
	if err != nil {
		return fmt.Errorf("failed to search vectors: %w", err)
	}

	var result struct {
		Results []struct {
			MemoryID         string         `json:"memory_id"`
			Content          string         `json:"content"`
			Metadata         map[string]any `json:"metadata,omitempty"`
			RelevanceScore   float64        `json:"relevance_score"`
			VectorSimilarity float64        `json:"vector_similarity"`
		} `json:"results"`
	}

	if err := json.Unmarshal(rawResult, &result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if len(result.Results) == 0 {
		fmt.Println("no vector search results found")
		return nil
	}

	for i, r := range result.Results {
		content := strings.ReplaceAll(r.Content, "\n", " ")
		if len([]rune(content)) > 80 {
			content = string([]rune(content)[:77]) + "..."
		}

		fmt.Printf("\n\033[36m[%d]\033[0m %s\n", i+1, r.MemoryID)
		fmt.Printf("    %s\n", content)
		fmt.Printf("    \033[90mSimilarity: %.3f | Relevance: %.3f\033[0m\n", r.VectorSimilarity, r.RelevanceScore)
	}

	fmt.Printf("\n%d result(s)\n", len(result.Results))
	return nil
}

// runVectorStats displays vector shard statistics.
func runVectorStats() error {
	client, err := connectDaemon()
	if err != nil {
		return fmt.Errorf("failed to connect to daemon: %w\n\nMake sure the daemon is running:\n  meept daemon start", err)
	}
	defer client.Close()

	rawResult, err := client.Call("memory.vector.stats", nil)
	if err != nil {
		return fmt.Errorf("failed to get vector stats: %w", err)
	}

	var stats struct {
		LoadedShards int   `json:"loaded_shards"`
		MaxRAMShards int   `json:"max_ram_shards"`
		LRUHits      int64 `json:"lru_hits"`
		LRUMisses    int64 `json:"lru_misses"`
		LRUEvictions int64 `json:"lru_evictions"`
		ShardDetails map[string]struct {
			Dimension      int    `json:"dimension"`
			M              int    `json:"m"`
			EFConstruction int    `json:"ef_construction"`
			EFSearch       int    `json:"ef_search"`
			VectorCount    int64  `json:"vector_count"`
			DatabaseSize   int64  `json:"database_size_bytes"`
			ShardID        string `json:"shard_id"`
		} `json:"shard_details"`
	}

	if err := json.Unmarshal(rawResult, &stats); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	fmt.Println("vector shard statistics")
	fmt.Println("=======================")
	fmt.Printf("Loaded Shards: %d / %d (max)\n", stats.LoadedShards, stats.MaxRAMShards)
	fmt.Printf("LRU Cache: %d hits, %d misses, %d evictions\n\n", stats.LRUHits, stats.LRUMisses, stats.LRUEvictions)

	if len(stats.ShardDetails) == 0 {
		fmt.Println("no shard details available")
		return nil
	}

	fmt.Println("shard details:")
	for name, detail := range stats.ShardDetails {
		fmt.Printf("\n  %s:\n", name)
		fmt.Printf("    Dimension: %d\n", detail.Dimension)
		fmt.Printf("    M: %d, EF Construction: %d, EF Search: %d\n", detail.M, detail.EFConstruction, detail.EFSearch)
		fmt.Printf("    Vectors: %d\n", detail.VectorCount)
		fmt.Printf("    Database Size: %s\n", formatBytes(detail.DatabaseSize))
	}

	return nil
}

// formatBytes formats bytes into human-readable size.
func formatBytes(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", float64(bytes)/GB)
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/MB)
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/KB)
	default:
		return fmt.Sprintf("%d bytes", bytes)
	}
}

// --- Epistemic Memory CLI Subcommands ---

func newMemoryReviewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "review",
		Short: "review pending epistemic memories",
		Long:  `Review auto-extracted claims, pending decisions, and unresolved predictions.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			rawResult, err := client.Call("memory.reviewQueue", map[string]any{})
			if err != nil {
				return fmt.Errorf("failed to query review queue: %w", err)
			}

			var result map[string]any
			if err := json.Unmarshal(rawResult, &result); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			renderReviewQueue(result)
			return nil
		},
	}
	return cmd
}

func newMemorySupersedeCmd() *cobra.Command {
	var confirm bool

	cmd := &cobra.Command{
		Use:   "supersede OLD_ID NEW_ID",
		Short: "mark a claim as superseded by a newer one",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			oldID, newID := args[0], args[1]

			if !confirm {
				fmt.Printf("supersede claim %s with %s?\n", oldID, newID)
				fmt.Print("confirm? [y/N] ")
				var resp string
				// EOF (piped empty stdin) means no confirmation was given;
				// resp stays empty and the branch below cancels.
				if _, scanErr := fmt.Scanln(&resp); scanErr != nil && !errors.Is(scanErr, io.EOF) {
					return fmt.Errorf("read confirmation: %w", scanErr)
				}
				if strings.ToLower(resp) != "y" {
					fmt.Println("cancelled")
					return nil
				}
			}

			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			rawResult, err := client.Call("memory.markSuperseded", map[string]string{
				"old_id": oldID,
				"new_id": newID,
			})
			if err != nil {
				return fmt.Errorf("failed to supersede: %w", err)
			}

			var result map[string]any
			if err := json.Unmarshal(rawResult, &result); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if redirected, ok := result["redirected_edges"].(float64); ok {
				fmt.Printf("superseded: %d edges redirected\n", int(redirected))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&confirm, "confirm", false, "skip confirmation prompt")
	return cmd
}

func newMemoryPromoteCmd() *cobra.Command {
	var reason string

	cmd := &cobra.Command{
		Use:   "promote ID",
		Short: "promote an auto-claim to confirmed status",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(reason) > memory.MaxVerdictReasonBytes {
				return fmt.Errorf("reason exceeds %d bytes (got %d)", memory.MaxVerdictReasonBytes, len(reason))
			}
			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			params := map[string]any{"id": args[0]}
			if reason != "" {
				params["reason"] = reason
			}
			_, err = client.Call("memory.promoteClaim", params)
			if err != nil {
				return fmt.Errorf("failed to promote claim: %w", err)
			}

			fmt.Printf("claim %s promoted to confirmed\n", args[0])
			return nil
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "optional verdict reason (max 512 bytes, stored with the claim)")
	return cmd
}

func newMemoryExpiredCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "expired",
		Short: "list claims whose validity window has closed",
		Long: `List epistemic claims whose valid_to timestamp is in the past.

Expired claims are hard-excluded from trust-weighted results; this command
shows what has aged out so it can be re-verified or rejected.

Examples:
  meept memory expired           # List expired claims (max 20)
  meept memory expired --limit 50`,
		RunE: func(cmd *cobra.Command, args []string) error {
			limit, err := cmd.Flags().GetInt("limit")
			if err != nil {
				return fmt.Errorf("read limit flag: %w", err)
			}

			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			rawResult, err := client.Call("memory.listExpired", map[string]any{"limit": limit})
			if err != nil {
				return fmt.Errorf("failed to list expired claims: %w", err)
			}

			var result struct {
				Claims []struct {
					MemoryID string `json:"memory_id"`
					Text     string `json:"text"`
					ValidTo  string `json:"valid_to"`
					Rev      int64  `json:"rev"`
					Status   any    `json:"status"`
				} `json:"claims"`
				// Newer daemons return raw memory results instead.
				Memories []struct {
					Memory struct {
						ID       string         `json:"id"`
						Content  string         `json:"content"`
						Metadata map[string]any `json:"metadata"`
					} `json:"memory"`
				} `json:"memories"`
			}
			if err := json.Unmarshal(rawResult, &result); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			type row struct {
				id, text, validTo string
				rev               int64
			}
			var rows []row
			for _, c := range result.Claims {
				status, _ := c.Status.(string)
				if status == "" {
					continue
				}
				rows = append(rows, row{id: c.MemoryID, text: c.Text, validTo: c.ValidTo, rev: c.Rev})
			}
			if len(rows) == 0 {
				for _, m := range result.Memories {
					vt, _ := m.Memory.Metadata["valid_to"].(string)
					rev := int64(0)
					if f, ok := m.Memory.Metadata["rev"].(float64); ok {
						rev = int64(f)
					}
					rows = append(rows, row{id: m.Memory.ID, text: m.Memory.Content, validTo: vt, rev: rev})
				}
			}

			if len(rows) == 0 {
				fmt.Println("no expired claims")
				return nil
			}
			for _, r := range rows {
				text := strings.ReplaceAll(r.text, "\n", " ")
				runes := []rune(text)
				if len(runes) > 80 {
					text = string(runes[:80])
				}
				fmt.Printf("%s  rev=%d  valid_to=%s  %s\n", r.id, r.rev, r.validTo, text)
			}
			return nil
		},
	}
	cmd.Flags().Int("limit", 20, "max claims to list")
	return cmd
}

func newMemoryConflictsCmd() *cobra.Command {
	var since string

	cmd := &cobra.Command{
		Use:   "conflicts",
		Short: "list claims created by conflict merge decisions",
		Long: `List claims the conflict detector created by merging candidates into
existing claims (metadata conflict_action=merge).

Each row shows the merged claim, the old claims it superseded, and the
detector's reason — the audit trail for automated merges.

Examples:
  meept memory conflicts                  # all merge claims (max 50)
  meept memory conflicts --since=48h      # merges from the last 48 hours`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			params := map[string]any{}
			if since != "" {
				d, err := time.ParseDuration(since)
				if err != nil {
					return fmt.Errorf("invalid --since duration %q: %w", since, err)
				}
				params["since_hours"] = int(d.Hours())
			}

			rawResult, err := client.Call("memory.listConflicts", params)
			if err != nil {
				return fmt.Errorf("failed to list conflict merges: %w", err)
			}

			var result struct {
				Memories []struct {
					Memory struct {
						ID        string         `json:"id"`
						Content   string         `json:"content"`
						CreatedAt string         `json:"created_at"`
						Metadata  map[string]any `json:"metadata"`
					} `json:"memory"`
				} `json:"memories"`
			}
			if err := json.Unmarshal(rawResult, &result); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if len(result.Memories) == 0 {
				fmt.Println("no conflict merge claims")
				return nil
			}
			for _, m := range result.Memories {
				text := strings.ReplaceAll(m.Memory.Content, "\n", " ")
				runes := []rune(text)
				if len(runes) > 80 {
					text = string(runes[:80])
				}
				targets := "[]"
				if raw, ok := m.Memory.Metadata["conflict_targets"].([]any); ok && len(raw) > 0 {
					ids := make([]string, 0, len(raw))
					for _, t := range raw {
						if s, ok := t.(string); ok {
							ids = append(ids, s)
						}
					}
					targets = strings.Join(ids, ",")
				}
				fmt.Printf("%s  targets=%s  %s\n", m.Memory.ID, targets, text)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&since, "since", "", "only merges newer than this duration ago (e.g. 48h)")
	return cmd
}

func newMemoryRejectCmd() *cobra.Command {
	var reason string

	cmd := &cobra.Command{
		Use:   "reject ID",
		Short: "reject an auto-claim",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(reason) > memory.MaxVerdictReasonBytes {
				return fmt.Errorf("reason exceeds %d bytes (got %d)", memory.MaxVerdictReasonBytes, len(reason))
			}
			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			params := map[string]any{"id": args[0]}
			if reason != "" {
				params["reason"] = reason
			}
			_, err = client.Call("memory.rejectClaim", params)
			if err != nil {
				return fmt.Errorf("failed to reject claim: %w", err)
			}

			fmt.Printf("claim %s rejected\n", args[0])
			return nil
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "optional verdict reason (max 512 bytes, stored with the claim)")
	return cmd
}

func renderReviewQueue(result map[string]any) {
	if autoClaims, ok := result["auto_claims"].([]any); ok && len(autoClaims) > 0 {
		fmt.Println("\n\033[33m=== auto-extracted claims (pending review) ===\033[0m")
		for i, item := range autoClaims {
			if m, ok := item.(map[string]any); ok {
				content, _ := m["content"].(string)
				content = strings.ReplaceAll(content, "\n", " ")
				if len([]rune(content)) > 80 {
					content = string([]rune(content)[:77]) + "..."
				}
				fmt.Printf("  [%d] %s\n", i+1, content)
			}
		}
	}

	if decisions, ok := result["pending_decisions"].([]any); ok && len(decisions) > 0 {
		fmt.Println("\n\033[36m=== pending decisions (due for review) ===\033[0m")
		for i, item := range decisions {
			if m, ok := item.(map[string]any); ok {
				content, _ := m["content"].(string)
				content = strings.ReplaceAll(content, "\n", " ")
				if len([]rune(content)) > 80 {
					content = string([]rune(content)[:77]) + "..."
				}
				fmt.Printf("  [%d] %s\n", i+1, content)
			}
		}
	}

	if predictions, ok := result["pending_predictions"].([]any); ok && len(predictions) > 0 {
		fmt.Println("\n\033[35m=== pending predictions (awaiting resolution) ===\033[0m")
		for i, item := range predictions {
			if m, ok := item.(map[string]any); ok {
				content, _ := m["content"].(string)
				content = strings.ReplaceAll(content, "\n", " ")
				if len([]rune(content)) > 80 {
					content = string([]rune(content)[:77]) + "..."
				}
				fmt.Printf("  [%d] %s\n", i+1, content)
			}
		}
	}

	total := 0
	for _, key := range []string{"auto_claims", "pending_decisions", "pending_predictions"} {
		if items, ok := result[key].([]any); ok {
			total += len(items)
		}
	}
	if total == 0 {
		fmt.Println("no items pending review")
	} else {
		fmt.Printf("\n%d item(s) pending review\n", total)
	}
}

// --- L0 raw-conversation tier (tree 01) ---

func newMemoryL0Cmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "l0",
		Short: "l0 raw-conversation store operations",
		Long: `Observability for the L0 raw-conversation tier (layered async
distillation). Reads <memory.data_dir>/l0/ directly — no daemon required.`,
	}
	cmd.AddCommand(newMemoryL0StatsCmd())
	return cmd
}

// newMemoryL0StatsCmd builds `meept memory l0 stats`: file count, line
// count, and oldest date across the daily JSONL files. Direct file reads
// over the configured memory.data_dir; a missing l0 dir reports zeroes
// (not an error — l0 may never have been enabled).
func newMemoryL0StatsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stats",
		Short: "show l0 file/line counts and oldest date",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadDefault()
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			dataDir := cfg.Memory.DataDir
			if dataDir == "" {
				dataDir = config.MeeptPath("memory")
			}
			return runL0Stats(filepath.Join(dataDir, "l0"))
		},
	}
}

// runL0Stats scans the daily YYYY-MM-DD.jsonl files in dir and prints file
// count, total line count, and the oldest date. Torn/blank lines still
// count as lines (they are on disk); only files matching the frozen daily
// layout are considered.
func runL0Stats(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("l0: no files (l0 recording not enabled or no turns recorded)")
			return nil
		}
		return fmt.Errorf("l0: read dir: %w", err)
	}

	files := 0
	lines := 0
	oldest := ""
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		dateStr, ok := strings.CutSuffix(name, ".jsonl")
		if !ok {
			continue
		}
		if _, err := time.Parse("2006-01-02", dateStr); err != nil {
			continue // not a daily file; skip
		}
		files++
		if oldest == "" || dateStr < oldest {
			oldest = dateStr
		}
		n, err := countLines(filepath.Join(dir, name))
		if err != nil {
			return fmt.Errorf("l0: count lines in %s: %w", name, err)
		}
		lines += n
	}

	fmt.Printf("files: %d\n", files)
	fmt.Printf("lines: %d\n", lines)
	if oldest == "" {
		fmt.Println("oldest: (none)")
	} else {
		fmt.Printf("oldest: %s\n", oldest)
	}
	return nil
}
