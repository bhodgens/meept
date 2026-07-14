package llm

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// taskTitlePrompt generates a concise task title from conversation context.
const taskTitlePrompt = `Generate a concise task title (max %d chars) that captures:
- The main topic/area (e.g., "Sidebar display", "API endpoint", "Database migration")
- The specific issue or action (e.g., "background image fix", "null pointer bug")

Format: "<TOPIC> - <SPECIFIC>"

Rules:
- Use lowercase for common words, capitalize proper nouns only
- Be specific enough to distinguish from similar tasks
- Omit articles (a, an, the) when space is tight
- Truncate with "…" if needed to fit the limit

Examples:
- "Sidebar display - background image fix"
- "API endpoint - rate limiter bug"
- "Database migration - user table schema"
- "Flutter UI - keyboard shortcut config"
- "Go tests - flaky race condition"

Conversation context:
%s

Return ONLY the title, no quotes or explanation.`

// sessionSummaryPrompt generates a medium-length session summary.
const sessionSummaryPrompt = `Summarize this coding session in 2-4 sentences. Capture:
- What was being built or fixed
- Key files modified
- Final outcome or current status

Session context:
%s

Return ONLY the summary, no quotes or explanation.`

// handoffSummaryPrompt generates a structured handoff for agent transitions.
const handoffSummaryPrompt = `You are producing a handoff summary for another agent that will continue this work.
Capture exact technical state, not abstractions.

## Goal
[What the user is trying to accomplish, stated exactly as given]

## Current State
[Exact current state: what is working, what is broken, what is in progress]

## Files
- [list every file path touched, prefixed with "read: ", "write: ", or "edit: "]

## Commands Run
- [list every shell command executed and its exact result]

## Errors Encountered
- [list exact error messages and what was learned]

## Next Steps
[ordered list of what remains, with exact file paths and symbol names]

## Critical Context
[Any exact values, API endpoints, config values, or identifiers that must be preserved]

Conversation context:
%s

Return ONLY the structured summary, no preamble.`

// TaskSummarizer generates concise summaries for task management.
type TaskSummarizer struct {
	chatter   Chatter
	tokenizer Tokenizer
}

// NewTaskSummarizer creates a new summarizer using the provided chatter.
func NewTaskSummarizer(chatter Chatter, tokenizer Tokenizer) *TaskSummarizer {
	if tokenizer == nil {
		tokenizer = &HeuristicTokenizer{}
	}
	return &TaskSummarizer{
		chatter:   chatter,
		tokenizer: tokenizer,
	}
}

// TaskTitleResult holds the result of a task title generation.
type TaskTitleResult struct {
	Title      string
	TokensUsed int
	Truncated  bool
}

// SessionSummaryResult holds the result of a session summarization.
type SessionSummaryResult struct {
	Summary    string
	TokensUsed int
}

// HandoffResult holds the result of a handoff summary generation.
type HandoffResult struct {
	Summary    string
	TokensUsed int
	Sections   map[string]string
}

// SummarizeTaskTitle generates a short, distinctive title for a task or subagent job.
func (s *TaskSummarizer) SummarizeTaskTitle(ctx context.Context, messages []ChatMessage, maxLen int) (TaskTitleResult, error) {
	if maxLen <= 0 {
		maxLen = 50
	}
	conversationText := s.serializeMessages(messages)
	if conversationText == "" {
		return TaskTitleResult{Title: "Unknown task"}, nil
	}
	// Handle nil chatter gracefully - return fallback title instead of panicking
	if s.chatter == nil {
		return TaskTitleResult{Title: "Task title generation unavailable"}, nil
	}
	prompt := fmt.Sprintf(taskTitlePrompt, maxLen, conversationText)
	resp, err := s.chatter.Chat(ctx, []ChatMessage{{Role: RoleUser, Content: prompt}})
	if err != nil {
		return TaskTitleResult{}, fmt.Errorf("task title generation failed: %w", err)
	}
	title := extractTitle(resp.Content, maxLen)
	tokensUsed := s.tokenizer.CountTokens(prompt) + s.tokenizer.CountTokens(resp.Content)
	return TaskTitleResult{
		Title:      title,
		TokensUsed: tokensUsed,
		Truncated:  len(title) >= maxLen,
	}, nil
}

// SummarizeSession generates a medium-length summary of a coding session.
func (s *TaskSummarizer) SummarizeSession(ctx context.Context, messages []ChatMessage) (SessionSummaryResult, error) {
	conversationText := s.serializeMessages(messages)
	if conversationText == "" {
		return SessionSummaryResult{Summary: "Empty session"}, nil
	}
	prompt := fmt.Sprintf(sessionSummaryPrompt, conversationText)
	resp, err := s.chatter.Chat(ctx, []ChatMessage{{Role: RoleUser, Content: prompt}})
	if err != nil {
		return SessionSummaryResult{}, fmt.Errorf("session summarization failed: %w", err)
	}
	summary := strings.TrimSpace(resp.Content)
	tokensUsed := s.tokenizer.CountTokens(prompt) + s.tokenizer.CountTokens(resp.Content)
	return SessionSummaryResult{
		Summary:    summary,
		TokensUsed: tokensUsed,
	}, nil
}

// SummarizeHandoff generates a structured handoff summary for agent transitions.
func (s *TaskSummarizer) SummarizeHandoff(ctx context.Context, messages []ChatMessage) (HandoffResult, error) {
	conversationText := s.serializeMessages(messages)
	if conversationText == "" {
		return HandoffResult{Summary: "No context to hand off"}, nil
	}
	prompt := fmt.Sprintf(handoffSummaryPrompt, conversationText)
	resp, err := s.chatter.Chat(ctx, []ChatMessage{{Role: RoleUser, Content: prompt}})
	if err != nil {
		return HandoffResult{}, fmt.Errorf("handoff summarization failed: %w", err)
	}
	summary := strings.TrimSpace(resp.Content)
	sections := parseHandoffSections(summary)
	tokensUsed := s.tokenizer.CountTokens(prompt) + s.tokenizer.CountTokens(resp.Content)
	return HandoffResult{
		Summary:    summary,
		TokensUsed: tokensUsed,
		Sections:   sections,
	}, nil
}

// serializeMessages converts chat messages to plain text for summarization.
func (s *TaskSummarizer) serializeMessages(messages []ChatMessage) string {
	if len(messages) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, msg := range messages {
		if msg.Role == RoleSystem && strings.Contains(msg.Content, "[Compacted Context]") {
			continue
		}
		switch msg.Role {
		case RoleUser:
			fmt.Fprintf(&sb, "[User]: %s\n", msg.Content)
		case RoleAssistant:
			if msg.Content != "" {
				fmt.Fprintf(&sb, "[Assistant]: %s\n", msg.Content)
			}
			for _, tc := range msg.ToolCalls {
				fmt.Fprintf(&sb, "  [Tool Call]: %s(%s)\n", tc.Function.Name, tc.Function.Arguments)
			}
		case RoleTool:
			content := msg.Content
			if len(content) > 500 {
				content = content[:500] + "..."
			}
			fmt.Fprintf(&sb, "  [Tool Result]: %s\n", content)
		}
	}
	return sb.String()
}

// extractTitle cleans and truncates a generated title to fit the max length.
func extractTitle(raw string, maxLen int) string {
	raw = strings.TrimSpace(raw)
	raw = regexp.MustCompile(`^["']|["']$`).ReplaceAllString(raw, "")
	raw = regexp.MustCompile(`^(Title:|Here:|Summary:)`).ReplaceAllString(raw, "")
	raw = strings.TrimSpace(raw)
	if idx := strings.Index(raw, "\n"); idx > 0 {
		raw = raw[:idx]
	}
	if len(raw) > maxLen {
		if maxLen > 3 {
			raw = raw[:maxLen-3] + "…"
		} else {
			raw = raw[:maxLen]
		}
	}
	return raw
}

// parseHandoffSections extracts structured sections from a handoff summary.
func parseHandoffSections(summary string) map[string]string {
	sections := make(map[string]string)
	headerRe := regexp.MustCompile(`(?m)^##\s+([^\n]+?)\s*$`)
	matches := headerRe.FindAllStringSubmatchIndex(summary, -1)
	for i, m := range matches {
		name := strings.TrimSpace(summary[m[2]:m[3]])
		start := m[1]
		end := len(summary)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		sections[name] = strings.TrimSpace(summary[start:end])
	}
	return sections
}
