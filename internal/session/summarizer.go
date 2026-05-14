package session

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

// Summarizer generates concise session descriptions using an LLM.
type Summarizer struct {
	llmClient *llm.Client
	logger    *slog.Logger
}

// NewSummarizer creates a new session summarizer.
func NewSummarizer(llmClient *llm.Client, logger *slog.Logger) *Summarizer {
	if logger == nil {
		logger = slog.Default()
	}
	return &Summarizer{
		llmClient: llmClient,
		logger:    logger,
	}
}

// SummarizeRequest contains the data needed to generate a summary.
type SummarizeRequest struct {
	FirstMessage string // The first user message in the session
	ProjectName  string // Optional project/cwd name for context
}

// SummarizeResult contains both the session name and description.
type SummarizeResult struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// GenerateDescription creates a concise session name and description from the first message.
// Returns a SummarizeResult with name (single word) and description (category: brief description).
func (s *Summarizer) GenerateDescription(ctx context.Context, req SummarizeRequest) (*SummarizeResult, error) {
	s.logger.Info("Summarizer.GenerateDescription called",
		"first_message_len", len(req.FirstMessage),
		"project_name", req.ProjectName,
		"has_llm_client", s.llmClient != nil,
	)

	if s.llmClient == nil {
		s.logger.Warn("No LLM client available for summarization, using simple extraction")
		return extractSimpleResult(req.FirstMessage), nil
	}

	systemPrompt := `You are a session summarizer. Generate a JSON object with:
1. "name": A single lowercase word that captures the topic (like a folder name)
2. "description": A brief 3-8 word description in "category: detail" format

Categories for description:
- "personal" - health, relationships, life questions
- "coding" - programming, debugging, code review
- "research" - learning, information gathering
- "task" - todo lists, planning, organization
- "creative" - writing, art, brainstorming
- "system" - system administration, devops
- Use the project name if discussing a specific codebase

Output ONLY valid JSON. Examples:
{"name": "debugging", "description": "coding: fix null pointer in auth"}
{"name": "weather", "description": "research: local forecast query"}
{"name": "vacation", "description": "task: plan hawaii itinerary"}
{"name": "headache", "description": "personal: remedy for migraines"}

All lowercase. No punctuation at end of description.`

	userPrompt := req.FirstMessage
	if req.ProjectName != "" {
		userPrompt = fmt.Sprintf("[Project: %s]\n\n%s", req.ProjectName, req.FirstMessage)
	}

	messages := []llm.ChatMessage{
		{Role: llm.RoleSystem, Content: systemPrompt},
		{Role: llm.RoleUser, Content: userPrompt},
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	s.logger.Debug("Sending summarization request to LLM",
		"system_prompt_len", len(systemPrompt),
		"user_prompt_len", len(userPrompt),
	)

	resp, err := s.llmClient.Chat(ctx, messages,
		llm.WithMaxTokens(100),
		llm.WithTemperature(0.3),
	)
	if err != nil {
		s.logger.Warn("LLM summarization request failed, using fallback", "error", err)
		return extractSimpleResult(req.FirstMessage), nil
	}

	s.logger.Debug("LLM summarization response received", "raw_response", resp.Content)

	// Parse JSON response
	content := strings.TrimSpace(resp.Content)
	var result SummarizeResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		s.logger.Warn("Failed to parse JSON response, using fallback", "error", err, "content", content)
		return extractSimpleResult(req.FirstMessage), nil
	}

	// Clean up and validate
	result.Name = strings.ToLower(strings.TrimSpace(result.Name))
	result.Description = strings.ToLower(strings.TrimSpace(result.Description))
	result.Description = strings.TrimSuffix(result.Description, ".")

	// Ensure name is a single word
	if words := strings.Fields(result.Name); len(words) > 1 {
		result.Name = words[0]
	}
	if result.Name == "" {
		result.Name = "chat"
	}
	if len(result.Description) > 60 {
		result.Description = result.Description[:60] + "..."
	}

	s.logger.Debug("Generated session summary", "name", result.Name, "description", result.Description)
	return &result, nil
}

// SummarizeBranchRequest contains the data needed to summarize a conversation branch.
type SummarizeBranchRequest struct {
	Messages []llm.ChatMessage
	BranchID string
}

// SummarizeBranchResult contains the output of branch summarization.
type SummarizeBranchResult struct {
	Summary  string
	BranchID string
	MsgCount int
}

// SummarizeBranch generates a summary of a conversation branch.
// Returns nil if fewer than 3 messages (below threshold).
// Falls back to simple extraction if LLM client is nil or call fails.
func (s *Summarizer) SummarizeBranch(ctx context.Context, req SummarizeBranchRequest) (*SummarizeBranchResult, error) {
	if len(req.Messages) < 3 {
		return nil, nil //nolint:nilnil // below-threshold branch is not an error; caller checks for nil result
	}

	if s.llmClient == nil {
		s.logger.Warn("No LLM client for branch summarization, using fallback",
			"branch_id", req.BranchID,
			"msg_count", len(req.Messages),
		)
		return &SummarizeBranchResult{
			Summary:  fallbackBranchSummary(req),
			BranchID: req.BranchID,
			MsgCount: len(req.Messages),
		}, nil
	}

	systemPrompt := `You are a conversation summarizer. Summarize the following conversation branch in 2-3 concise sentences.
Focus on: what was discussed, what was decided or concluded, and any important outcomes.
Do not include pleasantries or filler. Be factual and direct.`

	// Build conversation content from messages
	var parts []string
	for _, msg := range req.Messages {
		parts = append(parts, fmt.Sprintf("[%s]: %s", string(msg.Role), msg.Content))
	}
	userPrompt := fmt.Sprintf("Summarize this conversation branch (branch: %s):\n\n%s",
		req.BranchID,
		strings.Join(parts, "\n"),
	)

	// Truncate if too long
	if len(userPrompt) > 8000 {
		userPrompt = userPrompt[:8000] + "\n... (truncated)"
	}

	messages := []llm.ChatMessage{
		{Role: llm.RoleSystem, Content: systemPrompt},
		{Role: llm.RoleUser, Content: userPrompt},
	}

	sumCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	resp, err := s.llmClient.Chat(sumCtx, messages,
		llm.WithMaxTokens(300),
		llm.WithTemperature(0.3),
	)
	if err != nil {
		s.logger.Warn("LLM branch summarization failed, using fallback",
			"error", err,
			"branch_id", req.BranchID,
		)
		return &SummarizeBranchResult{
			Summary:  fallbackBranchSummary(req),
			BranchID: req.BranchID,
			MsgCount: len(req.Messages),
		}, nil
	}

	summary := strings.TrimSpace(resp.Content)
	if summary == "" {
		summary = fallbackBranchSummary(req)
	}

	return &SummarizeBranchResult{
		Summary:  summary,
		BranchID: req.BranchID,
		MsgCount: len(req.Messages),
	}, nil
}

// fallbackBranchSummary generates a simple summary without LLM.
func fallbackBranchSummary(req SummarizeBranchRequest) string {
	var firstUserMsg string
	for _, msg := range req.Messages {
		if string(msg.Role) == "user" {
			firstUserMsg = msg.Content
			break
		}
	}
	if len(firstUserMsg) > 100 {
		firstUserMsg = firstUserMsg[:100] + "..."
	}
	return fmt.Sprintf("branch %s: %d messages covering %s", req.BranchID, len(req.Messages), firstUserMsg)
}

// extractSimpleResult extracts the first few words as a fallback.
func extractSimpleResult(text string) *SummarizeResult {
	words := strings.Fields(text)

	// Name: first significant word
	name := "chat"
	if len(words) > 0 {
		name = strings.ToLower(words[0])
		// Skip common short words
		if len(name) < 3 && len(words) > 1 {
			name = strings.ToLower(words[1])
		}
	}

	// Description: first few words
	maxWords := min(len(words), 6)
	desc := "new conversation"
	if maxWords > 0 {
		desc = strings.Join(words[:maxWords], " ")
		if len(words) > maxWords {
			desc += "..."
		}
		desc = strings.ToLower(desc)
	}

	return &SummarizeResult{
		Name:        name,
		Description: desc,
	}
}
