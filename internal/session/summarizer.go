package session

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

// defaultPromptTruncationLimit is the default max length (in bytes) of the
// user prompt sent to the LLM before it gets truncated.
const defaultPromptTruncationLimit = 8000

// Summarizer generates concise session descriptions using an LLM.
type Summarizer struct {
	llmClient             *llm.Client
	logger                *slog.Logger
	promptTruncationLimit int
}

// SummarizerOption configures a Summarizer constructed via NewSummarizer.
type SummarizerOption func(*Summarizer)

// WithPromptTruncationLimit overrides the maximum user-prompt length (in bytes)
// before truncation is applied. Only takes effect when limit > 0.
func WithPromptTruncationLimit(limit int) SummarizerOption {
	return func(s *Summarizer) {
		if limit > 0 {
			s.promptTruncationLimit = limit
		}
	}
}

// NewSummarizer creates a new session summarizer.
func NewSummarizer(llmClient *llm.Client, logger *slog.Logger, opts ...SummarizerOption) *Summarizer {
	if logger == nil {
		logger = slog.Default()
	}
	s := &Summarizer{
		llmClient:             llmClient,
		logger:                logger,
		promptTruncationLimit: defaultPromptTruncationLimit,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}
	return s
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

	// Parse JSON response and clean up
	name, desc := cleanTitleResult(resp.Content,
		extractSimpleResult(req.FirstMessage).Name,
		extractSimpleResult(req.FirstMessage).Description,
	)

	s.logger.Debug("Generated session summary", "name", name, "description", desc)
	return &SummarizeResult{Name: name, Description: desc}, nil
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
	if len(userPrompt) > s.promptTruncationLimit {
		userPrompt = userPrompt[:s.promptTruncationLimit] + "\n... (truncated)"
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

// extractSimpleResult extracts a session name and description from text.
// Name: first non-filler word (or first word if all are fillers)
// Description: lowercased text, truncated to 60 chars with "..." if needed.
func extractSimpleResult(text string) *SummarizeResult {
	words := strings.Fields(text)

	// Default result for empty input
	if len(words) == 0 {
		return &SummarizeResult{
			Name:        "chat",
			Description: "new conversation",
		}
	}

	// Fillers to skip when extracting the name
	fillers := map[string]bool{
		"a": true, "an": true, "the": true,
		"is": true, "are": true, "was": true, "were": true,
		"i": true, "you": true, "we": true, "they": true,
		"my": true, "your": true, "our": true, "their": true,
		"what": true, "when": true, "where": true, "why": true, "how": true,
		"do": true, "does": true, "did": true, "will": true, "would": true,
		"can": true, "could": true, "should": true, "may": true, "might": true,
		"for": true, "to": true, "from": true, "in": true, "on": true, "at": true,
		"and": true, "or": true, "but": true, "of": true, "with": true,
		"me": true, "us": true, "it": true, "this": true, "that": true,
		"tell": true, "show": true, "give": true, "get": true, "got": true,
		"have": true, "had": true, "been": true, "being": true, "be": true,
	}

	// Find first non-filler word(s) for the name
	name := strings.ToLower(words[0])
	nonFillerIdx := -1
	for i, w := range words {
		if !fillers[strings.ToLower(w)] {
			nonFillerIdx = i
			break
		}
	}
	if nonFillerIdx >= 0 {
		// Take 1-2 non-filler words for the name
		if nonFillerIdx+1 < len(words) && !fillers[strings.ToLower(words[nonFillerIdx+1])] {
			name = strings.ToLower(strings.Join(words[nonFillerIdx:nonFillerIdx+2], " "))
		} else {
			name = strings.ToLower(words[nonFillerIdx])
		}
	}

	// Strip punctuation from name
	name = strings.Trim(name, ".,!?;:\"'()[]{}")

	// Description: lowercased text, truncated
	desc := strings.ToLower(strings.Join(words, " "))
	if len(desc) > 60 {
		desc = desc[:57] + "..."
	}

	return &SummarizeResult{
		Name:        name,
		Description: desc,
	}
}
