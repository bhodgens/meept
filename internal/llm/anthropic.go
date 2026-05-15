// Package llm provides LLM client functionality for various providers.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/llm/metrics"
)

const (
	anthropicDefaultTimeout = 5 * time.Minute
	anthropicAPIVersion     = "2023-06-01"
	anthropicMaxRetries     = 3
	anthropicRetryBackoff   = 2.0 // seconds - exponential: 2, 4, 8
)

// Anthropic HTTP status codes that warrant a retry
var anthropicRetryableStatusCodes = map[int]bool{
	429: true, // Too Many Requests
	500: true, // Internal Server Error
	502: true, // Bad Gateway
	503: true, // Service Unavailable
	504: true, // Gateway Timeout
	529: true, // Overloaded
}

// AnthropicClient implements the Chatter interface for Anthropic's Messages API.
// It provides native support for Anthropic-specific features including extended thinking.
type AnthropicClient struct {
	config       *ModelConfig
	budget       *Budget
	httpClient   *http.Client
	logger       *slog.Logger
	metricsStore *metrics.Store
	timeoutCalc  *metrics.Calculator
	tokenCache   ResponseCache
	keyBuilder   *CacheKeyBuilder
}

// AnthropicClientOption is a functional option for configuring an AnthropicClient.
type AnthropicClientOption func(*AnthropicClient)

// WithAnthropicBudget sets the token budget for the client.
func WithAnthropicBudget(budget *Budget) AnthropicClientOption {
	return func(c *AnthropicClient) {
		c.budget = budget
	}
}

// WithAnthropicLogger sets the logger for the client.
func WithAnthropicLogger(logger *slog.Logger) AnthropicClientOption {
	return func(c *AnthropicClient) {
		c.logger = logger
	}
}

// WithAnthropicTimeout sets the HTTP timeout for the client.
func WithAnthropicTimeout(timeout time.Duration) AnthropicClientOption {
	return func(c *AnthropicClient) {
		c.httpClient.Timeout = timeout
	}
}

// WithAnthropicMetricsStore sets the metrics store for the client.
func WithAnthropicMetricsStore(store *metrics.Store) AnthropicClientOption {
	return func(c *AnthropicClient) {
		c.metricsStore = store
	}
}

// WithAnthropicTimeoutCalculator sets the adaptive timeout calculator for the client.
func WithAnthropicTimeoutCalculator(calc *metrics.Calculator) AnthropicClientOption {
	return func(c *AnthropicClient) {
		c.timeoutCalc = calc
	}
}

// WithAnthropicTokenCache sets the token cache for the Anthropic client.
func WithAnthropicTokenCache(cache ResponseCache) AnthropicClientOption {
	return func(c *AnthropicClient) {
		if cache != nil {
			c.tokenCache = cache
			c.keyBuilder = NewCacheKeyBuilder(true) // Enable file-aware caching
		}
	}
}

// NewAnthropicClient creates a new Anthropic API client.
func NewAnthropicClient(config *ModelConfig, opts ...AnthropicClientOption) *AnthropicClient {
	c := &AnthropicClient{
		config: config,
		httpClient: &http.Client{
			Timeout: anthropicDefaultTimeout,
		},
		logger: slog.Default(),
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// Chat sends a chat completion request to Anthropic's Messages API.
func (c *AnthropicClient) Chat(ctx context.Context, messages []ChatMessage, opts ...ChatOption) (*Response, error) {
	chatOpts := &chatOptions{
		temperature:   c.config.Temperature,
		maxTokens:     c.config.MaxTokens,
		topP:          c.config.TopP,
		stopSequences: c.config.StopSequences,
		// Note: Anthropic doesn't support frequency_penalty or presence_penalty
	}
	for _, opt := range opts {
		opt(chatOpts)
	}

	// Check cache
	if c.tokenCache != nil && c.keyBuilder != nil {
		cacheKey := c.keyBuilder.Build("", c.config.ModelID, messages)
		if cached, found := c.tokenCache.Get(ctx, cacheKey); found {
			return cached.Response, nil
		}
	}

	if c.budget != nil {
		if !c.budget.CheckBudget() {
			return nil, &BudgetExceededError{Message: ErrBudgetExceeded}
		}
		if err := c.budget.WaitForRateLimit(ctx); err != nil {
			return nil, err
		}
	}

	// Compute adaptive timeout if a calculator is configured.
	// LLM-3 FIX: use per-request context timeout instead of mutating shared httpClient.Timeout
	if c.timeoutCalc != nil {
		estimatedTokens := chatOpts.maxTokens
		if estimatedTokens <= 0 {
			estimatedTokens = 4096
		}
		timeout := c.timeoutCalc.Calculate(
			ctx,
			c.config.ProviderID,
			c.config.ModelID,
			estimatedTokens,
			anthropicDefaultTimeout,
		)
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	// Build Anthropic API request
	reqBody, err := c.buildRequest(messages, chatOpts, false)
	if err != nil {
		return nil, &ClientError{Message: "failed to build request", Cause: err}
	}

	var lastErr error
	for attempt := 1; attempt <= anthropicMaxRetries; attempt++ {
		resp, err := c.doRequest(ctx, reqBody)
		if err != nil {
			var apiErr *APIError
			if errors.As(err, &apiErr) && anthropicRetryableStatusCodes[apiErr.StatusCode] {
				c.logger.Warn("Retryable error",
					"status", apiErr.StatusCode,
					"attempt", attempt,
					"max_retries", anthropicMaxRetries,
				)
				lastErr = err
				if attempt < anthropicMaxRetries {
					sleepDuration := time.Duration(anthropicRetryBackoff*float64(attempt)) * time.Second
					select {
					case <-time.After(sleepDuration):
						continue
					case <-ctx.Done():
						return nil, ctx.Err()
					}
				}
				continue
			}
			return nil, err
		}

		if c.budget != nil {
			c.budget.RecordUsage(resp.Usage)
		}

		// Store in cache
		if c.tokenCache != nil && c.keyBuilder != nil {
			cacheKey := c.keyBuilder.Build("", c.config.ModelID, messages)
			c.tokenCache.Put(ctx, cacheKey, resp)
		}

		return resp, nil
	}

	return nil, &ClientError{
		Message: fmt.Sprintf("All %d attempts failed", anthropicMaxRetries),
		Cause:   lastErr,
	}
}

// ChatWithProgress sends a chat completion request with progress reporting.
// It emits ProgressStageThinking events during extended thinking phases.
func (c *AnthropicClient) ChatWithProgress(ctx context.Context, messages []ChatMessage, progress ProgressCallback, opts ...ChatOption) (*Response, error) {
	reportProgress := func(stage ProgressStage, detail string) {
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

	reportProgress(ProgressStageStarting, "Starting Anthropic request...")

	chatOpts := &chatOptions{
		temperature:   c.config.Temperature,
		maxTokens:     c.config.MaxTokens,
		topP:          c.config.TopP,
		stopSequences: c.config.StopSequences,
		// Note: Anthropic doesn't support frequency_penalty or presence_penalty
	}
	for _, opt := range opts {
		opt(chatOpts)
	}

	// Check cache
	if c.tokenCache != nil && c.keyBuilder != nil {
		cacheKey := c.keyBuilder.Build("", c.config.ModelID, messages)
		if cached, found := c.tokenCache.Get(ctx, cacheKey); found {
			reportProgress(ProgressStageDone, "Cache hit")
			return cached.Response, nil
		}
	}

	if c.budget != nil {
		reportProgress(ProgressStageStarting, "Checking token budget...")
		if !c.budget.CheckBudget() {
			return nil, &BudgetExceededError{Message: ErrBudgetExceeded}
		}

		reportProgress(ProgressStageStarting, "Waiting for rate limit...")
		if err := c.budget.WaitForRateLimit(ctx); err != nil {
			return nil, err
		}
	}

	// Compute adaptive timeout if a calculator is configured.
	// LLM-3 FIX: use per-request context timeout instead of mutating shared httpClient.Timeout
	if c.timeoutCalc != nil {
		estimatedTokens := chatOpts.maxTokens
		if estimatedTokens <= 0 {
			estimatedTokens = 4096
		}
		timeout := c.timeoutCalc.Calculate(
			ctx,
			c.config.ProviderID,
			c.config.ModelID,
			estimatedTokens,
			anthropicDefaultTimeout,
		)
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	// Build Anthropic API request with streaming enabled for progress
	reqBody, err := c.buildRequest(messages, chatOpts, true)
	if err != nil {
		return nil, &ClientError{Message: "failed to build request", Cause: err}
	}

	if len(chatOpts.tools) > 0 {
		reportProgress(ProgressStageToolCall, fmt.Sprintf("Request includes %d tools", len(chatOpts.tools)))
	}

	// Check if model supports extended thinking
	supportsExtendedThinking := c.config.HasCapability("extended_thinking")
	if supportsExtendedThinking {
		reportProgress(ProgressStageThinking, "Model supports extended thinking")
	}

	var lastErr error
	for attempt := 1; attempt <= anthropicMaxRetries; attempt++ {
		if attempt > 1 {
			reportProgress(ProgressStageThinking, fmt.Sprintf("Retry attempt %d/%d...", attempt, anthropicMaxRetries))
		} else {
			reportProgress(ProgressStageThinking, "Model is thinking...")
		}

		resp, err := c.doStreamingRequest(ctx, reqBody, reportProgress)
		if err != nil {
			var apiErr *APIError
			if errors.As(err, &apiErr) && anthropicRetryableStatusCodes[apiErr.StatusCode] {
				c.logger.Warn("Retryable error",
					"status", apiErr.StatusCode,
					"attempt", attempt,
					"max_retries", anthropicMaxRetries,
				)
				lastErr = err
				if attempt < anthropicMaxRetries {
					sleepDuration := time.Duration(anthropicRetryBackoff*float64(attempt)) * time.Second
					select {
					case <-time.After(sleepDuration):
						continue
					case <-ctx.Done():
						reportProgress(ProgressStageDone, "Request cancelled")
						return nil, ctx.Err()
					}
				}
				continue
			}
			reportProgress(ProgressStageDone, fmt.Sprintf("Error: %v", err))
			return nil, err
		}

		reportProgress(ProgressStageStreaming, "Receiving response...")

		if c.budget != nil {
			c.budget.RecordUsage(resp.Usage)
		}

		// Store in cache
		if c.tokenCache != nil && c.keyBuilder != nil {
			cacheKey := c.keyBuilder.Build("", c.config.ModelID, messages)
			c.tokenCache.Put(ctx, cacheKey, resp)
		}

		if resp.HasToolCalls() {
			reportProgress(ProgressStageToolCall, fmt.Sprintf("Response contains %d tool calls", len(resp.ToolCalls)))
		}

		reportProgress(ProgressStageDone, fmt.Sprintf("Complete: %d tokens", resp.Usage.TotalTokens))

		return resp, nil
	}

	reportProgress(ProgressStageDone, fmt.Sprintf("Failed after %d attempts", anthropicMaxRetries))
	return nil, &ClientError{
		Message: fmt.Sprintf("All %d attempts failed", anthropicMaxRetries),
		Cause:   lastErr,
	}
}

// Anthropic API request structures

type anthropicRequest struct {
	Model         string             `json:"model"`
	MaxTokens     int                `json:"max_tokens"`
	System        string             `json:"system,omitempty"`
	Messages      []anthropicMessage `json:"messages"`
	Tools         []anthropicTool    `json:"tools,omitempty"`
	Temperature   *float64           `json:"temperature,omitempty"`
	TopP          *float64           `json:"top_p,omitempty"`
	StopSequences []string           `json:"stop_sequences,omitempty"`
	Stream        bool               `json:"stream,omitempty"`
	// Extended thinking configuration
	Thinking *anthropicThinkingConfig `json:"thinking,omitempty"`
}

type anthropicThinkingConfig struct {
	Type         string `json:"type"`
	BudgetTokens *int   `json:"budget_tokens,omitempty"`
}

type anthropicMessage struct {
	Role    string             `json:"role"`
	Content []anthropicContent `json:"content"`
}

type anthropicContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	// For tool results
	ToolUseID string `json:"tool_use_id,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
	Content   string `json:"content,omitempty"`
	// For tool use
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// Anthropic API response structures

type anthropicResponse struct {
	ID           string                  `json:"id"`
	Type         string                  `json:"type"`
	Role         string                  `json:"role"`
	Content      []anthropicContentBlock `json:"content"`
	StopReason   string                  `json:"stop_reason"`
	StopSequence *string                 `json:"stop_sequence,omitempty"`
	Usage        anthropicUsage          `json:"usage"`
	Model        string                  `json:"model"`
}

type anthropicContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	// Tool use fields
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
	// Thinking fields
	Thinking string `json:"thinking,omitempty"`
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type anthropicErrorResponse struct {
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// Streaming event structures

type anthropicStreamEvent struct {
	Type  string          `json:"type"`
	Index int             `json:"index,omitempty"`
	Delta *anthropicDelta `json:"delta,omitempty"`
	// For content_block_start
	ContentBlock *anthropicContentBlock `json:"content_block,omitempty"`
	// For message_start/message_delta
	Message *anthropicMessageMeta `json:"message,omitempty"`
	Usage   *anthropicUsage       `json:"usage,omitempty"`
}

type anthropicDelta struct {
	Type        string `json:"type,omitempty"`
	Text        string `json:"text,omitempty"`
	Thinking    string `json:"thinking,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
	StopReason  string `json:"stop_reason,omitempty"`
}

type anthropicMessageMeta struct {
	ID      string                  `json:"id,omitempty"`
	Type    string                  `json:"type,omitempty"`
	Role    string                  `json:"role,omitempty"`
	Content []anthropicContentBlock `json:"content,omitempty"`
	Usage   *anthropicUsage         `json:"usage,omitempty"`
}

// contentBlockAccum accumulates content during streaming response parsing.
type contentBlockAccum struct {
	Type      string
	Text      strings.Builder
	ID        string
	Name      string
	InputJSON strings.Builder
	Thinking  strings.Builder
}

// buildRequest constructs an Anthropic API request from our internal message format.
func (c *AnthropicClient) buildRequest(messages []ChatMessage, opts *chatOptions, stream bool) (*anthropicRequest, error) {
	// Extract system prompt from messages
	var systemPrompt string
	var apiMessages []anthropicMessage

	// LLM-2 FIX: Track mapping from input messages index to apiMessages index
	// This is needed because system messages are extracted and don't appear in apiMessages,
	// causing index divergence between the input slice and output slice.
	msgIndexToAPIIndex := make(map[int]int)

	for i, msg := range messages {
		switch msg.Role {
		case RoleSystem:
			// Accumulate system prompts - these do NOT get added to apiMessages
			if systemPrompt != "" {
				systemPrompt += "\n\n" + msg.Content
			} else {
				systemPrompt = msg.Content
			}
		case RoleTool:
			// LLM-1 FIX: Tool results must be separate user messages per Anthropic API spec
			// Do NOT append to assistant message content - create a new user message
			msgIndexToAPIIndex[i] = len(apiMessages)
			apiMessages = append(apiMessages, anthropicMessage{
				Role: "user",
				Content: []anthropicContent{{
					Type:      "tool_result",
					ToolUseID: msg.ToolCallID,
					Content:   msg.Content,
					IsError:   strings.Contains(strings.ToLower(msg.Content), "error"),
				}},
			})
		case RoleUser, RoleAssistant:
			msgIndexToAPIIndex[i] = len(apiMessages)
			apiMessages = append(apiMessages, anthropicMessage{
				Role: string(msg.Role),
				Content: []anthropicContent{{
					Type: ContentTypeText,
					Text: msg.Content,
				}},
			})
		}
	}

	// Handle tool calls in assistant messages
	// LLM-2 FIX: Use the mapping to find the correct apiMessages index
	for i, msg := range messages {
		if msg.Role != RoleAssistant || len(msg.ToolCalls) == 0 {
			continue
		}
		apiIdx, ok := msgIndexToAPIIndex[i]
		if !ok {
			continue // System message or other non-mapped message
		}
		// Replace the simple text content with structured content
		var content []anthropicContent
		if msg.Content != "" {
			content = append(content, anthropicContent{
				Type: ContentTypeText,
				Text: msg.Content,
			})
		}
		for _, tc := range msg.ToolCalls {
			content = append(content, anthropicContent{
				Type:  ContentTypeToolUse,
				ID:    tc.ID,
				Name:  tc.Function.Name,
				Input: json.RawMessage(tc.Function.Arguments),
			})
		}
		if apiIdx < len(apiMessages) && apiMessages[apiIdx].Role == "assistant" {
			apiMessages[apiIdx].Content = content
		}
	}

	req := &anthropicRequest{
		Model:       c.config.ModelID,
		MaxTokens:   opts.maxTokens,
		System:      systemPrompt,
		Messages:    apiMessages,
		Stream:      stream,
		Temperature: &opts.temperature,
	}

	// Add optional parameters if set
	if opts.topP > 0 {
		req.TopP = &opts.topP
	}
	if len(opts.stopSequences) > 0 {
		req.StopSequences = opts.stopSequences
	}

	// Add tools if present
	if len(opts.tools) > 0 {
		req.Tools = make([]anthropicTool, len(opts.tools))
		for i, tool := range opts.tools {
			schema, err := json.Marshal(tool.Function.Parameters)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal tool schema: %w", err)
			}
			req.Tools[i] = anthropicTool{
				Name:        tool.Function.Name,
				Description: tool.Function.Description,
				InputSchema: json.RawMessage(schema),
			}
		}
	}

	// Enable extended thinking if supported
	if c.config.HasCapability("extended_thinking") {
		req.Thinking = &anthropicThinkingConfig{
			Type: "enabled",
			// BudgetTokens is optional - let Anthropic use default
		}
	}

	return req, nil
}

// doRequest performs a non-streaming HTTP request to Anthropic's API.
func (c *AnthropicClient) doRequest(ctx context.Context, reqBody *anthropicRequest) (*Response, error) {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, &ClientError{Message: "failed to marshal request", Cause: err}
	}

	baseURL := strings.TrimSuffix(c.config.BaseURL, "/")
	url := baseURL + "/v1/messages"

	c.logger.Debug("Making Anthropic request", "url", url, "model", c.config.ModelID)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, &ClientError{Message: "failed to create request", Cause: err}
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("x-api-key", c.config.APIKey)
	httpReq.Header.Set("anthropic-version", anthropicAPIVersion)

	start := time.Now()
	resp, err := c.httpClient.Do(httpReq)
	latencyMs := time.Since(start).Milliseconds()

	// Record metrics if a store is configured.
	if c.metricsStore != nil {
		errType := metrics.ErrorTypeNone
		if err != nil {
			errType = metrics.ClassifyError(err, 0)
		} else if resp != nil {
			errType = metrics.ClassifyError(nil, resp.StatusCode)
		}
		record := metrics.RequestRecord{
			Timestamp:        time.Now(),
			ProviderID:       c.config.ProviderID,
			ModelID:          c.config.ModelID,
			LatencyMs:        latencyMs,
			HTTPStatus:       0,
			ErrorType:        errType,
			Success:          err == nil && resp != nil && resp.StatusCode == http.StatusOK,
			CompletionTokens: reqBody.MaxTokens / 2,
		}
		if resp != nil {
			record.HTTPStatus = resp.StatusCode
		}
		store := c.metricsStore
		logger := c.logger
		//nolint:gosec // goroutine outlives request context
		go func() {
			if rerr := store.Record(context.Background(), record); rerr != nil {
				logger.Debug("metrics record failed", "error", rerr)
			}
		}()
	}

	if err != nil {
		return nil, &ClientError{Message: "request failed", Cause: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &ClientError{Message: "failed to read response", Cause: err}
	}

	c.logger.Debug("Anthropic response received", "status", resp.StatusCode)

	// Check for retryable status codes
	if anthropicRetryableStatusCodes[resp.StatusCode] {
		detail := string(respBody)
		if len(detail) > 500 {
			detail = detail[:500]
		}
		return nil, &APIError{StatusCode: resp.StatusCode, Detail: detail}
	}

	// Check for other error status codes
	if resp.StatusCode != http.StatusOK {
		var apiErr anthropicErrorResponse
		if err := json.Unmarshal(respBody, &apiErr); err == nil && apiErr.Error.Message != "" {
			return nil, &APIError{StatusCode: resp.StatusCode, Detail: apiErr.Error.Message}
		}
		detail := string(respBody)
		if len(detail) > 1000 {
			detail = detail[:1000]
		}
		return nil, &APIError{StatusCode: resp.StatusCode, Detail: detail}
	}

	// Parse response
	var apiResp anthropicResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		preview := string(respBody)
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		c.logger.Error("Failed to parse Anthropic response",
			"error", err,
			"status", resp.StatusCode,
			"body_preview", preview,
		)
		return nil, &ClientError{Message: "failed to parse response", Cause: err}
	}

	return c.parseResponse(&apiResp), nil
}

// doStreamingRequest performs a streaming HTTP request to Anthropic's API.
// It processes server-sent events and reports progress via the callback.
func (c *AnthropicClient) doStreamingRequest(ctx context.Context, reqBody *anthropicRequest, progress func(ProgressStage, string)) (*Response, error) {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, &ClientError{Message: "failed to marshal request", Cause: err}
	}

	baseURL := strings.TrimSuffix(c.config.BaseURL, "/")
	url := baseURL + "/v1/messages"

	c.logger.Debug("Making Anthropic streaming request", "url", url, "model", c.config.ModelID)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, &ClientError{Message: "failed to create request", Cause: err}
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("x-api-key", c.config.APIKey)
	httpReq.Header.Set("anthropic-version", anthropicAPIVersion)

	start := time.Now()
	resp, err := c.httpClient.Do(httpReq)
	latencyMs := time.Since(start).Milliseconds()

	if c.metricsStore != nil {
		errType := metrics.ErrorTypeNone
		if err != nil {
			errType = metrics.ClassifyError(err, 0)
		} else if resp != nil {
			errType = metrics.ClassifyError(nil, resp.StatusCode)
		}
		record := metrics.RequestRecord{
			Timestamp:        time.Now(),
			ProviderID:       c.config.ProviderID,
			ModelID:          c.config.ModelID,
			LatencyMs:        latencyMs,
			ErrorType:        errType,
			Success:          err == nil && resp != nil && resp.StatusCode == http.StatusOK,
			CompletionTokens: reqBody.MaxTokens / 2,
		}
		if resp != nil {
			record.HTTPStatus = resp.StatusCode
		}
		store := c.metricsStore
		logger := c.logger
		//nolint:gosec // goroutine outlives request context
		go func() {
			if rerr := store.Record(context.Background(), record); rerr != nil {
				logger.Debug("metrics record failed", "error", rerr)
			}
		}()
	}

	if err != nil {
		return nil, &ClientError{Message: "request failed", Cause: err}
	}
	defer resp.Body.Close()

	// Check for error status before streaming
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		if anthropicRetryableStatusCodes[resp.StatusCode] {
			detail := string(respBody)
			if len(detail) > 500 {
				detail = detail[:500]
			}
			return nil, &APIError{StatusCode: resp.StatusCode, Detail: detail}
		}
		var apiErr anthropicErrorResponse
		if err := json.Unmarshal(respBody, &apiErr); err == nil && apiErr.Error.Message != "" {
			return nil, &APIError{StatusCode: resp.StatusCode, Detail: apiErr.Error.Message}
		}
		return nil, &APIError{StatusCode: resp.StatusCode, Detail: string(respBody)}
	}

	// Parse the SSE stream
	return c.parseStreamingResponse(resp.Body, progress)
}

// parseStreamingResponse parses server-sent events from Anthropic's streaming API.
func (c *AnthropicClient) parseStreamingResponse(body io.Reader, progress func(ProgressStage, string)) (*Response, error) {
	var blocks []contentBlockAccum
	var stopReason = "end_turn"
	var usage anthropicUsage

	scanner := newSSEScanner(body)
	for scanner.Scan() {
		event := scanner.Event()
		if event == nil {
			continue
		}

		var streamEvent anthropicStreamEvent
		if err := json.Unmarshal([]byte(event.Data), &streamEvent); err != nil {
			// Skip unparseable events (like ping)
			continue
		}

		switch streamEvent.Type {
		case "message_start":
			if streamEvent.Message != nil && streamEvent.Message.Usage != nil {
				usage.InputTokens = streamEvent.Message.Usage.InputTokens
			}

		case "content_block_start":
			if streamEvent.ContentBlock != nil {
				accum := contentBlockAccum{Type: streamEvent.ContentBlock.Type}
				if streamEvent.ContentBlock.ID != "" {
					accum.ID = streamEvent.ContentBlock.ID
				}
				if streamEvent.ContentBlock.Name != "" {
					accum.Name = streamEvent.ContentBlock.Name
				}
				blocks = append(blocks, accum)

				// Report extended thinking start
				if streamEvent.ContentBlock.Type == ContentTypeThinking && progress != nil {
					progress(ProgressStageThinking, "Extended thinking in progress...")
				}
			}

		case "content_block_delta":
			if len(blocks) == 0 || streamEvent.Delta == nil {
				continue
			}

			currentBlock := &blocks[len(blocks)-1]
			switch currentBlock.Type {
			case "text":
				if streamEvent.Delta.Text != "" {
					currentBlock.Text.WriteString(streamEvent.Delta.Text)
					if progress != nil {
						progress(ProgressStageStreaming, "Receiving text...")
					}
				}
			case ContentTypeThinking:
				if streamEvent.Delta.Thinking != "" {
					currentBlock.Thinking.WriteString(streamEvent.Delta.Thinking)
					// Don't spam progress for each thinking delta
				}
			case ContentTypeToolUse:
				if streamEvent.Delta.PartialJSON != "" {
					currentBlock.InputJSON.WriteString(streamEvent.Delta.PartialJSON)
				}
			}

		case "content_block_stop":
			// Block complete, nothing special to do

		case "message_delta":
			if streamEvent.Delta != nil && streamEvent.Delta.StopReason != "" {
				stopReason = streamEvent.Delta.StopReason
			}
			if streamEvent.Usage != nil {
				usage.OutputTokens = streamEvent.Usage.OutputTokens
			}

		case "message_stop":
			// Message complete
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, &ClientError{Message: "error reading stream", Cause: err}
	}

	// Build the response from accumulated blocks
	return c.buildResponseFromBlocks(blocks, stopReason, usage)
}

// buildResponseFromBlocks constructs a Response from accumulated content blocks.
func (c *AnthropicClient) buildResponseFromBlocks(blocks []contentBlockAccum, stopReason string, usage anthropicUsage) (*Response, error) {
	var content strings.Builder
	var toolCalls []ToolCall
	var thinking strings.Builder

	for _, block := range blocks {
		switch block.Type {
		case "text":
			content.WriteString(block.Text.String())
		case ContentTypeThinking:
			thinking.WriteString(block.Thinking.String())
		case ContentTypeToolUse:
			// Parse the accumulated input JSON
			var input json.RawMessage
			if block.InputJSON.Len() > 0 {
				input = json.RawMessage(block.InputJSON.String())
			} else {
				input = json.RawMessage("{}")
			}
			toolCalls = append(toolCalls, ToolCall{
				ID:   block.ID,
				Type: ContentTypeFunction,
				Function: ToolCallFunction{
					Name:      block.Name,
					Arguments: string(input),
				},
			})
		}
	}

	// Prepend thinking to content if present (for transparency)
	finalContent := content.String()
	if thinking.Len() > 0 {
		// In extended thinking mode, we include the thinking in the response
		// This allows the system to see the model's reasoning process
		finalContent = fmt.Sprintf("[Thinking]\n%s\n\n[Response]\n%s", thinking.String(), finalContent)
	}

	return &Response{
		Content:   finalContent,
		ToolCalls: toolCalls,
		Usage: TokenUsage{
			PromptTokens:     usage.InputTokens,
			CompletionTokens: usage.OutputTokens,
			TotalTokens:      usage.InputTokens + usage.OutputTokens,
		},
		Model:        c.config.ModelID,
		FinishReason: stopReason,
	}, nil
}

// parseResponse converts an Anthropic API response to our internal Response format.
func (c *AnthropicClient) parseResponse(apiResp *anthropicResponse) *Response {
	var content strings.Builder
	var toolCalls []ToolCall
	var thinking strings.Builder

	for _, block := range apiResp.Content {
		switch block.Type {
		case "text":
			content.WriteString(block.Text)
		case ContentTypeThinking:
			thinking.WriteString(block.Thinking)
		case ContentTypeToolUse:
			var input = block.Input
			if input == nil {
				input = json.RawMessage("{}")
			}
			toolCalls = append(toolCalls, ToolCall{
				ID:   block.ID,
				Type: ContentTypeFunction,
				Function: ToolCallFunction{
					Name:      block.Name,
					Arguments: string(input),
				},
			})
		}
	}

	// Prepend thinking to content if present
	finalContent := content.String()
	if thinking.Len() > 0 {
		finalContent = fmt.Sprintf("[Thinking]\n%s\n\n[Response]\n%s", thinking.String(), finalContent)
	}

	return &Response{
		Content:   finalContent,
		ToolCalls: toolCalls,
		Usage: TokenUsage{
			PromptTokens:     apiResp.Usage.InputTokens,
			CompletionTokens: apiResp.Usage.OutputTokens,
			TotalTokens:      apiResp.Usage.InputTokens + apiResp.Usage.OutputTokens,
		},
		Model:        apiResp.Model,
		FinishReason: apiResp.StopReason,
	}
}

// Close closes the client and releases resources.
func (c *AnthropicClient) Close() error {
	c.httpClient.CloseIdleConnections()
	return nil
}

// Config returns the current model configuration.
func (c *AnthropicClient) Config() *ModelConfig {
	return c.config
}

// SSE scanner for server-sent events

type sseScanner struct {
	reader io.Reader
	buffer []byte
	err    error
	event  *sseEvent
}

type sseEvent struct {
	Type string
	Data string
}

func newSSEScanner(reader io.Reader) *sseScanner {
	return &sseScanner{
		reader: reader,
		buffer: make([]byte, 0, 4096),
	}
}

func (s *sseScanner) Scan() bool {
	s.event = nil
	s.event = &sseEvent{}

	var currentLine strings.Builder
	chunk := make([]byte, 4096)

	for {
		// Read more data if buffer is empty
		if len(s.buffer) == 0 {
			n, err := s.reader.Read(chunk)
			if err != nil {
				if err == io.EOF {
					return len(s.buffer) > 0 || currentLine.Len() > 0
				}
				s.err = err
				return false
			}
			s.buffer = append(s.buffer, chunk[:n]...)
		}

		// Process buffer
	bufferLoop:
		for i := 0; i < len(s.buffer); i++ {
			c := s.buffer[i]

			switch c {
			case '\r':
				// Skip \r, look for \n
				if i+1 < len(s.buffer) && s.buffer[i+1] == '\n' {
					i++
				}
				// End of line
				if currentLine.Len() > 0 {
					s.processLine(currentLine.String())
					currentLine.Reset()
				} else if s.event.Data != "" {
					// Empty line means end of event
					s.buffer = s.buffer[i+1:]
					return true
				}
				s.buffer = s.buffer[i+1:]
				break bufferLoop
			case '\n':
				// End of line
				if currentLine.Len() > 0 {
					s.processLine(currentLine.String())
					currentLine.Reset()
				} else if s.event.Data != "" {
					// Empty line means end of event
					s.buffer = s.buffer[i+1:]
					return true
				}
				s.buffer = s.buffer[i+1:]
				break bufferLoop
			default:
				currentLine.WriteByte(c)
			}

			// If we've consumed all buffer bytes
			if i == len(s.buffer)-1 {
				s.buffer = s.buffer[:0]
				break
			}
		}
	}
}

func (s *sseScanner) processLine(line string) {
	if after, ok := strings.CutPrefix(line, "event: "); ok {
		s.event.Type = after
	} else if after, ok := strings.CutPrefix(line, "data: "); ok {
		data := after
		if s.event.Data != "" {
			s.event.Data += "\n" + data
		} else {
			s.event.Data = data
		}
	}
}

func (s *sseScanner) Event() *sseEvent {
	return s.event
}

func (s *sseScanner) Err() error {
	return s.err
}
