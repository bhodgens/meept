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
	defaultTimeout        = 120 * time.Second
	maxRetries            = 3
	retryBackoffBase      = 2.0 // seconds - exponential: 2, 4, 8
)

// HTTP status codes that warrant a retry
var retryableStatusCodes = map[int]bool{
	429: true, // Too Many Requests
	500: true, // Internal Server Error
	502: true, // Bad Gateway
	503: true, // Service Unavailable
	504: true, // Gateway Timeout
}

// Error types

// APIError is returned when the remote API returns an error response.
type APIError struct {
	StatusCode int
	Detail     string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Detail)
}

// ClientError is the base error for LLM client errors.
type ClientError struct {
	Message string
	Cause   error
}

func (e *ClientError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

func (e *ClientError) Unwrap() error {
	return e.Cause
}

// Client is an HTTP client for OpenAI-compatible chat completions endpoints.
type Client struct {
	config        *ModelConfig
	budget        *Budget
	httpClient    *http.Client
	logger        *slog.Logger
	metricsStore  *metrics.Store
	timeoutCalc   *metrics.Calculator
}

// ClientOption is a functional option for configuring a Client.
type ClientOption func(*Client)

// WithBudget sets the token budget for the client.
func WithBudget(budget *Budget) ClientOption {
	return func(c *Client) {
		c.budget = budget
	}
}

// WithLogger sets the logger for the client.
func WithLogger(logger *slog.Logger) ClientOption {
	return func(c *Client) {
		c.logger = logger
	}
}

// WithTimeout sets the HTTP timeout for the client.
func WithTimeout(timeout time.Duration) ClientOption {
	return func(c *Client) {
		c.httpClient.Timeout = timeout
	}
}

// WithMetricsStore sets the metrics store for the client.
func WithMetricsStore(store *metrics.Store) ClientOption {
	return func(c *Client) {
		c.metricsStore = store
	}
}

// WithTimeoutCalculator sets the adaptive timeout calculator for the client.
func WithTimeoutCalculator(calc *metrics.Calculator) ClientOption {
	return func(c *Client) {
		c.timeoutCalc = calc
	}
}

// NewClient creates a new LLM client.
func NewClient(config *ModelConfig, opts ...ClientOption) *Client {
	c := &Client{
		config: config,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
		logger: slog.Default(),
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// Chat sends a chat completion request and returns the parsed response.
func (c *Client) Chat(ctx context.Context, messages []ChatMessage, opts ...ChatOption) (*Response, error) {
	// Apply chat options, starting with model defaults
	chatOpts := &chatOptions{
		temperature:      c.config.Temperature,
		maxTokens:        c.config.MaxTokens,
		topP:             c.config.TopP,
		frequencyPenalty: c.config.FrequencyPenalty,
		presencePenalty:  c.config.PresencePenalty,
		stopSequences:    c.config.StopSequences,
	}
	for _, opt := range opts {
		opt(chatOpts)
	}

	// Budget gate
	if c.budget != nil {
		if !c.budget.CheckBudget() {
			return nil, &BudgetExceededError{Message: "Token budget exceeded - request blocked"}
		}
		if err := c.budget.WaitForRateLimit(ctx); err != nil {
			return nil, err
		}
	}

	// Compute adaptive timeout if available
	if c.timeoutCalc != nil {
		estimatedTokens := chatOpts.maxTokens
		if estimatedTokens <= 0 {
			estimatedTokens = 4096 // Safe default
		}
		timeout := c.timeoutCalc.Calculate(ctx, c.config.ProviderID, c.config.ModelID, estimatedTokens, defaultTimeout)
		c.httpClient.Timeout = timeout
	}

	// Build request payload
	msgDicts := make([]map[string]any, len(messages))
	for i, msg := range messages {
		msgDicts[i] = msg.ToOpenAIDict()
	}

	payload := map[string]any{
		"model":       c.config.ModelID,
		"messages":    msgDicts,
		"temperature": chatOpts.temperature,
		"max_tokens":  chatOpts.maxTokens,
	}

	// Add optional parameters if set
	if chatOpts.topP > 0 {
		payload["top_p"] = chatOpts.topP
	}
	if chatOpts.frequencyPenalty != 0 {
		payload["frequency_penalty"] = chatOpts.frequencyPenalty
	}
	if chatOpts.presencePenalty != 0 {
		payload["presence_penalty"] = chatOpts.presencePenalty
	}
	if len(chatOpts.stopSequences) > 0 {
		payload["stop"] = chatOpts.stopSequences
	}

	if len(chatOpts.tools) > 0 {
		payload["tools"] = chatOpts.tools
	}

	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		resp, err := c.doRequest(ctx, payload)
		if err != nil {
			var apiErr *APIError
			if errors.As(err, &apiErr) && retryableStatusCodes[apiErr.StatusCode] {
				c.logger.Warn("Retryable error",
					"status", apiErr.StatusCode,
					"attempt", attempt,
					"max_retries", maxRetries,
				)
				lastErr = err
				if attempt < maxRetries {
					sleepDuration := time.Duration(retryBackoffBase*float64(attempt)) * time.Second
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

		// Record usage
		if c.budget != nil {
			c.budget.RecordUsage(resp.Usage)
		}

		return resp, nil
	}

	return nil, &ClientError{
		Message: fmt.Sprintf("All %d attempts failed", maxRetries),
		Cause:   lastErr,
	}
}

// ChatWithProgress sends a chat completion request with progress reporting.
// The progress callback is invoked at various stages of the request lifecycle.
// If progress is nil, this behaves identically to Chat().
func (c *Client) ChatWithProgress(ctx context.Context, messages []ChatMessage, progress ProgressCallback, opts ...ChatOption) (*Response, error) {
	// Helper function to safely call progress callback
	reportProgress := func(stage ProgressStage, detail string) {
		if progress == nil {
			return
		}
		// Call progress in a goroutine to prevent callback errors from failing the request
		func() {
			defer func() {
				if r := recover(); r != nil {
					c.logger.Warn("Progress callback panicked", "stage", stage, "panic", r)
				}
			}()
			progress(stage, detail)
		}()
	}

	// Report starting stage
	reportProgress(ProgressStageStarting, "Starting LLM request...")

	// Apply chat options, starting with model defaults
	chatOpts := &chatOptions{
		temperature:      c.config.Temperature,
		maxTokens:        c.config.MaxTokens,
		topP:             c.config.TopP,
		frequencyPenalty: c.config.FrequencyPenalty,
		presencePenalty:  c.config.PresencePenalty,
		stopSequences:    c.config.StopSequences,
	}
	for _, opt := range opts {
		opt(chatOpts)
	}

	// Budget gate
	if c.budget != nil {
		reportProgress(ProgressStageStarting, "Checking token budget...")
		if !c.budget.CheckBudget() {
			return nil, &BudgetExceededError{Message: "Token budget exceeded - request blocked"}
		}

		reportProgress(ProgressStageStarting, "Waiting for rate limit...")
		if err := c.budget.WaitForRateLimit(ctx); err != nil {
			return nil, err
		}
	}

	// Build request payload
	msgDicts := make([]map[string]any, len(messages))
	for i, msg := range messages {
		msgDicts[i] = msg.ToOpenAIDict()
	}

	payload := map[string]any{
		"model":       c.config.ModelID,
		"messages":    msgDicts,
		"temperature": chatOpts.temperature,
		"max_tokens":  chatOpts.maxTokens,
	}

	// Add optional parameters if set
	if chatOpts.topP > 0 {
		payload["top_p"] = chatOpts.topP
	}
	if chatOpts.frequencyPenalty != 0 {
		payload["frequency_penalty"] = chatOpts.frequencyPenalty
	}
	if chatOpts.presencePenalty != 0 {
		payload["presence_penalty"] = chatOpts.presencePenalty
	}
	if len(chatOpts.stopSequences) > 0 {
		payload["stop"] = chatOpts.stopSequences
	}

	if len(chatOpts.tools) > 0 {
		payload["tools"] = chatOpts.tools
		reportProgress(ProgressStageToolCall, fmt.Sprintf("Request includes %d tools", len(chatOpts.tools)))
	}

	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		if attempt > 1 {
			reportProgress(ProgressStageThinking, fmt.Sprintf("Retry attempt %d/%d...", attempt, maxRetries))
		} else {
			reportProgress(ProgressStageThinking, "Model is thinking...")
		}

		resp, err := c.doRequest(ctx, payload)
		if err != nil {
			var apiErr *APIError
			if errors.As(err, &apiErr) && retryableStatusCodes[apiErr.StatusCode] {
				c.logger.Warn("Retryable error",
					"status", apiErr.StatusCode,
					"attempt", attempt,
					"max_retries", maxRetries,
				)
				lastErr = err
				if attempt < maxRetries {
					sleepDuration := time.Duration(retryBackoffBase*float64(attempt)) * time.Second
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

		// Streaming stage - response received
		reportProgress(ProgressStageStreaming, "Receiving response...")

		// Record usage
		if c.budget != nil {
			c.budget.RecordUsage(resp.Usage)
		}

		// Check if response contains tool calls
		if resp.HasToolCalls() {
			reportProgress(ProgressStageToolCall, fmt.Sprintf("Response contains %d tool calls", len(resp.ToolCalls)))
		}

		// Report completion with token count
		reportProgress(ProgressStageDone, fmt.Sprintf("Complete: %d tokens", resp.Usage.TotalTokens))

		return resp, nil
	}

	reportProgress(ProgressStageDone, fmt.Sprintf("Failed after %d attempts", maxRetries))
	return nil, &ClientError{
		Message: fmt.Sprintf("All %d attempts failed", maxRetries),
		Cause:   lastErr,
	}
}

// chatOptions holds options for a chat request.
type chatOptions struct {
	tools            []ToolDefinition
	temperature      float64
	maxTokens        int
	topP             float64
	frequencyPenalty float64
	presencePenalty  float64
	stopSequences    []string
}

// ChatOption is a functional option for configuring a chat request.
type ChatOption func(*chatOptions)

// WithTools sets the tools for the chat request.
func WithTools(tools []ToolDefinition) ChatOption {
	return func(o *chatOptions) {
		o.tools = tools
	}
}

// WithTemperature sets the temperature for the chat request.
func WithTemperature(temp float64) ChatOption {
	return func(o *chatOptions) {
		o.temperature = temp
	}
}

// WithMaxTokens sets the max tokens for the chat request.
func WithMaxTokens(tokens int) ChatOption {
	return func(o *chatOptions) {
		o.maxTokens = tokens
	}
}

// WithTopP sets the top_p (nucleus sampling) for the chat request.
func WithTopP(p float64) ChatOption {
	return func(o *chatOptions) {
		o.topP = p
	}
}

// WithFrequencyPenalty sets the frequency penalty for the chat request.
func WithFrequencyPenalty(p float64) ChatOption {
	return func(o *chatOptions) {
		o.frequencyPenalty = p
	}
}

// WithPresencePenalty sets the presence penalty for the chat request.
func WithPresencePenalty(p float64) ChatOption {
	return func(o *chatOptions) {
		o.presencePenalty = p
	}
}

// WithStopSequences sets the stop sequences for the chat request.
func WithStopSequences(seqs []string) ChatOption {
	return func(o *chatOptions) {
		o.stopSequences = seqs
	}
}

// doRequest performs the HTTP request and parses the response.
func (c *Client) doRequest(ctx context.Context, payload map[string]any) (*Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, &ClientError{Message: "failed to marshal request", Cause: err}
	}

	// Build URL - baseURL should be the full API base (e.g., http://host/v1 or http://host/api)
	// We just append /chat/completions to whatever baseURL is configured
	baseURL := strings.TrimSuffix(c.config.BaseURL, "/")
	url := baseURL + "/chat/completions"

	c.logger.Debug("Making LLM request", "url", url, "model", c.config.ModelID)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, &ClientError{Message: "failed to create request", Cause: err}
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.config.APIKey)
	}

	// Time the HTTP request
	start := time.Now()
	resp, err := c.httpClient.Do(req)
	latencyMs := time.Since(start).Milliseconds()

	// Record metrics if metrics store is configured
	if c.metricsStore != nil && payload["max_tokens"] != nil {
		errType := metrics.ErrorTypeNone
		if err != nil {
			errType = metrics.ClassifyError(err, 0)
		} else if resp != nil {
			errType = metrics.ClassifyError(nil, resp.StatusCode)
		}
		go func() {
			record := metrics.RequestRecord{
				Timestamp:  time.Now(),
				ProviderID: c.config.ProviderID,
				ModelID:    c.config.ModelID,
				LatencyMs:  latencyMs,
				HTTPStatus: 0,
				ErrorType:  errType,
				Success:    err == nil && (resp == nil || resp.StatusCode == http.StatusOK),
			}
			if resp != nil {
				record.HTTPStatus = resp.StatusCode
			}
			// Estimate tokens (rough)
			if maxTok, ok := payload["max_tokens"].(int); ok {
				record.CompletionTokens = maxTok / 2 // Very rough estimate
			}
			if rerr := c.metricsStore.Record(context.Background(), record); rerr != nil {
				c.logger.Debug("metrics record failed", "error", rerr)
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

	c.logger.Debug("LLM response received", "status", resp.StatusCode, "content_type", resp.Header.Get("Content-Type"))

	// Check for rate limit (429) specifically
	if resp.StatusCode == http.StatusTooManyRequests {
		detail := string(respBody)
		if len(detail) > 500 {
			detail = detail[:500]
		}
		retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
		return nil, &RateLimitError{
			ProviderID: c.config.ProviderID,
			ModelID:    c.config.ModelID,
			RetryAfter: retryAfter,
			Cause:      &APIError{StatusCode: resp.StatusCode, Detail: detail},
		}
	}

	// Check for other retryable status codes
	if retryableStatusCodes[resp.StatusCode] {
		detail := string(respBody)
		if len(detail) > 500 {
			detail = detail[:500]
		}
		return nil, &APIError{StatusCode: resp.StatusCode, Detail: detail}
	}

	// Check for other error status codes
	if resp.StatusCode != http.StatusOK {
		detail := string(respBody)
		if len(detail) > 1000 {
			detail = detail[:1000]
		}
		return nil, &APIError{StatusCode: resp.StatusCode, Detail: detail}
	}

	// Parse response
	var chatResp ChatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		preview := string(respBody)
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		c.logger.Error("Failed to parse LLM response",
			"error", err,
			"status", resp.StatusCode,
			"content_type", resp.Header.Get("Content-Type"),
			"body_preview", preview,
		)
		return nil, &ClientError{Message: "failed to parse response", Cause: err}
	}

	parsedResp, err := c.parseResponse(&chatResp)

	// Update metrics with actual token counts if available
	if c.metricsStore != nil && parsedResp != nil {
		go func() {
			record := metrics.RequestRecord{
				Timestamp:        time.Now(),
				ProviderID:       c.config.ProviderID,
				ModelID:          c.config.ModelID,
				PromptTokens:     chatResp.Usage.PromptTokens,
				CompletionTokens: chatResp.Usage.CompletionTokens,
				LatencyMs:        latencyMs,
				HTTPStatus:       resp.StatusCode,
				ErrorType:        metrics.ErrorTypeNone,
				Success:          true,
			}
			if rerr := c.metricsStore.Record(context.Background(), record); rerr != nil {
				c.logger.Debug("metrics record failed", "error", rerr)
			}
		}()
	}

	return parsedResp, err
}

// parseResponse converts a raw ChatResponse to a Response.
func (c *Client) parseResponse(chatResp *ChatResponse) (*Response, error) {
	if len(chatResp.Choices) == 0 {
		return nil, &ClientError{Message: "no choices in response"}
	}

	choice := chatResp.Choices[0]
	msg := choice.Message

	var content string
	if msg.Content != nil {
		content = *msg.Content
	}

	var toolCalls []ToolCall
	if len(msg.ToolCalls) > 0 {
		toolCalls = make([]ToolCall, len(msg.ToolCalls))
		for i, tc := range msg.ToolCalls {
			toolCalls[i] = tc.ToToolCall()
		}
	}

	model := chatResp.Model
	if model == "" {
		model = c.config.ModelID
	}

	return &Response{
		Content:      content,
		ToolCalls:    toolCalls,
		Usage: TokenUsage{
			PromptTokens:     chatResp.Usage.PromptTokens,
			CompletionTokens: chatResp.Usage.CompletionTokens,
			TotalTokens:      chatResp.Usage.TotalTokens,
		},
		Model:        model,
		FinishReason: choice.FinishReason,
	}, nil
}

// SwitchModel switches to a different model/endpoint at runtime.
func (c *Client) SwitchModel(config *ModelConfig) {
	c.config = config
	c.logger.Info("Switched model",
		"model", config.ModelID,
		"base_url", config.BaseURL,
	)
}

// Close closes the client (releases resources).
func (c *Client) Close() error {
	c.httpClient.CloseIdleConnections()
	return nil
}

// Config returns the current model configuration.
// Compile-time assertion that Client implements io.Closer.
var _ io.Closer = (*Client)(nil)

func (c *Client) Config() *ModelConfig {
	return c.config
}

// Budget returns the token budget tracker, if one is configured.
func (c *Client) Budget() *Budget {
	return c.budget
}
