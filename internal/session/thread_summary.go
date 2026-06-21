package session

import (
	"fmt"
	"strings"
)

// AssembleThreadContext creates context from inactive thread summaries.
// This provides continuity when switching between threads.
func AssembleThreadContext(threads []*Thread, activeThreadID string) string {
	var sb strings.Builder

	for _, thread := range threads {
		if thread.ID != activeThreadID && thread.Summary != "" {
			sb.WriteString(fmt.Sprintf(
				"[Context from %s thread]: %s\n",
				thread.TopicLabel,
				thread.Summary,
			))
		}
	}

	if sb.Len() == 0 {
		return ""
	}

	return "\n" + sb.String() + "\n"
}

// GenerateThreadSummary creates a summary from thread messages.
func GenerateThreadSummary(messages []Message) string {
	if len(messages) == 0 {
		return ""
	}

	// Simple summary: first and last message preview
	firstMsg := messages[0]
	lastMsg := messages[len(messages)-1]

	firstPreview := truncateString(firstMsg.Content, 100)
	lastPreview := truncateString(lastMsg.Content, 100)

	return fmt.Sprintf("Discussion from %s: %s... (latest: %s...)",
		firstMsg.Role, firstPreview, lastPreview)
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
