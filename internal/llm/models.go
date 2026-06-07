// Package llm provides LLM client functionality for OpenAI-compatible APIs.
package llm

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Role represents the role of a message in a conversation.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// ChatMessage represents a single message in a chat conversation.
type ChatMessage struct {
	Role       Role       `json:"role"`
	Content    string     `json:"content"`
	Name       string     `json:"name,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	// SummaryLevel tracks the hierarchical summarization depth for this
	// message. 0 = original, 1 = first-level summary, 2 = summary of
	// summaries, etc. Not serialized to external APIs.
	SummaryLevel int `json:"-"`
	// Critical marks a message that must never be dropped by the context
	// compressor. Critical messages are counted in QualityMetrics so callers
	// can verify retention. Not serialized to external APIs.
	Critical bool `json:"-"`
}

// ToOpenAIDict converts the message to the format expected by OpenAI API.
func (m *ChatMessage) ToOpenAIDict() map[string]any {
	msg := map[string]any{
		"role":    string(m.Role),
		"content": m.Content,
	}
	if m.Name != "" {
		msg["name"] = m.Name
	}
	if len(m.ToolCalls) > 0 {
		calls := make([]map[string]any, len(m.ToolCalls))
		for i, tc := range m.ToolCalls {
			calls[i] = tc.ToOpenAIDict()
		}
		msg["tool_calls"] = calls
	}
	if m.ToolCallID != "" {
		msg["tool_call_id"] = m.ToolCallID
	}
	return msg
}

// ToolCallFunction represents the function payload inside a tool call.
type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // Raw JSON string
}

// ToolCall represents a tool/function call returned by the model.
type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function ToolCallFunction `json:"function"`
}

// ToOpenAIDict converts the tool call to the format expected by OpenAI API.
func (tc *ToolCall) ToOpenAIDict() map[string]any {
	return map[string]any{
		"id":   tc.ID,
		"type": tc.Type,
		"function": map[string]any{
			"name":      tc.Function.Name,
			"arguments": tc.Function.Arguments,
		},
	}
}

// ParsedArguments parses the tool call arguments as a map.
func (tc *ToolCall) ParsedArguments() (map[string]any, error) {
	var args map[string]any
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		return nil, err
	}
	return args, nil
}

// TokenUsage represents token usage counters returned by the API.
type TokenUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	CachedTokens     int `json:"cached_tokens,omitempty"`
}

// Response represents a parsed response from the LLM API.
type Response struct {
	Content      string     `json:"content,omitempty"`
	ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
	Usage        TokenUsage `json:"usage"`
	Model        string     `json:"model"`
	FinishReason string     `json:"finish_reason"`
}

// HasToolCalls returns true if the response contains tool calls.
func (r *Response) HasToolCalls() bool {
	return len(r.ToolCalls) > 0
}

// ModelConfig holds configuration for a specific LLM model endpoint.
type ModelConfig struct {
	BaseURL              string
	ModelID              string
	APIKey               string
	CostPerMillionInput  float64
	CostPerMillionOutput float64
	MaxTokens            int
	Temperature          float64
	TopP                 float64
	FrequencyPenalty     float64
	PresencePenalty      float64
	StopSequences        []string
	ContextLimit         int
	Capabilities         map[string]bool
	ProviderID           string
}

// HasCapability checks if the model has a specific capability.
func (m *ModelConfig) HasCapability(capability string) bool {
	return m.Capabilities[capability]
}

// HasCapabilities checks if the model has all specified capabilities.
func (m *ModelConfig) HasCapabilities(caps []string) bool {
	for _, capName := range caps {
		if !m.Capabilities[capName] {
			return false
		}
	}
	return true
}

// TotalCost returns the total cost per million tokens (input + output).
func (m *ModelConfig) TotalCost() float64 {
	return m.CostPerMillionInput + m.CostPerMillionOutput
}

// AliasEntry holds the resolved models and configuration for an alias.
type AliasEntry struct {
	Models   []*ModelConfig // Ordered by priority (first = primary)
	Timeout  time.Duration  // Base cooldown timeout after failure
	MaxFails int            // Max consecutive failures before rotation
}

// AliasHealth tracks the health and rotation state of an alias.
type AliasHealth struct {
	CurrentIndex     int       // Which model in the rotation is currently active
	ConsecutiveFails int       // Number of consecutive failures on the current model
	LastFailure      time.Time // When the last failure occurred
	CooldownUntil    time.Time // Don't use the current model until this time
}

// ToolDefinition defines a tool/function for the LLM.
type ToolDefinition struct {
	Type     string      `json:"type"`
	Function FunctionDef `json:"function"`
}

// FunctionDef defines a function for tool use.
type FunctionDef struct {
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Parameters  FunctionParameters `json:"parameters"`
}

// FunctionParameters defines the parameters for a function.
type FunctionParameters struct {
	Type       string                       `json:"type"`
	Properties map[string]ParameterProperty `json:"properties"`
	Required   []string                     `json:"required,omitempty"`
}

// ParameterProperty defines a single parameter property.
type ParameterProperty struct {
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
}

// NewToolDefinition creates a new tool definition.
func NewToolDefinition(name, description string, params FunctionParameters) ToolDefinition {
	return ToolDefinition{
		Type: "function",
		Function: FunctionDef{
			Name:        name,
			Description: description,
			Parameters:  params,
		},
	}
}

// CountTokens returns the approximate token count for a tool definition.
// Uses the provided tokenizer if available, otherwise falls back to character-based heuristic.
func (t *ToolDefinition) CountTokens(tokenizer Tokenizer) int {
	if tokenizer == nil {
		// Fall back to heuristic: 3 chars/token
		tokenizer = &HeuristicTokenizer{}
	}

	// Count tokens in name
	tokens := tokenizer.CountTokens(t.Function.Name)

	// Count tokens in description
	tokens += tokenizer.CountTokens(t.Function.Description)

	// Count tokens in parameters structure
	tokens += 10 // "parameters" key + structural overhead
	tokens += tokenizer.CountTokens(t.Function.Parameters.Type)

	// Count tokens in each property
	for key, prop := range t.Function.Parameters.Properties {
		tokens += tokenizer.CountTokens(key)              // property name
		tokens += tokenizer.CountTokens(prop.Type)        // type
		tokens += tokenizer.CountTokens(prop.Description) // description
		tokens += 2                                       // structural overhead per property

		// Count enum values if present
		for _, enumVal := range prop.Enum {
			tokens += tokenizer.CountTokens(enumVal)
			tokens++ // structural overhead
		}
	}

	// Count required fields
	for _, req := range t.Function.Parameters.Required {
		tokens += tokenizer.CountTokens(req)
		tokens++ // structural overhead
	}

	// Add structural overhead for the tool definition itself
	tokens += 15 // "type", "function", braces, etc.

	return tokens
}

// CountToolDefinitionsTokens counts tokens for multiple tool definitions.
func CountToolDefinitionsTokens(tools []ToolDefinition, tokenizer Tokenizer) int {
	total := 0
	for _, tool := range tools {
		total += tool.CountTokens(tokenizer)
	}
	return total
}

// ChatRequest represents a request to the chat completions endpoint.
type ChatRequest struct {
	Model            string           `json:"model"`
	Messages         []map[string]any `json:"messages"`
	Temperature      float64          `json:"temperature,omitempty"`
	MaxTokens        int              `json:"max_tokens,omitempty"`
	TopP             float64          `json:"top_p,omitempty"`
	FrequencyPenalty float64          `json:"frequency_penalty,omitempty"`
	PresencePenalty  float64          `json:"presence_penalty,omitempty"`
	Stop             []string         `json:"stop,omitempty"`
	Tools            []ToolDefinition `json:"tools,omitempty"`
}

// ChatResponse represents the raw response from the chat completions endpoint.
type ChatResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   struct {
		PromptTokens        int `json:"prompt_tokens"`
		CompletionTokens    int `json:"completion_tokens"`
		TotalTokens         int `json:"total_tokens"`
		PromptTokensDetails struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	} `json:"usage"`
}

// Choice represents a single choice in the response.
type Choice struct {
	Index        int             `json:"index"`
	Message      ResponseMessage `json:"message"`
	FinishReason string          `json:"finish_reason"`
}

// ResponseMessage represents the message in a response choice.
// Content may be a string, null, or an array of content blocks
// (e.g., [{type: "text", text: "..."}]). We use json.RawMessage to
// handle all formats.
type ResponseMessage struct {
	Role      string          `json:"role"`
	Content   json.RawMessage `json:"content"`
	ToolCalls []RawToolCall   `json:"tool_calls,omitempty"`
}

// ContentString extracts the text content from the Content field,
// handling both plain string and array-of-blocks formats.
func (m *ResponseMessage) ContentString() string {
	if len(m.Content) == 0 {
		return ""
	}
	// Try plain string first
	var s string
	if err := json.Unmarshal(m.Content, &s); err == nil {
		return s
	}
	// Try array of content blocks: [{type: "text", text: "..."}]
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(m.Content, &blocks); err == nil {
		var sb strings.Builder
		first := true
		for _, b := range blocks {
			if b.Type != "text" {
				continue
			}
			if !first {
				sb.WriteString("\n")
			}
			sb.WriteString(b.Text)
			first = false
		}
		return sb.String()
	}
	// Fallback: return raw JSON as string
	return string(m.Content)
}

// RawToolCall represents the raw tool call from the API.
type RawToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// ToToolCall converts a RawToolCall to a ToolCall.
func (rtc *RawToolCall) ToToolCall() ToolCall {
	return ToolCall{
		ID:   rtc.ID,
		Type: rtc.Type,
		Function: ToolCallFunction{
			Name:      rtc.Function.Name,
			Arguments: rtc.Function.Arguments,
		},
	}
}

// SummaryExtract holds structured information extracted from a conversation
// during content-aware summarization. Instead of generic "role: content"
// concatenation, the summarizer produces this structured representation so
// downstream consumers can query decisions, file paths, open questions, etc.
type SummaryExtract struct {
	Decisions           []string `json:"decisions"`   // Key decisions made
	FilePaths           []string `json:"file_paths"`  // Files referenced/modified
	UnresolvedQuestions []string `json:"unresolved"`  // Open questions remaining
	TaskState           string   `json:"task_state"`  // Current task status
	KeyFindings         []string `json:"findings"`    // Important discoveries
	FileReads           []string `json:"file_reads"`  // Files read (compaction)
	FileWrites          []string `json:"file_writes"` // Files written (compaction)
	FileEdits           []string `json:"file_edits"`  // Files edited (compaction)
	ErrorsEncountered   []string `json:"errors"`      // Errors encountered (compaction)
}

// DeltaCallback is invoked for each content chunk during a streaming response.
type DeltaCallback func(delta string) error

// StreamAbortedError indicates that a TTSR rule triggered mid-stream,
// requiring the caller to retry with the rule content injected.
type StreamAbortedError struct {
	RuleName string
	RuleBody string
	Reason   string
}

func (e *StreamAbortedError) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("stream aborted by rule %q: %s", e.RuleName, e.Reason)
	}
	return fmt.Sprintf("stream aborted by rule %q", e.RuleName)
}

// Ptr returns a pointer to the given value.
func Ptr[T any](v T) *T { return &v }

// DerefOr returns the dereferenced value of p, or def if p is nil.
func DerefOr[T any](p *T, def T) T {
	if p == nil {
		return def
	}
	return *p
}
