package session

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

// SessionRefresher periodically refreshes session titles based on
// conversation progress.
type SessionRefresher struct {
	llmClient *llm.Client
	logger    *slog.Logger
}

// NewSessionRefresher creates a new session refresher.
func NewSessionRefresher(llmClient *llm.Client, logger *slog.Logger) *SessionRefresher {
	if logger == nil {
		logger = slog.Default()
	}
	return &SessionRefresher{
		llmClient: llmClient,
		logger:    logger,
	}
}

// RefreshRequest contains parameters for refreshing a session title.
type RefreshRequest struct {
	SessionID   string   `json:"session_id"`
	Topic       string   `json:"topic,omitempty"`      // Current dominant topic
	TurnCount   int      `json:"turn_count"`           // Number of exchanges
	Keywords    []string `json:"keywords,omitempty"`   // Extracted keywords
	FirstMsg    string   `json:"first_message,omitempty"` // Original first message for context
}

// RefreshResult contains the updated title.
type RefreshResult struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Refresh generates an updated title based on session progress.
func (r *SessionRefresher) Refresh(ctx context.Context, req RefreshRequest) (*RefreshResult, error) {
	r.logger.Info("SessionRefresher.Refresh called",
		"session_id", req.SessionID,
		"turn_count", req.TurnCount,
		"topic", req.Topic,
		"keyword_count", len(req.Keywords),
	)

	if r.llmClient == nil {
		r.logger.Warn("No LLM client, generating simple title")
		return &RefreshResult{
			Name:        fmt.Sprintf("session-%d", req.TurnCount),
			Description: fmt.Sprintf("%d turns", req.TurnCount),
		}, nil
	}

	systemPrompt := `You are updating a session title based on conversation progress.
Generate a JSON object with:
1. "name": A single lowercase word capturing the dominant topic (like a folder name)
2. "description": A brief 3-6 word description in "category: detail" format

Categories: coding, research, task, personal, creative, system

Output ONLY valid JSON. Examples:
{"name": "debugging", "description": "coding: fixed auth bug"}
{"name": "refactoring", "description": "coding: cleanup module structure"}
{"name": "planning", "description": "task: sprint roadmap discussion"}

All lowercase. No punctuation.`

	// Build user prompt with context
	keywordsStr := strings.Join(req.Keywords, ", ")
	if keywordsStr == "" {
		keywordsStr = "none"
	}

	userPrompt := fmt.Sprintf(
		"Session has %d turns. Dominant topic: %s. Keywords: %s.\nProvide updated title that reflects current focus.",
		req.TurnCount, req.Topic, keywordsStr,
	)

	// Add original first message for context if available
	if req.FirstMsg != "" {
		if len(req.FirstMsg) > 100 {
			req.FirstMsg = req.FirstMsg[:100] + "..."
		}
		userPrompt += fmt.Sprintf("\nOriginal topic: %s", req.FirstMsg)
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	resp, err := r.llmClient.Chat(ctx, []llm.ChatMessage{
		{Role: llm.RoleSystem, Content: systemPrompt},
		{Role: llm.RoleUser, Content: userPrompt},
	}, llm.WithMaxTokens(80), llm.WithTemperature(0.3))

	if err != nil {
		r.logger.Warn("LLM refresh failed, using fallback",
			"error", err,
			"session_id", req.SessionID,
		)
		return &RefreshResult{
			Name:        fmt.Sprintf("session-%d", req.TurnCount),
			Description: fmt.Sprintf("%d turns, topic: %s", req.TurnCount, req.Topic),
		}, nil
	}

	r.logger.Info("LLM refresh response received",
		"raw_response", resp.Content,
		"session_id", req.SessionID,
	)

	// Parse JSON response and clean up
	name, desc := cleanTitleResult(
		resp.Content,
		fmt.Sprintf("session-%d", req.TurnCount),
		fmt.Sprintf("%d turns", req.TurnCount),
	)

	result := &RefreshResult{
		Name:        name,
		Description: desc,
	}

	r.logger.Info("Generated refreshed session title",
		"session_id", req.SessionID,
		"name", result.Name,
		"description", result.Description,
	)
	return result, nil
}
