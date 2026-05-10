package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/spf13/cobra"
)

func newCacheCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Manage token cache",
		Long: `Manage the LLM token cache.

Examples:
  meept cache status           # Show cache statistics
  meept cache clear            # Clear all cache entries
  meept cache invalidate file.go  # Invalidate entries for a file
  meept cache inspect --hash <hash>  # Inspect a specific cache entry`,
	}

	cmd.AddCommand(newCacheStatusCmd())
	cmd.AddCommand(newCacheClearCmd())
	cmd.AddCommand(newCacheInvalidateCmd())
	cmd.AddCommand(newCacheInspectCmd())

	return cmd
}

func newCacheStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show cache statistics",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCacheStatus()
		},
	}
}

func newCacheClearCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "clear",
		Short: "Clear all cache entries",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCacheClear()
		},
	}
}

func newCacheInvalidateCmd() *cobra.Command {
	var filePath string

	cmd := &cobra.Command{
		Use:   "invalidate",
		Short: "Invalidate cache entries",
		Long: `Invalidate cache entries by file path.

Examples:
  meept cache invalidate --path internal/llm/client.go
  meept cache invalidate -f pkg/models/client.go`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if filePath == "" {
				return fmt.Errorf("--path flag is required")
			}
			return runCacheInvalidate(filePath)
		},
	}

	cmd.Flags().StringVarP(&filePath, "path", "p", "", "File path to invalidate")
	if err := cmd.MarkFlagRequired("path"); err != nil {
		// The flag was just added above, so this cannot fail.
		log.Printf("cache: warning: MarkFlagRequired failed: %v", err)
	}

	return cmd
}

func newCacheInspectCmd() *cobra.Command {
	var promptHash string

	cmd := &cobra.Command{
		Use:   "inspect",
		Short: "inspect a specific cache entry",
		Long: `Inspect cache entries matching a prompt hash.

Shows cached response content, creation time, hit count, and associated file hashes.

Examples:
  meept cache inspect --hash abc123def456...
  meept cache inspect -H <sha256-hash>`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if promptHash == "" {
				return fmt.Errorf("--hash flag is required")
			}
			return runCacheInspect(promptHash)
		},
	}

	cmd.Flags().StringVarP(&promptHash, "hash", "H", "", "prompt hash to inspect")
	if err := cmd.MarkFlagRequired("hash"); err != nil {
		log.Printf("cache: warning: MarkFlagRequired failed: %v", err)
	}

	return cmd
}

func runCacheStatus() error {
	client, err := connectDaemon()
	if err != nil {
		return fmt.Errorf("failed to connect to daemon: %w\n\nMake sure the daemon is running:\n  meept daemon start", err)
	}
	defer client.Close()

	resp, err := client.CacheStats()
	if err != nil {
		return fmt.Errorf("failed to get cache stats: %w", err)
	}

	fmt.Println("Token Cache Statistics")
	fmt.Println("====================")
	fmt.Printf("L1 Cache:\n")
	fmt.Printf("  Entries:     %d\n", resp.L1Entries)
	fmt.Printf("  Hits:        %d\n", resp.L1Hits)
	fmt.Printf("  Misses:      %d\n", resp.L1Misses)
	fmt.Printf("  Evictions:   %d\n", resp.Evictions)
	fmt.Println()
	fmt.Printf("L2 Cache:\n")
	fmt.Printf("  Entries:     %d\n", resp.L2Entries)
	fmt.Printf("  Hits:        %d\n", resp.L2Hits)
	fmt.Printf("  Misses:      %d\n", resp.L2Misses)
	fmt.Println()
	fmt.Printf("Overall:\n")
	fmt.Printf("  Total Hits:  %d\n", resp.TotalHits)
	fmt.Printf("  Hit Rate:    %.1f%%\n", resp.HitRate)

	return nil
}

func runCacheClear() error {
	client, err := connectDaemon()
	if err != nil {
		return fmt.Errorf("failed to connect to daemon: %w\n\nMake sure the daemon is running:\n  meept daemon start", err)
	}
	defer client.Close()

	if err := client.CacheClear(); err != nil {
		return fmt.Errorf("failed to clear cache: %w", err)
	}

	fmt.Println("Token cache cleared successfully")
	return nil
}

func runCacheInvalidate(filePath string) error {
	client, err := connectDaemon()
	if err != nil {
		return fmt.Errorf("failed to connect to daemon: %w\n\nMake sure the daemon is running:\n  meept daemon start", err)
	}
	defer client.Close()

	if err := client.CacheInvalidate(filePath); err != nil {
		return fmt.Errorf("failed to invalidate cache: %w", err)
	}

	fmt.Printf("Cache entries invalidated for file: %s\n", filePath)
	return nil
}

func runCacheInspect(promptHash string) error {
	client, err := connectDaemon()
	if err != nil {
		return fmt.Errorf("failed to connect to daemon: %w\n\nMake sure the daemon is running:\n  meept daemon start", err)
	}
	defer client.Close()

	resp, err := client.CacheInspect(promptHash)
	if err != nil {
		return fmt.Errorf("failed to inspect cache: %w", err)
	}

	if !resp.Found || resp.Count == 0 {
		fmt.Printf("No cache entries found for prompt hash: %s\n", promptHash)
		return nil
	}

	fmt.Printf("Found %d cache entr%s for hash: %s\n", resp.Count, pluralize(resp.Count), promptHash)
	fmt.Println()

	for i, entry := range resp.Entries {
		fmt.Printf("Entry %d:\n", i+1)
		fmt.Printf("  Model:       %s\n", entry.ModelID)
		fmt.Printf("  Source:      %s\n", entry.Source)
		fmt.Printf("  Created:     %s\n", entry.CreatedAt)
		fmt.Printf("  Expires:     %s\n", entry.ExpiresAt)
		fmt.Printf("  Hit count:   %d\n", entry.HitCount)

		if len(entry.FileHashes) > 0 {
			fmt.Println("  File hashes:")
			for path, hash := range entry.FileHashes {
				fmt.Printf("    %s: %s\n", path, hash)
			}
		}

		if entry.Response != "" {
			// Truncate long responses for display
			response := entry.Response
			if len(response) > 500 {
				response = response[:500] + "..."
			}
			fmt.Println("  Response:")
			for _, line := range strings.Split(response, "\n") {
				fmt.Printf("    %s\n", line)
			}
		}

		if i < len(resp.Entries)-1 {
			fmt.Println()
		}
	}

	return nil
}

func pluralize(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}
