package main

import (
	"fmt"
	"log"

	"github.com/spf13/cobra"

	"github.com/caimlas/meept/internal/tui"
)

func newCacheCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Manage token cache",
		Long: `Manage the LLM token cache.

Examples:
  meept cache status           # Show cache statistics
  meept cache clear            # Clear all cache entries
  meept cache invalidate file.go  # Invalidate entries for a file`,
	}

	cmd.AddCommand(newCacheStatusCmd())
	cmd.AddCommand(newCacheClearCmd())
	cmd.AddCommand(newCacheInvalidateCmd())

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

func runCacheStatus() error {
	socket := getSocketPath()
	client := tui.NewRPCClient(socket)

	if err := client.Connect(); err != nil {
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
	socket := getSocketPath()
	client := tui.NewRPCClient(socket)

	if err := client.Connect(); err != nil {
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
	socket := getSocketPath()
	client := tui.NewRPCClient(socket)

	if err := client.Connect(); err != nil {
		return fmt.Errorf("failed to connect to daemon: %w\n\nMake sure the daemon is running:\n  meept daemon start", err)
	}
	defer client.Close()

	if err := client.CacheInvalidate(filePath); err != nil {
		return fmt.Errorf("failed to invalidate cache: %w", err)
	}

	fmt.Printf("Cache entries invalidated for file: %s\n", filePath)
	return nil
}
