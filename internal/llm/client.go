package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/llm/metrics"
)

const (
	defaultTimeout       = 120 * time.Second
	maxRetries           = 3
	retryBackoffBase     = 2.0 // seconds - exponential: 2, 4, 8
	retryBackoffMaxDelay = 30 * time.Second
	streamMaxRetries     = 3 // D4: retry attempts for streaming
)

// HTTP status codes that warrant a retry
var retryableStatusCodes = map[int]bool{
	429: true, // Too Many Requests
	500: true, // Internal Server Error
	502: true, // Bad Gateway
	503: true, // Service Unavailable
	504: true, // Gateway Timeout
	529: true, // Overloaded (Anthropic-specific)
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

func (e *APIError) UserMessage() string {
	switch e.StatusCode {
	case 401:
		return "authentication failed — check your API key"
	case 403:
		return "access denied — check your API key permissions"
	case 404:
		return "model not found — check your model configuration"
	case 429:
		return "rate limit exceeded — please wait and try again"
	case 500, 502, 503:
		return "provider is experiencing issues — will retry"
	default:
		return fmt.Sprintf("API error (status %d)", e.StatusCode)
	}
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

func (e *ClientError) UserMessage() string {
	return e.Message
}

// Client is an HTTP client for OpenAI-compatible chat completions endpoints.
type Client struct {
	config       *ModelConfig
	configMu     sync.RWMutex
	budget       *Budget
	httpClient   *http.Client
	logger       *slog.Logger
	metricsStore *metrics.Store
	timeoutCalc  *metrics.Calculator
	tokenCache   ResponseCache
	keyBuilder   *CacheKeyBuilder
	tokenResolver       TokenResolver
	oauthProvider       string
	extraHeaders        map[string]string
}

// ClientOption is a functional option for configuring a Client.
type ClientOption func(*Client)

// toolCallAccum accumulates tool call data across stream chunks and retry attempts.
type toolCallAccum struct {
	ID        string
	Name      string
	Arguments strings.Builder
}

// streamRetryState tracks state across stream retry attempts.
// D4: Used for retry with resume capability.
type streamRetryState struct {
	// lastEventID tracks the last successfully processed event for resume
	lastEventID string
	// accumulated content from prior attempts
	accumulated strings.Builder
	// tool call accumulators from prior attempts
	toolCallAccums map[int]*toolCallAccum
	// usage from prior attempts
	usage TokenUsage
	// deltasSent counts how many deltas were sent to the callback
	deltasSent int
	// isResume is true if this attempt should resume from lastEventID
	isResume bool
}

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

// SetMetricsStore sets the metrics store after client creation.
// This is used when the metrics store is created after the client
// (e.g. in daemon wiring where the store lives in daemon.go).
func (c *Client) SetMetricsStore(store *metrics.Store) {
	if store != nil {
		c.metricsStore = store
	}
}

// WithTimeoutCalculator sets the adaptive timeout calculator for the client.
func WithTimeoutCalculator(calc *metrics.Calculator) ClientOption {
	return func(c *Client) {
		c.timeoutCalc = calc
	}
}

// WithTokenCache sets the token cache for the client.
func WithTokenCache(cache ResponseCache) ClientOption {
	return func(c *Client) {
		if cache != nil {
			c.tokenCache = cache
			c.keyBuilder = NewCacheKeyBuilder(true) // Enable file-aware caching
		}
	}
}

// WithTokenResolver sets the OAuth token resolver and provider name for the
// client. When set, the client resolves a fresh access token from the resolver
// before each request and uses it as the Bearer token. A nil resolver is
// safely ignored.
func WithTokenResolver(tr TokenResolver, provider string) ClientOption {
	return func(c *Client) {
		if tr != nil {
			c.tokenResolver = tr
			c.oauthProvider = provider
		}
	}
}

// WithExtraHeaders sets additional HTTP headers sent with every request.
// For example, GitHub Models requires X-GitHub-Api-Version. A nil map is
// safely ignored.
func WithExtraHeaders(headers map[string]string) ClientOption {
	return func(c *Client) {
		if headers != nil {
			c.extraHeaders = headers
		}
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
	c.configMu.RLock()
	cfg := c.config
	c.configMu.RUnlock()

	// Apply chat options, starting with model defaults
	chatOpts := &chatOptions{
		temperature:      cfg.Temperature,
		maxTokens:        cfg.MaxTokens,
		topP:             cfg.TopP,
		frequencyPenalty: cfg.FrequencyPenalty,
		presencePenalty:  cfg.PresencePenalty,
		stopSequences:    cfg.StopSequences,
	}
	for _, opt := range opts {
		opt(chatOpts)
	}

	// Check cache
	if c.tokenCache != nil && c.keyBuilder != nil {
		cacheKey := c.keyBuilder.Build("", cfg.ModelID, messages)
		if cached, found := c.tokenCache.Get(ctx, cacheKey); found {
			return cached.Response, nil
		}
	}

	// Budget gate
	if c.budget != nil {
		result := c.budget.CheckBudgetWithScope(chatOpts.taskID, chatOpts.sessionID)
		if result.Exceeded {
			return nil, &BudgetExceededError{
				Message: result.Reason.Message(result.Used, result.Limit),
				Reason:  result.Reason,
				Used:    result.Used,
				Limit:   result.Limit,
			}
		}
		if err := c.budget.WaitForRateLimit(ctx); err != nil {
			return nil, err
		}
	}

	// Compute adaptive timeout if available (LLM-3 FIX: use per-request context timeout instead of mutating shared httpClient.Timeout)
	if c.timeoutCalc != nil {
		estimatedTokens := chatOpts.maxTokens
		if estimatedTokens <= 0 {
			estimatedTokens = 4096 // Safe default
		}
		timeout := c.timeoutCalc.Calculate(ctx, cfg.ProviderID, cfg.ModelID, estimatedTokens, defaultTimeout)
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	// Build request payload
	msgDicts := make([]map[string]any, len(messages))
	for i, msg := range messages {
		msgDicts[i] = msg.ToOpenAIDict()
	}

	payload := map[string]any{
		"model":       cfg.ModelID,
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
			var rlErr *RateLimitError
			if errors.As(err, &rlErr) {
				apiErr = &APIError{StatusCode: http.StatusTooManyRequests}
				// rlErr already wraps the APIError cause
			} else if !errors.As(err, &apiErr) || !retryableStatusCodes[apiErr.StatusCode] {
				return nil, err
			}

			c.logger.Warn("Retryable error",
				"status", apiErr.StatusCode,
				"attempt", attempt,
				"max_retries", maxRetries,
			)
			lastErr = err
			if attempt < maxRetries {
				// Respect Retry-After from rate limit errors if available.
				sleepDuration := time.Duration(0)
				if rlErr != nil && rlErr.RetryAfter > 0 {
					sleepDuration = rlErr.RetryAfter
				}
				// Fall back to exponential backoff with jitter.
				if sleepDuration == 0 {
					expDelay := time.Duration(math.Pow(retryBackoffBase, float64(attempt)) * float64(time.Second))
					sleepDuration = BackoffWithJitter(expDelay, retryBackoffMaxDelay, true)
				}
				c.logger.Debug("Retry backoff",
					"attempt", attempt,
					"sleep", sleepDuration,
				)
				select {
				case <-time.After(sleepDuration):
					continue
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
			continue
		}

		// Record usage with scope
		if c.budget != nil {
			c.budget.RecordUsageWithScope(resp.Usage, chatOpts.taskID, chatOpts.sessionID)
			// Record cost with scope if model pricing is available
			if c.config != nil {
				costUSD := float64(resp.Usage.PromptTokens)*c.config.CostPerMillionInput/1_000_000 + float64(resp.Usage.CompletionTokens)*c.config.CostPerMillionOutput/1_000_000
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

		// Store in cache
		if c.tokenCache != nil && c.keyBuilder != nil {
			cacheKey := c.keyBuilder.Build("", cfg.ModelID, messages)
			c.tokenCache.Put(ctx, cacheKey, resp)
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

	c.configMu.RLock()
	cfg := c.config
	c.configMu.RUnlock()

	// Apply chat options, starting with model defaults
	chatOpts := &chatOptions{
		temperature:      cfg.Temperature,
		maxTokens:        cfg.MaxTokens,
		topP:             cfg.TopP,
		frequencyPenalty: cfg.FrequencyPenalty,
		presencePenalty:  cfg.PresencePenalty,
		stopSequences:    cfg.StopSequences,
	}
	for _, opt := range opts {
		opt(chatOpts)
	}

	// Check cache
	if c.tokenCache != nil && c.keyBuilder != nil {
		cacheKey := c.keyBuilder.Build("", cfg.ModelID, messages)
		if cached, found := c.tokenCache.Get(ctx, cacheKey); found {
			reportProgress(ProgressStageDone, "Cache hit")
			return cached.Response, nil
		}
	}

	// Budget gate
	if c.budget != nil {
		reportProgress(ProgressStageStarting, "Checking token budget...")
		result := c.budget.CheckBudgetWithScope(chatOpts.taskID, chatOpts.sessionID)
		if result.Exceeded {
			return nil, &BudgetExceededError{
				Message: result.Reason.Message(result.Used, result.Limit),
				Reason:  result.Reason,
				Used:    result.Used,
				Limit:   result.Limit,
			}
		}

		reportProgress(ProgressStageStarting, "Waiting for rate limit...")
		if err := c.budget.WaitForRateLimit(ctx); err != nil {
			return nil, err
		}
	}

	// Compute adaptive timeout if available (mirrors Chat() logic)
	if c.timeoutCalc != nil {
		estimatedTokens := chatOpts.maxTokens
		if estimatedTokens <= 0 {
			estimatedTokens = 4096 // Safe default
		}
		timeout := c.timeoutCalc.Calculate(ctx, cfg.ProviderID, cfg.ModelID, estimatedTokens, defaultTimeout)
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	// Build request payload
	msgDicts := make([]map[string]any, len(messages))
	for i, msg := range messages {
		msgDicts[i] = msg.ToOpenAIDict()
	}

	payload := map[string]any{
		"model":       cfg.ModelID,
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
			var rlErr *RateLimitError
			if errors.As(err, &rlErr) {
				apiErr = &APIError{StatusCode: http.StatusTooManyRequests}
			} else if !errors.As(err, &apiErr) || !retryableStatusCodes[apiErr.StatusCode] {
				reportProgress(ProgressStageDone, fmt.Sprintf("Error: %v", err))
				return nil, err
			}

			c.logger.Warn("Retryable error",
				"status", apiErr.StatusCode,
				"attempt", attempt,
				"max_retries", maxRetries,
			)
			lastErr = err
			if attempt < maxRetries {
				sleepDuration := time.Duration(0)
				if rlErr != nil && rlErr.RetryAfter > 0 {
					sleepDuration = rlErr.RetryAfter
				}
				if sleepDuration == 0 {
					expDelay := time.Duration(math.Pow(retryBackoffBase, float64(attempt)) * float64(time.Second))
					sleepDuration = BackoffWithJitter(expDelay, retryBackoffMaxDelay, true)
				}
				c.logger.Debug("Retry backoff",
					"attempt", attempt,
					"sleep", sleepDuration,
				)
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

		// Streaming stage - response received
		reportProgress(ProgressStageStreaming, "Receiving response...")

		// Record usage with scope
		if c.budget != nil {
			c.budget.RecordUsageWithScope(resp.Usage, chatOpts.taskID, chatOpts.sessionID)
			// Record cost with scope if model pricing is available
			if c.config != nil {
				costUSD := float64(resp.Usage.PromptTokens)*c.config.CostPerMillionInput/1_000_000 + float64(resp.Usage.CompletionTokens)*c.config.CostPerMillionOutput/1_000_000
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

		// Store in cache
		if c.tokenCache != nil && c.keyBuilder != nil {
			cacheKey := c.keyBuilder.Build("", cfg.ModelID, messages)
			c.tokenCache.Put(ctx, cacheKey, resp)
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
	taskID           string
	sessionID        string
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

// WithTaskScope sets the task and session scope for budget tracking.
func WithTaskScope(taskID, sessionID string) ChatOption {
	return func(o *chatOptions) {
		o.taskID = taskID
		o.sessionID = sessionID
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
	c.configMu.RLock()
	baseURL := strings.TrimSuffix(c.config.BaseURL, "/")
	modelID := c.config.ModelID
	apiKey := c.config.APIKey
	providerID := c.config.ProviderID
	extraHeaders := c.extraHeaders
	c.configMu.RUnlock()
	url := baseURL + "/chat/completions"

	// Log request for diagnosis
	c.logger.Debug("Making LLM request", "url", url, "model", modelID, "payload_len", len(body), "messages_count", len(payload["messages"].([]map[string]any)))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, &ClientError{Message: "failed to create request", Cause: err}
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	// Resolve OAuth token if a token resolver is configured.
	if c.tokenResolver != nil && c.oauthProvider != "" {
		token, err := c.tokenResolver.ResolveToken(ctx, c.oauthProvider)
		if err != nil {
			return nil, &ClientError{Message: "failed to resolve OAuth token", Cause: err}
		}
		req.Header.Set("Authorization", "Bearer "+token)
	} else if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	// Apply extra headers (e.g. X-GitHub-Api-Version for GitHub Models).
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	// Time the HTTP request
	start := time.Now()
	resp, err := c.httpClient.Do(req) //nolint:bodyclose // resp.Body closed below when resp != nil
	latencyMs := time.Since(start).Milliseconds()

	if resp != nil {
		defer resp.Body.Close()
	}

	// Record error metrics only here; successful requests are recorded after parsing
	// with actual token counts (see below after parseResponse)
	if c.metricsStore != nil && (err != nil || (resp != nil && resp.StatusCode != http.StatusOK)) {
		errType := metrics.ErrorTypeNone
		if err != nil {
			errType = metrics.ClassifyError(err, 0)
		} else if resp != nil {
			errType = metrics.ClassifyError(nil, resp.StatusCode)
		}
		httpStatus := 0
		if resp != nil {
			httpStatus = resp.StatusCode
		}
		//nolint:gosec // goroutine outlives request context
		go func() {
			record := metrics.RequestRecord{
				Timestamp:  time.Now(),
				ProviderID: providerID,
				ModelID:    modelID,
				LatencyMs:  latencyMs,
				HTTPStatus: httpStatus,
				ErrorType:  errType,
				Success:    false,
				CostUSD:    0, // no usage data on error path
			}
			if rerr := c.metricsStore.Record(context.Background(), record); rerr != nil {
				c.logger.Debug("metrics record failed", "error", rerr)
			}
		}()
	}

	if err != nil {
		return nil, &ClientError{Message: "request failed", Cause: err}
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &ClientError{Message: "failed to read response", Cause: err}
	}

	// Log response body preview at debug level for diagnosis
	bodyPreview := string(respBody)
	if len(bodyPreview) > 500 {
		bodyPreview = bodyPreview[:500] + "..."
	}
	c.logger.Debug("LLM response received", "status", resp.StatusCode, "content_type", resp.Header.Get("Content-Type"), "body_preview", bodyPreview)

	// Check for rate limit (429) specifically
	if resp.StatusCode == http.StatusTooManyRequests {
		detail := string(respBody)
		if len(detail) > 500 {
			detail = detail[:500]
		}
		retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))

		// Try to parse structured error metadata from the response body.
		var providerDetail *ProviderErrorDetail
		if len(respBody) > 0 {
			providerDetail = ParseRateLimitBody(respBody)
		}

		rlErr := &RateLimitError{
			ProviderID: providerID,
			ModelID:    modelID,
			RetryAfter: retryAfter,
			Cause:      &APIError{StatusCode: resp.StatusCode, Detail: detail},
		}

		if providerDetail != nil {
			// Use provider-suggested retry-after if header was absent
			if retryAfter == 0 && providerDetail.RetryAfter > 0 {
				rlErr.RetryAfter = providerDetail.RetryAfter
			}
			if providerDetail.RetryStrategy != nil && providerDetail.RetryStrategy.Type != "" {
				rlErr.LimitType = providerDetail.RetryStrategy.Type
			} else if providerDetail.LimitBudget != nil {
				rlErr.LimitType = providerDetail.LimitBudget.Window
			}
			rlErr.RetryStrategy = providerDetail.RetryStrategy
			rlErr.LimitBudget = providerDetail.LimitBudget
		}

		return nil, rlErr
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
		costUSD := float64(chatResp.Usage.PromptTokens)*c.config.CostPerMillionInput/1_000_000 + float64(chatResp.Usage.CompletionTokens)*c.config.CostPerMillionOutput/1_000_000
		//nolint:gosec // goroutine outlives request context
		go func() {
			record := metrics.RequestRecord{
				Timestamp:        time.Now(),
				ProviderID:       providerID,
				ModelID:          modelID,
				PromptTokens:     chatResp.Usage.PromptTokens,
				CompletionTokens: chatResp.Usage.CompletionTokens,
				CachedTokens:     chatResp.Usage.PromptTokensDetails.CachedTokens,
				LatencyMs:        latencyMs,
				HTTPStatus:       resp.StatusCode,
				ErrorType:        metrics.ErrorTypeNone,
				Success:          true,
				CostUSD:          costUSD,
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
	content = msg.ContentString()

	var toolCalls []ToolCall
	if len(msg.ToolCalls) > 0 {
		toolCalls = make([]ToolCall, len(msg.ToolCalls))
		for i, tc := range msg.ToolCalls {
			toolCalls[i] = tc.ToToolCall()
		}
	}

	model := chatResp.Model
	if model == "" {
		c.configMu.RLock()
		model = c.config.ModelID
		c.configMu.RUnlock()
	}

	return &Response{
		Content:   content,
		ToolCalls: toolCalls,
		Usage: TokenUsage{
			PromptTokens:     chatResp.Usage.PromptTokens,
			CompletionTokens: chatResp.Usage.CompletionTokens,
			TotalTokens:      chatResp.Usage.TotalTokens,
			CachedTokens:     chatResp.Usage.PromptTokensDetails.CachedTokens,
		},
		Model:        model,
		FinishReason: choice.FinishReason,
	}, nil
}

// ChatWithDeltaCallback sends a streaming chat completion request and invokes
// onDelta for each content chunk. If onDelta returns a non-nil error, the
// stream is cancelled and that error is returned. The final accumulated
// Response is returned on successful completion.
// D4: Added retry with resume capability for transient errors.
func (c *Client) ChatWithDeltaCallback(ctx context.Context, messages []ChatMessage, onDelta DeltaCallback, opts ...ChatOption) (*Response, error) {
	if onDelta == nil {
		// Fallback to non-streaming when no callback provided
		return c.Chat(ctx, messages, opts...)
	}

	c.configMu.RLock()
	cfg := c.config
	c.configMu.RUnlock()

	chatOpts := &chatOptions{
		temperature:      cfg.Temperature,
		maxTokens:        cfg.MaxTokens,
		topP:             cfg.TopP,
		frequencyPenalty: cfg.FrequencyPenalty,
		presencePenalty:  cfg.PresencePenalty,
		stopSequences:    cfg.StopSequences,
	}
	for _, opt := range opts {
		opt(chatOpts)
	}

	msgDicts := make([]map[string]any, len(messages))
	for i, msg := range messages {
		msgDicts[i] = msg.ToOpenAIDict()
	}

	payload := map[string]any{
		"model":       cfg.ModelID,
		"messages":    msgDicts,
		"temperature": chatOpts.temperature,
		"max_tokens":  chatOpts.maxTokens,
		"stream":      true,
	}
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

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, &ClientError{Message: "failed to marshal request", Cause: err}
	}

	// Budget gate with scope
	if c.budget != nil {
		result := c.budget.CheckBudgetWithScope(chatOpts.taskID, chatOpts.sessionID)
		if result.Exceeded {
			return nil, &BudgetExceededError{
				Message: result.Reason.Message(result.Used, result.Limit),
				Reason:  result.Reason,
				Used:    result.Used,
				Limit:   result.Limit,
			}
		}
		if err := c.budget.WaitForRateLimit(ctx); err != nil {
			return nil, err
		}
	}

	// D4: Retry loop for transient errors with resume capability
	var lastErr error
	retryState := &streamRetryState{
		toolCallAccums: make(map[int]*toolCallAccum),
	}

	for attempt := 0; attempt < streamMaxRetries; attempt++ {
		if attempt > 0 {
			// D4: Set resume flag for retry attempts
			retryState.isResume = true
			c.logger.Debug("stream retry attempt", "attempt", attempt+1, "max", streamMaxRetries)
		}

		resp, httpResp, err := c.doStreamRequest(ctx, body, onDelta, retryState)
		if err == nil {
			// Record usage with scope
			if c.budget != nil && resp != nil {
				c.budget.RecordUsageWithScope(resp.Usage, chatOpts.taskID, chatOpts.sessionID)
				// Record cost with scope if model pricing is available
				if c.config != nil {
					costUSD := float64(resp.Usage.PromptTokens)*c.config.CostPerMillionInput/1_000_000 + float64(resp.Usage.CompletionTokens)*c.config.CostPerMillionOutput/1_000_000
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

			// Record metrics on success
			if c.metricsStore != nil {
				costUSD := float64(0)
				go func() {
					record := metrics.RequestRecord{
						Timestamp:  time.Now(),
						ProviderID: cfg.ProviderID,
						ModelID:    cfg.ModelID,
						LatencyMs:  0,
						HTTPStatus: httpResp.StatusCode,
						ErrorType:  metrics.ErrorTypeNone,
						Success:    true,
						CostUSD:    costUSD,
					}
					_ = c.metricsStore.Record(context.Background(), record)
				}()
			}
			return resp, nil
		}
		lastErr = err

		// D4: Check if error is retryable
		if !isRetryableStreamingError(err) {
			c.logger.Debug("non-retryable stream error", "error", err)
			return nil, err
		}

		// D4: Don't retry if we're on the last attempt
		if attempt >= streamMaxRetries-1 {
			break
		}

		// D4: Calculate backoff with exponential delay and Retry-After
		backoff := time.Duration(1<<uint(attempt)) * time.Second // 2s, 4s, 8s
		if rlErr, ok := err.(*RateLimitError); ok && rlErr.RetryAfter > 0 {
			backoff = rlErr.RetryAfter
			c.logger.Debug("using Retry-After from rate limit response", "retry_after", backoff)
		}

		select {
		case <-time.After(backoff):
			// Continue to next attempt
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	return nil, fmt.Errorf("streaming failed after %d attempts: %w", streamMaxRetries, lastErr)
}


// doStreamRequest performs a single streaming HTTP request and invokes onDelta for each chunk.
// D4: Extracted to enable retry with resume capability.
// retryState tracks state from prior attempts (accumulated content, tool calls, usage).
// If retryState.isResume is true, the request includes Last-Event-ID header for resume.
func (c *Client) doStreamRequest(ctx context.Context, body []byte, onDelta DeltaCallback, retryState *streamRetryState) (*Response, *http.Response, error) {
	c.configMu.RLock()
	baseURL := strings.TrimSuffix(c.config.BaseURL, "/")
	modelID := c.config.ModelID
	apiKey := c.config.APIKey
	providerID := c.config.ProviderID
	extraHeaders := c.extraHeaders
	c.configMu.RUnlock()
	url := baseURL + "/chat/completions"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		c.logger.Debug("stream request failed at creation", "error", err)
		return nil, nil, &ClientError{Message: "failed to create request", Cause: err}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	// D4: Add Last-Event-ID header for resume on retry attempts
	if retryState != nil && retryState.isResume && retryState.lastEventID != "" {
		req.Header.Set("Last-Event-ID", retryState.lastEventID)
		c.logger.Debug("stream resume", "last_event_id", retryState.lastEventID)
	}

	// Resolve OAuth token if a token resolver is configured.
	if c.tokenResolver != nil && c.oauthProvider != "" {
		token, err := c.tokenResolver.ResolveToken(ctx, c.oauthProvider)
		if err != nil {
			return nil, nil, &ClientError{Message: "failed to resolve OAuth token", Cause: err}
		}
		req.Header.Set("Authorization", "Bearer "+token)
	} else if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	// Apply extra headers (e.g. X-GitHub-Api-Version for GitHub Models).
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Debug("stream request failed", "error", err)
		return nil, nil, &ClientError{Message: "request failed", Cause: err}
	}

	if resp.StatusCode != http.StatusOK {
		retryAfter := extractRetryAfter(resp)
		detail, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		apiErr := &APIError{StatusCode: resp.StatusCode, Detail: string(detail)}
		// Wrap in RateLimitError for 429 to preserve Retry-After
		if resp.StatusCode == 429 && retryAfter > 0 {
			return nil, nil, &RateLimitError{
				ProviderID: providerID,
				ModelID:    modelID,
				RetryAfter: retryAfter,
				Cause:      apiErr,
			}
		}
		// S3-9 FIX: return nil resp — body is already closed; returning it
		// would let callers dereference a closed-body http.Response.
		return nil, nil, apiErr
	}

	// Parse stream
	var accumulated strings.Builder
	var finishReason string
	var usage TokenUsage

	// Pre-populate from retryState if resuming
	if retryState != nil && retryState.accumulated.Len() > 0 {
		accumulated.WriteString(retryState.accumulated.String())
		usage = retryState.usage
	}

	scanner := bufio.NewScanner(resp.Body)
	// Pre-populate tool calls from retryState
	toolCallAccums := make(map[int]*toolCallAccum)
	if retryState != nil && retryState.toolCallAccums != nil {
		for idx, accum := range retryState.toolCallAccums {
			toolCallAccums[idx] = &toolCallAccum{
				ID:   accum.ID,
				Name: accum.Name,
			}
			toolCallAccums[idx].Arguments.WriteString(accum.Arguments.String())
		}
	}

	deltasSent := 0
	if retryState != nil {
		deltasSent = retryState.deltasSent
	}

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			resp.Body.Close()
			// S3-9 FIX: body is now closed; don't return resp.
			return nil, nil, ctx.Err()
		default:
		}

		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					Role      string `json:"role"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Type     string `json:"type"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
			Usage *struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
				TotalTokens      int `json:"total_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			c.logger.Warn("failed to parse stream chunk", "error", err, "data", data)
			continue
		}
		if len(chunk.Choices) == 0 {
			continue
		}

		// Handle content delta
		delta := chunk.Choices[0].Delta.Content
		if delta != "" {
			accumulated.WriteString(delta)
			// Skip deltas already sent (on resume)
			if retryState == nil || !retryState.isResume || deltasSent >= retryState.deltasSent {
				if err := onDelta(delta); err != nil {
					resp.Body.Close()
					// S3-9 FIX: body is now closed; don't return resp.
					return nil, nil, err
				}
				deltasSent++
			}
		}

		// Handle tool call deltas
		for _, tcDelta := range chunk.Choices[0].Delta.ToolCalls {
			idx := tcDelta.Index
			if accum, exists := toolCallAccums[idx]; exists {
				accum.Arguments.WriteString(tcDelta.Function.Arguments)
			} else {
				toolCallAccums[idx] = &toolCallAccum{
					ID:   tcDelta.ID,
					Name: tcDelta.Function.Name,
				}
				toolCallAccums[idx].Arguments.WriteString(tcDelta.Function.Arguments)
			}
		}

		// Capture usage from final chunk
		if chunk.Usage != nil {
			usage = TokenUsage{
				PromptTokens:     chunk.Usage.PromptTokens,
				CompletionTokens: chunk.Usage.CompletionTokens,
				TotalTokens:      chunk.Usage.TotalTokens,
			}
		}

		if chunk.Choices[0].FinishReason != nil {
			finishReason = *chunk.Choices[0].FinishReason
		}
	}
	resp.Body.Close()

	if err := scanner.Err(); err != nil {
		// S3-9 FIX: body is already closed above; don't return resp.
		return nil, nil, &ClientError{Message: "stream read failed", Cause: err}
	}

	// Build tool calls from accumulators
	var toolCalls []ToolCall
	for _, accum := range toolCallAccums {
		toolCalls = append(toolCalls, ToolCall{
			ID:   accum.ID,
			Type: "function",
			Function: ToolCallFunction{
				Name:      accum.Name,
				Arguments: accum.Arguments.String(),
			},
		})
	}

	result := &Response{
		Content:      accumulated.String(),
		ToolCalls:    toolCalls,
		Usage:        usage,
		Model:        modelID,
		FinishReason: finishReason,
	}

	return result, resp, nil
}

// SwitchModel switches to a different model/endpoint at runtime.
func (c *Client) SwitchModel(config *ModelConfig) error {
	if config == nil {
		return &ClientError{Message: "SwitchModel: config must not be nil"}
	}
	c.configMu.Lock()
	c.config = config
	c.configMu.Unlock()
	c.logger.Info("Switched model",
		"model", config.ModelID,
		"base_url", config.BaseURL,
	)
	return nil
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
	c.configMu.RLock()
	defer c.configMu.RUnlock()
	return c.config
}

// isRetryableStreamingError returns true for transient errors that warrant stream retry.
// D4: Used for ChatWithDeltaCallback retry logic.
func isRetryableStreamingError(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == 429 || apiErr.StatusCode == 502 || apiErr.StatusCode == 503 || apiErr.StatusCode == 504
	}
	var clientErr *ClientError
	if errors.As(err, &clientErr) {
		errStr := clientErr.Error()
		return strings.Contains(errStr, "stream") || strings.Contains(errStr, "broken pipe") || strings.Contains(errStr, "unexpected EOF")
	}
	return false
}

// extractRetryAfter extracts Retry-After duration from HTTP response headers.
// D4: Parses Retry-After header before creating APIError.
func extractRetryAfter(resp *http.Response) time.Duration {
	if resp == nil {
		return 0
	}
	header := resp.Header.Get("Retry-After")
	if header == "" {
		return 0
	}
	// Try parsing as seconds
	if seconds, err := strconv.Atoi(header); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	// Try parsing as RFC1123 date
	if t, err := time.Parse(time.RFC1123, header); err == nil {
		duration := time.Until(t)
		if duration > 0 {
			return duration
		}
	}
	return 0
}


// Budget returns the token budget tracker, if one is configured.
func (c *Client) Budget() *Budget {
	return c.budget
}
