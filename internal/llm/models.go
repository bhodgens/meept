// Package llm provides LLM client functionality for OpenAI-compatible APIs.
package llm

import (
	"encoding/json"
	"fmt"
	"net/url"
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
	Role       Role          `json:"role"`
	Content    string        `json:"content"`
	Parts      []ContentPart `json:"parts,omitempty"` // Non-empty => takes precedence for LLM serialization
	Name       string        `json:"name,omitempty"`
	ToolCalls  []ToolCall    `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
	// IsToolError indicates that this tool-role message represents a failed
	// tool execution. Used by the Anthropic client to set the IsError flag
	// on tool_result blocks. Not serialized to external APIs (set per-trip).
	IsToolError bool `json:"-"`
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
// It is preserved for backward compatibility; callers with access to an
// UploadStore should use ToOpenAIDictWithStore so that file:// image URLs
// are resolved to data: URLs before serialization.
func (m *ChatMessage) ToOpenAIDict() map[string]any {
	return m.ToOpenAIDictWithStore(nil)
}

// ToOpenAIDictWithStore is like ToOpenAIDict but resolves file:// image URLs
// to data: URLs using the provided upload store. A nil store preserves the
// legacy behavior (URLs pass through verbatim), matching the previous
// ToOpenAIDict semantics for callers that have not been wired up yet.
func (m *ChatMessage) ToOpenAIDictWithStore(store UploadStore) map[string]any {
	msg := map[string]any{
		"role": string(m.Role),
	}
	if len(m.Parts) > 0 {
		content := make([]map[string]any, 0, len(m.Parts))
		for _, p := range m.Parts {
			switch p.Type {
			case "text":
				content = append(content, map[string]any{
					"type": "text",
					"text": p.Text,
				})
			case "image_url":
				if p.ImageURL == nil {
					continue
				}
				if p.ImageURL.Description != "" {
					content = append(content, map[string]any{
						"type": "text",
						"text": fmt.Sprintf("[image: %s]", p.ImageURL.Description),
					})
				} else {
					url := p.ImageURL.URL
					if store != nil {
						resolved, err := resolveImageURL(url, store)
						if err != nil {
							// Fall back to a text placeholder so the request
							// still succeeds, mirroring the Anthropic path
							// (anthropic.go:708-711).
							content = append(content, map[string]any{
								"type": "text",
								"text": fmt.Sprintf("[image: unable to load %s]", url),
							})
							continue
						}
						url = resolved
					}
					content = append(content, map[string]any{
						"type": "image_url",
						"image_url": map[string]any{
							"url": url,
						},
					})
				}
			}
		}
		msg["content"] = content
	} else if m.Content != "" {
		msg["content"] = m.Content
	} else if m.Role != RoleAssistant || len(m.ToolCalls) == 0 {
		// Emit null for empty content only when the message isn't an
		// assistant-with-tool-calls (those omit content per OpenAI spec).
		// GLM/Qwen reject assistant messages with content="" + tool_calls
		// (HTTP 400, error 1214: "messages parameter is illegal").
		msg["content"] = nil
	}
	if m.Name != "" {
		msg["name"] = m.Name
	}
	// Tool calls are only valid on assistant messages (calling a tool).
	// Tool response messages (role="tool") should NOT include tool_calls -
	// they include tool_call_id to match the original call.
	// See: https://platform.openai.com/docs/api-reference/chat/create
	if len(m.ToolCalls) > 0 && m.Role == RoleAssistant {
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
	// Reasoning holds the assistant's chain-of-thought text when the
	// vendor exposes it as a separate channel (Anthropic thinking,
	// OpenAI o1-style reasoning, DeepSeek reasons). Empty when not
	// surfaced by the provider.
	Reasoning string `json:"reasoning,omitempty"`
}

// HasToolCalls returns true if the response contains tool calls.
func (r *Response) HasToolCalls() bool {
	return len(r.ToolCalls) > 0
}

// PromptCacheConfig controls prompt caching behavior for providers that
// support it (e.g. Anthropic's cache_control markers).
type PromptCacheConfig struct {
	// Enabled controls whether prompt cache blocks are emitted. Defaults to
	// true when the struct is zero-valued (use IsEnabled to check).
	Enabled *bool `json:"enabled" yaml:"enabled"`
}

// IsEnabled reports whether prompt caching is active. A nil Enabled pointer
// means "default true".
func (p *PromptCacheConfig) IsEnabled() bool {
	return p == nil || p.Enabled == nil || *p.Enabled
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
	// ToolConstraint declares the grammar-constraint wire mode this
	// endpoint supports for tool calls: "llamacpp", "vllm", or
	// "json_schema". Empty means no constraint support (no grammar is
	// ever attached). See internal/llm/gbnf.go.
	ToolConstraint string
	// SchemaMode is the resolved tool-schema mode for this endpoint
	// ("full"|"indexed", loop-economics leaf 02). Empty means no
	// model/provider-level override; the effective mode falls back to the
	// global [agent.tools].schema_mode (default "indexed") via
	// Resolver.EffectiveSchemaMode.
	SchemaMode string
	// OAuthProvider identifies the OAuth provider (e.g. "github-models",
	// "google-oauth") whose token should be used in place of a static API
	// key. When non-empty, the LLM client resolves a fresh access token
	// from the token store before each request.
	OAuthProvider string
	// ExtraHeaders are additional HTTP headers sent with every request.
	// For example, GitHub Models requires X-GitHub-Api-Version.
	ExtraHeaders map[string]string
	// Timeout is the per-request timeout in seconds.
	// When 0, the default timeout (120s) is used.
	Timeout time.Duration
	// MaxConcurrency is the maximum number of concurrent requests allowed
	// to this model/provider. When 0, no limit is enforced (unlimited).
	// Use this to prevent overwhelming rate-limited APIs or local LLMs.
	MaxConcurrency int
	// ConfiguredTimeout records whether the ORIGINATING config declared a
	// nonzero timeout: for this alias (tree 02 leaf 04, DECISIONS.md D10).
	// It is set for every model built from that alias's entry and mirrors
	// the alias-level flag; the alias's Timeout field alone cannot carry
	// the distinction because NewResolver substitutes the 30s default.
	ConfiguredTimeout bool
	// DefaultReasoning is the model-level default reasoning effort/budget
	// configuration. When non-nil, it is used if no per-request or agent-level
	// reasoning override is present.
	DefaultReasoning *ReasoningConfig
	// PromptCache controls prompt caching behavior. When nil, caching is
	// enabled by default.
	PromptCache *PromptCacheConfig
	// ProviderAPI is the provider-level api field (openai, comfyui, gemini, …).
	ProviderAPI string
	// CatalogRef is "provider/map-key" as written in models.json5.
	CatalogRef string
	// GenerationAPI is an optional per-model transport override.
	GenerationAPI string
	// Workflow is a ComfyUI API-format workflow path.
	Workflow string
	// GenerationURL is a full URL for kind=http models.
	GenerationURL string
	// BodyTemplate is the JSON body template for kind=http models.
	BodyTemplate map[string]any
	// ResponseURLPath / ResponseB64Path extract the asset from an http response.
	ResponseURLPath string
	ResponseB64Path string
	ImageApp        string
	VideoApp        string
}

// EndpointKey returns the cooldown identity for a model's base endpoint:
// endpoint URL host + credential fingerprint (QuotaCredentialKey's provider
// portion; audit R2). Host-only keys are wrong in this repo's config
// practice — gala-mlx and gala-llama share one host while xai (API key) and
// xai-oauth (subscription) share api.x.ai with unrelated credentials — so
// models share timeout fate ONLY when both host and credential match
// (DECISIONS.md D10). A nil or hostless config falls back to a stable,
// non-empty key derived from the credential alone.
func EndpointKey(cfg *ModelConfig) string {
	host := ""
	if cfg != nil {
		host = endpointHost(cfg.BaseURL)
	}
	cred := QuotaCredentialKey(endpointKeyProviderID(cfg), cfg)
	return host + "|" + cred
}

// endpointHost extracts the URL host from a base URL, tolerating configs
// that omit a scheme. Empty input maps to "" so the fallback key stays
// stable.
func endpointHost(baseURL string) string {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return ""
	}
	if !strings.Contains(baseURL, "://") {
		baseURL = "http://" + baseURL
	}
	if u, err := url.Parse(baseURL); err == nil {
		return strings.ToLower(u.Host)
	}
	return ""
}

// endpointKeyProviderID names the credential scope for the endpoint key.
// The QuotaCredentialKey provider portion doubles as the scope so models
// from DIFFERENT providers never share an endpoint key even when they
// point at the same host.
func endpointKeyProviderID(cfg *ModelConfig) string {
	if cfg == nil {
		return ""
	}
	return cfg.ProviderID
}

// HasCapability checks if the model has a specific capability.
func (m *ModelConfig) HasCapability(capability string) bool {
	return m.Capabilities[capability]
}

// GenerationTransport returns the image/video backend for this model.
// Model-level api wins. Else provider api if it is a generation transport.
// Else infer openai_images / openai_videos from capabilities.
func (m *ModelConfig) GenerationTransport() string {
	if m == nil {
		return ""
	}
	if m.GenerationAPI != "" {
		return m.GenerationAPI
	}
	switch m.ProviderAPI {
	case "openai_images", "openai_videos", "gemini", "comfyui", "infsh", "http":
		return m.ProviderAPI
	}
	if m.HasCapability(CapVideoGen) && !m.HasCapability(CapImageGen) {
		return "openai_videos"
	}
	if m.HasCapability(CapImageGen) {
		return "openai_images"
	}
	if m.HasCapability(CapVideoGen) {
		return "openai_videos"
	}
	return ""
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
	Models                 []*ModelConfig // Ordered by priority (first = primary)
	Timeout                time.Duration  // Base cooldown timeout after failure
	MaxFails               int            // Max consecutive failures before rotation
	DefaultModel           string         // Optional: revert to this model after cooldown
	BalancedStickyRequests bool           // Optional: pin callers to single model
	// timeoutArmed records that the originating CONFIG declared a nonzero
	// timeout: (tree 02 leaf 04, DECISIONS.md D10). Alias-level blocks arm
	// only when true — a Timeout value substituted as the 30s default must
	// never look like an explicit opt-in.
	timeoutArmed bool
}

// AliasHealth tracks the health and rotation state of an alias.
//
// Locking convention (issue #29): Resolver.mu is the SINGLE lock guarding
// every field of this struct. Do NOT add a second mutex here — new fields
// join the Resolver.mu regime, and helper methods on this type must be
// documented "callers must hold Resolver.mu" and only be called with it
// held.
type AliasHealth struct {
	CurrentIndex     int
	ConsecutiveFails int
	LastFailure      time.Time
	CooldownUntil    time.Time
	StickyPins       map[string]int
	// RevertAt arms default-model reversion: when non-zero, rotation reverts
	// to AliasEntry.DefaultModel after this deadline (armed by
	// RecordAliasFailure when default_model is configured).
	RevertAt time.Time
	// FailedProviderID/FailedModelID identify the model that most recently
	// failed (empty = no known failure). Sticky pins are matched against
	// this IDENTITY — not a rotation index — so interleaved resolves for
	// other models cannot misattribute the failure (issue #30).
	FailedProviderID string
	FailedModelID    string
	// entryBlocks maps provider/model ID pairs to their quota unblock time.
	// Zero means not blocked.
	entryBlocks map[string]time.Time
	// credentialBlock maps credential keys to their quota unblock time.
	// Blocking a credential blocks all models sharing that key.
	credentialBlock map[string]time.Time
	// Alias-level explicit-timeout state (tree 02 leaf 04, DECISIONS.md
	// D10). TimeoutArmed is true only when the alias CONFIG declared a
	// nonzero timeout: — without it these fields stay zero forever and no
	// alias-level block ever applies. TimeoutStreak counts CONSECUTIVE
	// failures of the SAME member model (FailedProviderID/FailedModelID
	// identity); TimeoutBlockUntil is the current alias-block deadline
	// (zero = not blocked); TimeoutBlocks counts blocks ARMED so far and
	// drives the incremental doubling ladder (1x, 2x, 4x base — capped).
	TimeoutArmed      bool
	TimeoutStreak     int
	TimeoutBlockUntil time.Time
	TimeoutBlocks     int
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
	Type        string             `json:"type"`
	Description string             `json:"description,omitempty"`
	Enum        []string           `json:"enum,omitempty"`
	Items       *ParameterProperty `json:"items,omitempty"`
	// Properties and Required support nested object schemas (used by the
	// GBNF/json-schema tool-call converters). Absent for non-object props.
	Properties map[string]ParameterProperty `json:"properties,omitempty"`
	Required   []string                     `json:"required,omitempty"`
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
	// ReasoningContent captures chain-of-thought text from OpenAI-compat
	// providers that surface it as a sibling field to `content`.
	ReasoningContent string `json:"reasoning_content,omitempty"`
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
		// Note: Non-text blocks (tool_use, image) are intentionally skipped.
		// Tool calls are handled separately via msg.ToolCalls - dual-path design by convention.
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
//
//go:fix inline
func Ptr[T any](v T) *T { return new(v) }

// DerefOr returns the dereferenced value of p, or def if p is nil.
func DerefOr[T any](p *T, def T) T {
	if p == nil {
		return def
	}
	return *p
}
