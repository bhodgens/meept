// Package llm provides LLM client functionality for OpenAI-compatible APIs.
package llm

import (
	"encoding/json"
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
	Role       Role        `json:"role"`
	Content    string      `json:"content"`
	Name       string      `json:"name,omitempty"`
	ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`
	ToolCallID string      `json:"tool_call_id,omitempty"`
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
}

// Response represents a parsed response from the LLM API.
type Response struct {
	Content      string      `json:"content,omitempty"`
	ToolCalls    []ToolCall  `json:"tool_calls,omitempty"`
	Usage        TokenUsage  `json:"usage"`
	Model        string      `json:"model"`
	FinishReason string      `json:"finish_reason"`
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
func (m *ModelConfig) HasCapability(cap string) bool {
	return m.Capabilities[cap]
}

// HasCapabilities checks if the model has all specified capabilities.
func (m *ModelConfig) HasCapabilities(caps []string) bool {
	for _, cap := range caps {
		if !m.Capabilities[cap] {
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
	CurrentIndex       int       // Which model in the rotation is currently active
	ConsecutiveFails   int       // Number of consecutive failures on the current model
	LastFailure        time.Time // When the last failure occurred
	CooldownUntil      time.Time // Don't use the current model until this time
}

// ToolDefinition defines a tool/function for the LLM.
type ToolDefinition struct {
	Type     string         `json:"type"`
	Function FunctionDef    `json:"function"`
}

// FunctionDef defines a function for tool use.
type FunctionDef struct {
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Parameters  FunctionParameters  `json:"parameters"`
}

// FunctionParameters defines the parameters for a function.
type FunctionParameters struct {
	Type       string                        `json:"type"`
	Properties map[string]ParameterProperty  `json:"properties"`
	Required   []string                      `json:"required,omitempty"`
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
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// Choice represents a single choice in the response.
type Choice struct {
	Index        int             `json:"index"`
	Message      ResponseMessage `json:"message"`
	FinishReason string          `json:"finish_reason"`
}

// ResponseMessage represents the message in a response choice.
type ResponseMessage struct {
	Role      string          `json:"role"`
	Content   *string         `json:"content"`
	ToolCalls []RawToolCall   `json:"tool_calls,omitempty"`
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

// Ptr returns a pointer to the given value.
func Ptr[T any](v T) *T { return &v }

// DerefOr returns the dereferenced value of p, or def if p is nil.
func DerefOr[T any](p *T, def T) T {
	if p == nil {
		return def
	}
	return *p
}
