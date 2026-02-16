package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/caimlas/meept/internal/tui"
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
		RunE: func(cmd *cobra.Command, args []string) error {
			query := ""
			if len(args) > 0 {
				query = strings.Join(args, " ")
			}
			return runMemorySearch(query, limit)
		},
	}

	cmd.Flags().IntVarP(&limit, "limit", "n", 10, "Maximum number of results")

	return cmd
}

func runMemorySearch(query string, limit int) error {
	socket := getSocketPath()

	client := tui.NewRPCClient(socket)
	if err := client.Connect(); err != nil {
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
		fmt.Println("No memories found")
		return nil
	}

	for i, item := range items {
		// Type badge
		memType := item.GetType()
		typeColor := getTypeColor(memType)

		// Truncate content
		content := item.Content
		content = strings.ReplaceAll(content, "\n", " ")
		if len(content) > 80 {
			content = content[:77] + "..."
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
