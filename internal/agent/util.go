// Package agent provides utility functions for the agent package.
package agent

import (
	"fmt"
	"strings"
)

// truncateString truncates a string to the given max length (in runes), adding "..." if truncated.
// Rune-safe to avoid splitting multi-byte UTF-8 characters.
func truncateString(s string, maxLen int) string {
	if maxLen <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	// When maxLen is too small to fit any content alongside the ellipsis,
	// return just the ellipsis (matches the original byte-level behavior
	// and avoids a negative slice bound when maxLen < 3).
	if maxLen <= 3 {
		return "..."
	}
	return string(runes[:maxLen-3]) + "..."
}

// extractBriefDescription returns the first meaningful (non-heading, non-empty)
// line from a markdown body. This is used to produce a short summary from agent
// purpose bodies that start with "# Name" followed by a description paragraph.
func extractBriefDescription(body string) string {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		return trimmed
	}
	return ""
}

// formatTokenCount formats a token count for compact display.
// >= 1,000,000 -> "1.2M", >= 1,000 -> "1.5K", otherwise -> "42"
func formatTokenCount(count int) string {
	if count >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(count)/1_000_000)
	}
	if count >= 1_000 {
		return fmt.Sprintf("%.1fK", float64(count)/1_000)
	}
	return fmt.Sprintf("%d", count)
}
