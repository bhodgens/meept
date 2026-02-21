package session

import (
	"context"
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

// GenerateDescription creates a concise session description from the first message.
// Returns a description like "personal: toenail fungus" or "meept: debug job queue".
func (s *Summarizer) GenerateDescription(ctx context.Context, req SummarizeRequest) (string, error) {
	s.logger.Info("Summarizer.GenerateDescription called",
		"first_message_len", len(req.FirstMessage),
		"project_name", req.ProjectName,
		"has_llm_client", s.llmClient != nil,
	)

	if s.llmClient == nil {
		s.logger.Warn("No LLM client available for summarization, using simple extraction")
		// Fallback to simple extraction if no LLM available
		return extractSimple(req.FirstMessage), nil
	}

	// Build the summarization prompt
	systemPrompt := `You are a session summarizer. Generate a very brief description (3-8 words) of what this conversation is about.

Format: "category: brief description"

Categories to use:
- "personal" - health, relationships, life questions, personal matters
- "coding" - programming, debugging, code review, software development
- "research" - learning, information gathering, explanations
- "task" - todo lists, planning, organization
- "creative" - writing, art, brainstorming, ideas
- "system" - system administration, devops, infrastructure
- Use the project name if discussing a specific codebase

Examples:
- "personal: remedy for headaches"
- "coding: fix null pointer in auth"
- "myproject: add user authentication"
- "research: kubernetes networking"
- "task: plan vacation itinerary"
- "creative: story ideas for scifi"

Be extremely concise. No punctuation at the end. All lowercase.`

	userPrompt := req.FirstMessage
	if req.ProjectName != "" {
		userPrompt = fmt.Sprintf("[Project: %s]\n\n%s", req.ProjectName, req.FirstMessage)
	}

	// Create minimal message for summarization
	messages := []llm.ChatMessage{
		{Role: llm.RoleSystem, Content: systemPrompt},
		{Role: llm.RoleUser, Content: userPrompt},
	}

	// Use a short timeout for summarization
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	s.logger.Debug("Sending summarization request to LLM",
		"system_prompt_len", len(systemPrompt),
		"user_prompt_len", len(userPrompt),
	)

	// Request with minimal tokens
	resp, err := s.llmClient.Chat(ctx, messages,
		llm.WithMaxTokens(50),
		llm.WithTemperature(0.3),
	)
	if err != nil {
		s.logger.Warn("LLM summarization request failed, using fallback", "error", err)
		return extractSimple(req.FirstMessage), nil
	}

	s.logger.Debug("LLM summarization response received",
		"raw_response", resp.Content,
	)

	// Clean up the response
	desc := strings.TrimSpace(resp.Content)
	desc = strings.ToLower(desc)
	desc = strings.TrimSuffix(desc, ".")

	// Validate it's not too long
	if len(desc) > 60 {
		desc = desc[:60] + "..."
	}

	s.logger.Debug("Generated session description", "description", desc)
	return desc, nil
}

// extractSimple extracts the first few words as a fallback.
func extractSimple(text string) string {
	words := strings.Fields(text)
	maxWords := 6
	if len(words) < maxWords {
		maxWords = len(words)
	}
	if maxWords == 0 {
		return "new conversation"
	}
	desc := strings.Join(words[:maxWords], " ")
	if len(words) > maxWords {
		desc += "..."
	}
	return strings.ToLower(desc)
}
