package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Cloudflare-required headers for the ChatGPT Codex backend, mirroring the
// best-known-working codex-rs CLI contract. Sources:
//   - User-Agent shape: openai/codex codex-rs/login/src/auth/default_client.rs
//     get_codex_user_agent(): "<originator>/<version> (<os type> <os version>;
//     <arch>) <terminal>" — upstream's own macOS test asserts
//     ^codex_cli_rs/\d+\.\d+\.\d+ \(Mac OS \d+\.\d+\.\d+; (x86_64|arm64)\) (\S+)$
//   - OpenAI-Beta + session_id: openai/codex commit 6f2b01b (core request
//     builder: .header("OpenAI-Beta", "responses=experimental")).
const (
	// codexCLIVersion is the codex-rs release whose wire fingerprint this
	// client mirrors: rust-v0.153.2 (github.com/openai/codex/releases,
	// 2026-09-03, latest stable at time of writing). Upstream has rejected
	// placeholder versions such as 0.0.0, so this must track a real release.
	codexCLIVersion = "0.153.2"

	codexOriginator  = "codex_cli_rs"
	codexAuthClaimNS = "https://api.openai.com/auth"

	// codexResponsesBeta is the value codex-rs sends in its OpenAI-Beta
	// request header on every /responses call.
	codexResponsesBeta = "responses=experimental"

	// codexUserAgentProduct fills the trailing <terminal> slot of the
	// codex-rs User-Agent (upstream puts the detected terminal, e.g.
	// "iTerm2"). meept is headless, so it identifies itself here.
	codexUserAgentProduct = "meept"
)

// codexUserAgent is the default User-Agent sent with every Codex request,
// computed once at init via codexClientUserAgent. It is a var (not a const)
// only so tests can pin the header value verbatim.
var codexUserAgent = codexClientUserAgent()

// codexUAInvocations counts codexClientUserAgent calls; the loopback tests
// use the delta to prove the UA is computed once, not per request.
var codexUAInvocations atomic.Uint64

// codexOSVersion is the sanitized OS version used in the User-Agent
// (lazy: the uname syscall result is cached for process lifetime).
var codexOSVersion = sync.OnceValue(func() string {
	return sanitizeOSVersion(codexOSVersionRaw())
})

// codexClientUserAgent builds the codex-rs-shaped User-Agent:
//
//	codex_cli_rs/<version> (<os type> <os version>; <arch>) <terminal>
//
// e.g. "codex_cli_rs/0.153.2 (Mac OS 26.5.2; arm64) meept". The version is
// the pinned codexCLIVersion (a real upstream release), the OS type/version
// and arch mirror the os_info-derived shape codex-rs emits, and the
// trailing token identifies the client.
func codexClientUserAgent() string {
	codexUAInvocations.Add(1)
	return fmt.Sprintf("%s/%s (%s %s; %s) %s",
		codexOriginator, codexCLIVersion,
		codexOSFamily(), codexOSVersion(),
		codexArch(), codexUserAgentProduct,
	)
}

// codexOSFamily maps runtime.GOOS onto the OS-type label the codex-rs
// os_info crate emits (macOS hosts report "Mac OS").
func codexOSFamily() string {
	switch runtime.GOOS {
	case "darwin":
		return "Mac OS"
	case "windows":
		return "Windows"
	default:
		return "Linux"
	}
}

// codexArch maps runtime.GOARCH onto the architecture labels codex-rs
// emits ("x86_64", not Go's "amd64").
func codexArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x86_64"
	case "386":
		return "x86"
	default:
		return runtime.GOARCH
	}
}

// sanitizeOSVersion keeps the leading dotted-numeric prefix of an OS
// version string ("26.5.2" → "26.5.2", "6.12.34-1-MANJARO" → "6.12.34",
// ".5.2" or "garbage" → "0.0.0") so the value always matches the
// \d+(\.\d+)* shape upstream expects.
func sanitizeOSVersion(s string) string {
	if s == "" || s[0] < '0' || s[0] > '9' {
		return "0.0.0"
	}
	end := 1
	for end < len(s) && (s[end] >= '0' && s[end] <= '9' || s[end] == '.') {
		end++
	}
	return s[:end]
}

// codexDefaultTimeout is the per-request HTTP timeout for Codex calls.
const codexDefaultTimeout = 120 * time.Second

// codexAccountID extracts the ChatGPT account ID from the ACCESS TOKEN JWT.
// The claim lives at claims["https://api.openai.com/auth"]["chatgpt_account_id"].
// Any parse failure — malformed token, undecodable payload, missing claim —
// returns "" so the caller can silently omit the ChatGPT-Account-ID header.
func codexAccountID(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		// Some issuers emit padded standard-encoded segments; retry with
		// StdEncoding (DecodeString handles the padding).
		payload, err = base64.StdEncoding.DecodeString(parts[1])
		if err != nil {
			return ""
		}
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	auth, ok := claims[codexAuthClaimNS].(map[string]any)
	if !ok {
		return ""
	}
	id, ok := auth["chatgpt_account_id"].(string)
	if !ok {
		return ""
	}
	return id
}

// CodexClient talks to the ChatGPT Codex backend (Responses API dialect).
// It implements Chatter using the non-streaming /responses endpoint with
// the Cloudflare-required client headers.
type CodexClient struct {
	configMu      sync.RWMutex
	config        *ModelConfig
	budget        *Budget
	httpClient    *http.Client
	logger        *slog.Logger
	tokenResolver TokenResolver
	oauthProvider string
}

// CodexClientOption is a functional option for NewCodexClient.
type CodexClientOption func(*CodexClient)

// WithCodexLogger sets the client logger.
func WithCodexLogger(l *slog.Logger) CodexClientOption {
	return func(c *CodexClient) {
		if l != nil {
			c.logger = l
		}
	}
}

// WithCodexBudget attaches a token budget for usage recording.
func WithCodexBudget(b *Budget) CodexClientOption {
	return func(c *CodexClient) {
		if b != nil {
			c.budget = b
		}
	}
}

// WithCodexTimeout sets the per-request HTTP timeout.
func WithCodexTimeout(d time.Duration) CodexClientOption {
	return func(c *CodexClient) {
		if d > 0 {
			c.httpClient.Timeout = d
		}
	}
}

// WithCodexTokenResolver wires an OAuth token resolver. A nil resolver is
// ignored so callers can pass through unset values unconditionally.
func WithCodexTokenResolver(tr TokenResolver, provider string) CodexClientOption {
	return func(c *CodexClient) {
		if tr != nil {
			c.tokenResolver = tr
			c.oauthProvider = provider
		}
	}
}

// NewCodexClient creates a CodexClient with defaults: 120s HTTP timeout,
// slog.Default() logger.
func NewCodexClient(cfg *ModelConfig, opts ...CodexClientOption) *CodexClient {
	if cfg == nil {
		cfg = &ModelConfig{}
	}
	c := &CodexClient{
		config: cfg,
		httpClient: &http.Client{
			Timeout: codexDefaultTimeout,
		},
		logger: slog.Default(),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Config returns the current model configuration.
func (c *CodexClient) Config() *ModelConfig {
	c.configMu.RLock()
	defer c.configMu.RUnlock()
	return c.config
}

// codexResponsesPayload is the wire shape of the non-streaming /responses call.
type codexResponsesPayload struct {
	Model        string           `json:"model"`
	Input        string           `json:"input"`
	Instructions string           `json:"instructions,omitempty"`
	Stream       bool             `json:"stream"`
	Store        bool             `json:"store"`
	Tools        []map[string]any `json:"tools,omitempty"`
	ToolChoice   string           `json:"tool_choice,omitempty"`
	MaxTokens    int              `json:"max_output_tokens,omitempty"`
}

// codexResponsesResponse is the subset of the /responses response we parse.
type codexResponsesResponse struct {
	Output []struct {
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Summary []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"summary"`
		CallID    string `json:"call_id"`
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"output"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// Chat sends a non-streaming Responses request and returns the parsed Response.
func (c *CodexClient) Chat(ctx context.Context, messages []ChatMessage, opts ...ChatOption) (*Response, error) {
	c.configMu.RLock()
	cfg := c.config
	c.configMu.RUnlock()

	// Apply ChatOption funcs onto the shared chatOptions struct (same
	// package), seeded with config defaults like buildChatRequest does.
	chatOpts := &chatOptions{
		temperature: cfg.Temperature,
		maxTokens:   cfg.MaxTokens,
		topP:        cfg.TopP,
	}
	for _, opt := range opts {
		opt(chatOpts)
	}

	payload := c.buildPayload(messages, cfg, chatOpts)
	resp, err := c.doRequest(ctx, payload, cfg)
	if err != nil {
		return nil, err
	}

	// Record usage against the budget (mutex-free; Budget locks internally).
	if c.budget != nil {
		c.budget.RecordUsageWithScope(resp.Usage, chatOpts.taskID, chatOpts.sessionID)
		if cfg.CostPerMillionInput > 0 || cfg.CostPerMillionOutput > 0 {
			costUSD := float64(resp.Usage.PromptTokens)*cfg.CostPerMillionInput/1_000_000 +
				float64(resp.Usage.CompletionTokens)*cfg.CostPerMillionOutput/1_000_000
			if costUSD > 0 {
				c.budget.RecordCostWithScope(CostRecord{
					Timestamp:        time.Now(),
					CostUSD:          costUSD,
					PromptTokens:     resp.Usage.PromptTokens,
					CompletionTokens: resp.Usage.CompletionTokens,
				}, chatOpts.taskID, chatOpts.sessionID)
			}
		}
	}
	return resp, nil
}

// ChatWithProgress behaves like Chat, reporting start/done progress stages.
// The Responses call is non-streaming, so intermediate stages are not
// available; the callback receives ProgressStageStarting before the request
// and ProgressStageDone after a successful parse.
func (c *CodexClient) ChatWithProgress(ctx context.Context, messages []ChatMessage, progress ProgressCallback, opts ...ChatOption) (*Response, error) {
	report := func(stage ProgressStage, detail string) {
		if progress == nil {
			return
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					c.logger.Warn("Progress callback panicked", "stage", stage, "panic", r)
				}
			}()
			progress(stage, detail)
		}()
	}
	report(ProgressStageStarting, "Sending Codex request")
	resp, err := c.Chat(ctx, messages, opts...)
	if err != nil {
		return nil, err
	}
	report(ProgressStageDone, "Response complete")
	return resp, nil
}

// buildPayload converts ChatMessages into the Codex Responses payload:
// last user message → input, concatenated system messages → instructions,
// tools in the flat Responses shape (name/description/parameters at top
// level, not nested under "function").
func (c *CodexClient) buildPayload(messages []ChatMessage, cfg *ModelConfig, chatOpts *chatOptions) *codexResponsesPayload {
	payload := &codexResponsesPayload{
		Model:  cfg.ModelID,
		Stream: false,
		Store:  false,
	}

	var systemParts []string
	inputText := ""
	for _, msg := range messages {
		switch msg.Role {
		case RoleSystem:
			if msg.Content != "" {
				systemParts = append(systemParts, msg.Content)
			}
		case RoleUser:
			if msg.Content != "" {
				inputText = msg.Content
			}
			// Parts (multimodal) take precedence for serialization; extract
			// text parts since Responses input here is plain text.
			if len(msg.Parts) > 0 {
				var textParts []string
				for _, p := range msg.Parts {
					if p.Type == ContentTypeText && p.Text != "" {
						textParts = append(textParts, p.Text)
					}
				}
				if len(textParts) > 0 {
					inputText = strings.Join(textParts, "\n")
				}
			}
		}
	}
	payload.Input = inputText
	payload.Instructions = strings.Join(systemParts, "\n\n")

	if len(chatOpts.tools) > 0 {
		tools := make([]map[string]any, len(chatOpts.tools))
		for i, td := range chatOpts.tools {
			tool := map[string]any{
				"type": "function",
			}
			if td.Type != "" {
				tool["type"] = td.Type
			}
			tool["name"] = td.Function.Name
			if td.Function.Description != "" {
				tool["description"] = td.Function.Description
			}
			tool["parameters"] = td.Function.Parameters
			tools[i] = tool
		}
		payload.Tools = tools
		payload.ToolChoice = "auto"
	}

	if chatOpts.maxTokens > 0 {
		payload.MaxTokens = chatOpts.maxTokens
	}

	return payload
}

// doRequest sends the payload and parses the response. cfg must be captured
// under lock by the caller.
func (c *CodexClient) doRequest(ctx context.Context, payload *codexResponsesPayload, cfg *ModelConfig) (*Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, &ClientError{Message: "failed to marshal request", Cause: err}
	}

	url := strings.TrimSuffix(cfg.BaseURL, "/") + "/responses"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, &ClientError{Message: "failed to create request", Cause: err}
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", codexUserAgent)
	req.Header.Set("originator", codexOriginator)
	// Session affinity: codex-rs sends its per-session UUID in a `session_id`
	// HEADER (openai/codex commit 6f2b01b). The ${session_id} substitution
	// sentinel other meept clients support via ModelConfig.ExtraHeaders is
	// not wired for CodexClient yet, so nothing supplies a value today; an
	// empty value is stripped by net/http (matching codex-rs omitting the
	// header when unset). Callers can still set a static session_id via
	// extra_headers below until per-turn substitution lands.
	req.Header.Set("session_id", "")

	req.Header.Set("OpenAI-Beta", codexResponsesBeta)
	// STREAMING DRIFT (documented, intentionally not fixed here): codex-rs
	// always POSTs stream:true and parses a text/event-stream (see the
	// Accept: text/event-stream in the upstream request builder); community
	// reports flag non-streaming requests to /backend-api/codex/responses
	// as unreliable. This client still sends stream:false and expects a
	// single JSON document. Switching to SSE requires a Responses-API event
	// parser (response.output_text.delta / response.completed lifecycle) —
	// not a contained change, so it stays a tracked follow-up.

	// Resolve OAuth token when a resolver is wired; otherwise fall back to
	// the static API key.
	token := ""
	if c.tokenResolver != nil && c.oauthProvider != "" {
		token, err = c.tokenResolver.ResolveToken(ctx, c.oauthProvider)
		if err != nil {
			return nil, &ClientError{Message: "failed to resolve OAuth token", Cause: err}
		}
	} else if cfg.APIKey != "" {
		token = cfg.APIKey
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
		// ChatGPT-Account-ID is derived from the access-token JWT; any
		// parse failure silently omits the header.
		if accountID := codexAccountID(token); accountID != "" {
			req.Header.Set("ChatGPT-Account-ID", accountID)
		}
	}

	// Static configured headers (ModelConfig.ExtraHeaders) are applied last
	// so they can override the Cloudflare/client defaults above. Values are
	// sent verbatim — no per-request session substitution.
	for k, v := range cfg.ExtraHeaders {
		if v == "" {
			continue
		}
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, &ClientError{Message: "request failed", Cause: err}
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			c.logger.Debug("codex: close response body", "error", cerr)
		}
	}()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &ClientError{Message: "failed to read response", Cause: err}
	}

	if resp.StatusCode != http.StatusOK {
		detail := string(respBody)
		if len(detail) > 512 {
			detail = detail[:512]
		}
		return nil, &ClientError{
			Message: fmt.Sprintf("codex API error (status %d): %s", resp.StatusCode, detail),
		}
	}

	var parsed codexResponsesResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, &ClientError{Message: "failed to parse response", Cause: err}
	}
	return c.parseResponse(&parsed, cfg), nil
}

// parseResponse converts the Responses output items into the common Response.
func (c *CodexClient) parseResponse(parsed *codexResponsesResponse, cfg *ModelConfig) *Response {
	resp := &Response{
		Model: cfg.ModelID,
	}

	var contentB strings.Builder
	var reasoningB strings.Builder
	hasFunctionCall := false

	for _, item := range parsed.Output {
		switch item.Type {
		case "message":
			for _, part := range item.Content {
				if part.Type == "output_text" && part.Text != "" {
					contentB.WriteString(part.Text)
				}
			}
		case "reasoning":
			for _, part := range item.Summary {
				if part.Type == "summary_text" && part.Text != "" {
					reasoningB.WriteString(part.Text)
				}
			}
		case "function_call":
			hasFunctionCall = true
			resp.ToolCalls = append(resp.ToolCalls, ToolCall{
				ID:   item.CallID,
				Type: "function",
				Function: ToolCallFunction{
					Name:      item.Name,
					Arguments: item.Arguments,
				},
			})
		}
	}

	resp.Content = contentB.String()
	resp.Reasoning = reasoningB.String()
	resp.Usage = TokenUsage{
		PromptTokens:     parsed.Usage.InputTokens,
		CompletionTokens: parsed.Usage.OutputTokens,
		TotalTokens:      parsed.Usage.InputTokens + parsed.Usage.OutputTokens,
	}
	if hasFunctionCall {
		resp.FinishReason = "tool_calls"
	} else {
		resp.FinishReason = "stop"
	}
	return resp
}

// Ensure CodexClient satisfies Chatter.
var _ Chatter = (*CodexClient)(nil)
