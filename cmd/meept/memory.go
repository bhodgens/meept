package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func newMemoryCmd() *cobra.Command {
	var limit int

	cmd := &cobra.Command{
		Use:   "memory [query]",
		Short: "Search memories",
		Long: `Search the memory store.

Without arguments, lists recent memories.
With a query, searches for matching memories.

Examples:
  meept memory                    # List recent memories
  meept memory "project setup"    # Search for specific memories
  meept memory -n 50 "config"     # Search with custom limit`,
	}

	cmd.AddCommand(newMemoryExportCmd())
	cmd.AddCommand(newMemoryVectorCmd())
	cmd.AddCommand(newMemoryReviewCmd())
	cmd.AddCommand(newMemorySupersedeCmd())
	cmd.AddCommand(newMemoryPromoteCmd())
	cmd.AddCommand(newMemoryRejectCmd())

	cmd.Flags().IntVarP(&limit, "limit", "n", 10, "Maximum number of results")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		query := ""
		if len(args) > 0 {
			query = strings.Join(args, " ")
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
		fmt.Printf("\033[0m\n")
	}

	fmt.Printf("\n%d result(s)\n", len(items))
	return nil
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
				fmt.Scanln(&resp)
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
	cmd := &cobra.Command{
		Use:   "promote ID",
		Short: "promote an auto-claim to confirmed status",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			_, err = client.Call("memory.promoteClaim", map[string]string{"id": args[0]})
			if err != nil {
				return fmt.Errorf("failed to promote claim: %w", err)
			}

			fmt.Printf("claim %s promoted to confirmed\n", args[0])
			return nil
		},
	}
	return cmd
}

func newMemoryRejectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reject ID",
		Short: "reject an auto-claim",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			_, err = client.Call("memory.rejectClaim", map[string]string{"id": args[0]})
			if err != nil {
				return fmt.Errorf("failed to reject claim: %w", err)
			}

			fmt.Printf("claim %s rejected\n", args[0])
			return nil
		},
	}
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
