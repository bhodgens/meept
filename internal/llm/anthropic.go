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
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/llm/metrics"
)

const (
	anthropicDefaultTimeout = 5 * time.Minute
	anthropicAPIVersion     = "2023-06-01"
	// D4/D8 (leaf 03): anthropicMaxRetries / anthropicRetryBackoff and the
	// 30s retryBackoffMaxDelay cap are gone — both loops take their count
	// from FailurePolicyConfig.ShortRetries (nil-safe default 3,
	// defaultShortRetries in client.go) and their schedule from
	// DefaultBackoffPlan + ParseRetryAfter.
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
	configMu      sync.RWMutex
	config        *ModelConfig
	budget        *Budget
	httpClient    *http.Client
	logger        *slog.Logger
	metricsStore  *metrics.Store
	timeoutCalc   *metrics.Calculator
	tokenCache    ResponseCache
	keyBuilder    *CacheKeyBuilder
	uploadStore   UploadStore
	tokenResolver TokenResolver
	oauthProvider string
	// quotaMaxWait is the upper bound applied to derived quota waits. Zero
	// falls back to DefaultQuotaMaxWait. See Client.quotaMaxWait.
	quotaMaxWait time.Duration
	// failurePolicy is the injected tree-02 policy config (leaf 03 Task 5):
	// ShortRetries bounds both anthropic retry loops; nil-safe default 3.
	failurePolicy *FailurePolicyConfig
}

// SetQuotaMaxWait sets the quota wait upper bound. Nil-receiver safe.
func (c *AnthropicClient) SetQuotaMaxWait(d time.Duration) {
	if c == nil || d <= 0 {
		return
	}
	c.quotaMaxWait = d
}

// SetFailurePolicyConfig injects the tree-02 failure-policy config (leaf 03
// Task 5). Mirrors Client.SetFailurePolicyConfig: nil-safe, and a non-
// positive ShortRetries falls back to the default 3.
func (c *AnthropicClient) SetFailurePolicyConfig(cfg *FailurePolicyConfig) {
	if c == nil {
		return
	}
	if cfg != nil && cfg.ShortRetries <= 0 {
		clone := *cfg
		clone.ShortRetries = defaultShortRetries
		c.failurePolicy = &clone
		return
	}
	c.failurePolicy = cfg
}

// shortRetryBudget returns the loop retry count: the injected config value
// or the nil-safe default (leaf 03 Task 5).
func (c *AnthropicClient) shortRetryBudget() int {
	if c != nil && c.failurePolicy != nil && c.failurePolicy.ShortRetries > 0 {
		return c.failurePolicy.ShortRetries
	}
	return defaultShortRetries
}

// policyCfg resolves the injected policy config or the package defaults.
func (c *AnthropicClient) policyCfg() FailurePolicyConfig {
	if c != nil && c.failurePolicy != nil {
		return *c.failurePolicy
	}
	return FailurePolicyConfig{
		Horizon:      24 * time.Hour,
		BaseThrottle: 30 * time.Second,
		PollFloor:    time.Hour,
	}
}

// quotaDetailFromBody returns a truncated body detail for quota error causes,
// preferring the parsed Anthropic error message when present.
func (c *AnthropicClient) quotaDetailFromBody(respBody []byte) string {
	var anthErr anthropicErrorResponse
	if err := json.Unmarshal(respBody, &anthErr); err == nil && anthErr.Error.Message != "" {
		return anthErr.Error.Message
	}
	bodyStr := string(respBody)
	if len(bodyStr) > 500 {
		bodyStr = bodyStr[:500]
	}
	return bodyStr
}

// AnthropicClientOption is a functional option for configuring an AnthropicClient.
type AnthropicClientOption func(*AnthropicClient)

// WithAnthropicBudget sets the token budget for the client.
func WithAnthropicBudget(budget *Budget) AnthropicClientOption {
	return func(c *AnthropicClient) {
		c.budget = budget
	}
}

// WithAnthropicTokenResolver sets the OAuth token resolver and provider
// name for subscription (Bearer) auth. The resolver takes precedence over
// the static API key. A nil resolver is ignored.
func WithAnthropicTokenResolver(tr TokenResolver, provider string) AnthropicClientOption {
	return func(c *AnthropicClient) {
		if tr != nil {
			c.tokenResolver = tr
			c.oauthProvider = provider
		}
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

// WithAnthropicUploadStore sets the upload store for resolving image file references.
func WithAnthropicUploadStore(store UploadStore) AnthropicClientOption {
	return func(c *AnthropicClient) {
		if store != nil {
			c.uploadStore = store
		}
	}
}

// NewAnthropicClient creates a new Anthropic API client.
func NewAnthropicClient(config *ModelConfig, opts ...AnthropicClientOption) *AnthropicClient {
	if config == nil {
		config = &ModelConfig{}
	}
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

// isAnthropicRoute reports whether a model config targets an Anthropic-
// compatible endpoint (direct, OpenRouter-claude, or Bedrock-claude).
// Used to decide whether the Anthropic wire format applies.
func isAnthropicRoute(cfg *ModelConfig) bool {
	if cfg == nil {
		return false
	}
	if cfg.ProviderID == ProviderIDAnthropic {
		return true
	}
	if strings.Contains(strings.ToLower(cfg.BaseURL), "anthropic") {
		return true
	}
	// Bedrock and OpenRouter host Claude models; route by model-id prefix.
	if cfg.ProviderID == ProviderIDBedrock || cfg.ProviderID == "openrouter" {
		mid := strings.ToLower(cfg.ModelID)
		if strings.Contains(mid, "claude") || strings.HasPrefix(mid, "anthropic/") || strings.HasPrefix(mid, "anthropic.") {
			return true
		}
	}
	return false
}

// anthropicRequestURL constructs the request URL honoring provider quirks.
// Bedrock uses /model/{modelId}/invoke[_with_response_stream]; all others
// use {baseURL}/v1/messages. OpenRouter and similar providers whose BaseURL
// already ends in /v1 have that suffix stripped to avoid a doubled /v1/v1/.
func (c *AnthropicClient) anthropicRequestURL(streaming bool) string {
	base := strings.TrimSuffix(c.config.BaseURL, "/")
	if c.config.ProviderID == ProviderIDBedrock {
		suffix := "invoke"
		if streaming {
			// Hyphenated form per the Bedrock InvokeModelWithResponseStream
			// REST route (the underscored variant does not exist).
			suffix = "invoke-with-response-stream"
		}
		// url.PathEscape preserves ':' (valid pchar per RFC 3986) so
		// "anthropic.claude-sonnet-4-6-v2:0" round-trips correctly.
		return base + "/model/" + url.PathEscape(c.config.ModelID) + "/" + suffix
	}
	// OpenRouter and other OpenAI-compatible gateways expose Anthropic
	// behind /api/v1; strip a trailing /v1 so appending /v1/messages
	// doesn't yield /v1/v1/messages.
	base = strings.TrimSuffix(base, "/v1")
	return base + "/v1/messages"
}

// Chat sends a chat completion request to Anthropic's Messages API.
func (c *AnthropicClient) Chat(ctx context.Context, messages []ChatMessage, opts ...ChatOption) (*Response, error) {
	// Capture config under lock to avoid race with SwitchModel
	c.configMu.RLock()
	cfg := c.config
	c.configMu.RUnlock()
	chatOpts := &chatOptions{
		temperature:   cfg.Temperature,
		maxTokens:     cfg.MaxTokens,
		topP:          cfg.TopP,
		stopSequences: cfg.StopSequences,
		// Note: Anthropic doesn't support frequency_penalty or presence_penalty
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

	// Compute adaptive timeout if a calculator is configured.
	// LLM-3 FIX: use per-request context timeout instead of mutating shared httpClient.Timeout
	if c.timeoutCalc != nil {
		estimatedTokens := chatOpts.maxTokens
		if estimatedTokens <= 0 {
			estimatedTokens = 4096
		}
		timeout := c.timeoutCalc.Calculate(
			ctx,
			cfg.ProviderID,
			cfg.ModelID,
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

	// D4 (leaf 03): classify-first policy engine, identical shape to the
	// openai loops. Quota exits immediately (quota-reset-resilience
	// contract 1, placement unchanged); throttle retries within the short
	// budget and escalates to ThrottleBackoffError; server errors keep the
	// bounded retry + ClientError shape; fatal returns immediately.
	shortRetries := c.shortRetryBudget()
	now := time.Now()
	plan := DefaultBackoffPlan(FailureThrottle, now, c.policyCfg())

	for attempt := 1; attempt <= shortRetries; attempt++ {
		resp, err := c.doRequest(ctx, reqBody)
		if err != nil {
			// Quota errors never re-enter the short-retry loop: the window
			// is hours, not seconds. Return immediately (quota-reset-
			// resilience contract 1).
			var quotaErr *QuotaResetError
			if errors.As(err, &quotaErr) {
				return nil, err
			}

			// D4/D7: classify FIRST. The request layer already resolved
			// quota shapes (rate_limit_error / quota_exceeded / 402) to
			// QuotaResetError, so a surviving error is a bare throttle or
			// server error.
			status, header := errorStatusAndHeader(err)
			verdict := Classify(status, header, nil, time.Now())

			switch verdict.Class {
			case FailureThrottle:
				// Short-horizon retries honoring the server schedule
				// (ParseRetryAfter over the plan step, capped by the plan);
				// exhaustion escalates to ThrottleBackoffError (D4/D8).
				lastErr = err
				c.logger.Warn("Retryable throttle",
					"status", status,
					"attempt", attempt,
					"max_retries", shortRetries,
				)
				if attempt < shortRetries {
					wait := shortThrottleSleep(header, plan, time.Now(), attempt-1)
					if rlErr, ok := err.(*RateLimitError); ok && rlErr.RetryAfter > 0 && rlErr.RetryAfter < wait {
						// Parity with the legacy loop: a smaller
						// server-suggested wait is honored.
						wait = rlErr.RetryAfter
					}
					c.logger.Debug("Retry backoff", "attempt", attempt, "sleep", wait)
					select {
					case <-time.After(wait):
						continue
					case <-ctx.Done():
						return nil, ctx.Err()
					}
				}
				return nil, &ThrottleBackoffError{
					ProviderID: c.config.ProviderID,
					ModelID:    c.config.ModelID,
					RetryAt:    plan.NextAttempt(time.Now(), attempt, errorRetryAt(err)),
					Attempt:    attempt,
					Cause:      err,
				}
			case FailureServerError:
				// Bounded retries via the plan; give up → the historical
				// ClientError shape (leaf Notes).
				lastErr = err
				c.logger.Warn("Retryable server error",
					"status", status,
					"attempt", attempt,
					"max_retries", shortRetries,
				)
				if attempt < shortRetries {
					wait := shortThrottleSleep(nil, plan, time.Now(), attempt-1)
					c.logger.Debug("Retry backoff", "attempt", attempt, "sleep", wait)
					select {
					case <-time.After(wait):
						continue
					case <-ctx.Done():
						return nil, ctx.Err()
					}
				}
				continue
			default:
				// FailureFatal and anything unclassifiable: return
				// immediately (existing behavior).
				return nil, err
			}
		}

		if c.budget != nil {
			c.budget.RecordUsageWithScope(resp.Usage, chatOpts.taskID, chatOpts.sessionID)
			// Record cost with scope if model pricing is available
			if cfg != nil {
				costUSD := float64(resp.Usage.PromptTokens)*cfg.CostPerMillionInput/1_000_000 + float64(resp.Usage.CompletionTokens)*cfg.CostPerMillionOutput/1_000_000
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

	// D8 exhaustion cap: throttle escalations already returned
	// ThrottleBackoffError above; reaching here means the remaining retries
	// were server errors — the historical ClientError shape stands.
	return nil, &ClientError{
		Message: fmt.Sprintf("All %d attempts failed", shortRetries),
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

	// Capture config under lock to avoid race with SwitchModel
	c.configMu.RLock()
	cfg := c.config
	c.configMu.RUnlock()

	reportProgress(ProgressStageStarting, "Starting Anthropic request...")

	chatOpts := &chatOptions{
		temperature:   cfg.Temperature,
		maxTokens:     cfg.MaxTokens,
		topP:          cfg.TopP,
		stopSequences: cfg.StopSequences,
		// Note: Anthropic doesn't support frequency_penalty or presence_penalty
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

	// Compute adaptive timeout if a calculator is configured.
	// LLM-3 FIX: use per-request context timeout instead of mutating shared httpClient.Timeout
	if c.timeoutCalc != nil {
		estimatedTokens := chatOpts.maxTokens
		if estimatedTokens <= 0 {
			estimatedTokens = 4096
		}
		timeout := c.timeoutCalc.Calculate(
			ctx,
			cfg.ProviderID,
			cfg.ModelID,
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
	supportsExtendedThinking := cfg.HasCapability("extended_thinking")
	if supportsExtendedThinking {
		reportProgress(ProgressStageThinking, "Model supports extended thinking")
	}

	var lastErr error

	// D4 (leaf 03): classify-first policy engine, identical shape to the
	// anthropic non-streaming loop. Streaming subtlety: the pre-first-token
	// gating is preserved exactly — doStreamingRequest only returns HTTP-
	// status errors before parseStreamingResponse runs, so a throttle that
	// survived the short retries surfaces as ThrottleBackoffError only
	// when no tokens flowed; a mid-stream failure returns its own error.
	shortRetries := c.shortRetryBudget()
	now := time.Now()
	plan := DefaultBackoffPlan(FailureThrottle, now, c.policyCfg())

	for attempt := 1; attempt <= shortRetries; attempt++ {
		if attempt > 1 {
			reportProgress(ProgressStageThinking, fmt.Sprintf("Retry attempt %d/%d...", attempt, shortRetries))
		} else {
			reportProgress(ProgressStageThinking, "Model is thinking...")
		}

		resp, err := c.doStreamingRequest(ctx, reqBody, reportProgress)
		if err != nil {
			// Quota errors never re-enter the short-retry loop (streaming
			// path): hours-scale window, return immediately.
			var quotaErr *QuotaResetError
			if errors.As(err, &quotaErr) {
				reportProgress(ProgressStageDone, fmt.Sprintf("Error: %v", err))
				return nil, err
			}

			// D4/D7: classify FIRST (quota already peeled off above).
			status, header := errorStatusAndHeader(err)
			verdict := Classify(status, header, nil, time.Now())

			switch verdict.Class {
			case FailureThrottle:
				// Short-horizon retries honoring the server schedule;
				// exhaustion escalates to ThrottleBackoffError (D4/D8).
				lastErr = err
				c.logger.Warn("Retryable throttle",
					"status", status,
					"attempt", attempt,
					"max_retries", shortRetries,
				)
				if attempt < shortRetries {
					wait := shortThrottleSleep(header, plan, time.Now(), attempt-1)
					if rlErr, ok := err.(*RateLimitError); ok && rlErr.RetryAfter > 0 && rlErr.RetryAfter < wait {
						// Parity with the legacy loop: a smaller
						// server-suggested wait is honored.
						wait = rlErr.RetryAfter
					}
					c.logger.Debug("Retry backoff", "attempt", attempt, "sleep", wait)
					select {
					case <-time.After(wait):
						continue
					case <-ctx.Done():
						reportProgress(ProgressStageDone, "Request cancelled")
						return nil, ctx.Err()
					}
				}
				reportProgress(ProgressStageDone, "Throttled: parked for backoff")
				return nil, &ThrottleBackoffError{
					ProviderID: c.config.ProviderID,
					ModelID:    c.config.ModelID,
					RetryAt:    plan.NextAttempt(time.Now(), attempt, errorRetryAt(err)),
					Attempt:    attempt,
					Cause:      err,
				}
			case FailureServerError:
				// Bounded retries via the plan; give up → the historical
				// ClientError shape (leaf Notes).
				lastErr = err
				c.logger.Warn("Retryable server error",
					"status", status,
					"attempt", attempt,
					"max_retries", shortRetries,
				)
				if attempt < shortRetries {
					wait := shortThrottleSleep(nil, plan, time.Now(), attempt-1)
					c.logger.Debug("Retry backoff", "attempt", attempt, "sleep", wait)
					select {
					case <-time.After(wait):
						continue
					case <-ctx.Done():
						reportProgress(ProgressStageDone, "Request cancelled")
						return nil, ctx.Err()
					}
				}
				continue
			default:
				// FailureFatal and anything unclassifiable: return
				// immediately (existing behavior).
				reportProgress(ProgressStageDone, fmt.Sprintf("Error: %v", err))
				return nil, err
			}
		}

		reportProgress(ProgressStageStreaming, "Receiving response...")

		if c.budget != nil {
			c.budget.RecordUsageWithScope(resp.Usage, chatOpts.taskID, chatOpts.sessionID)
			// Record cost with scope if model pricing is available
			if cfg != nil {
				costUSD := float64(resp.Usage.PromptTokens)*cfg.CostPerMillionInput/1_000_000 + float64(resp.Usage.CompletionTokens)*cfg.CostPerMillionOutput/1_000_000
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

		if resp.HasToolCalls() {
			reportProgress(ProgressStageToolCall, fmt.Sprintf("Response contains %d tool calls", len(resp.ToolCalls)))
		}

		reportProgress(ProgressStageDone, fmt.Sprintf("Complete: %d tokens", resp.Usage.TotalTokens))

		return resp, nil
	}

	reportProgress(ProgressStageDone, fmt.Sprintf("Failed after %d attempts", shortRetries))
	return nil, &ClientError{
		Message: fmt.Sprintf("All %d attempts failed", shortRetries),
		Cause:   lastErr,
	}
}

// Anthropic API request structures

// anthropicCacheControl marks a content block for prompt caching.
type anthropicCacheControl struct {
	Type string `json:"type"` // "ephemeral"
}

// anthropicSystemBlock is a single block in the system prompt array. When
// CacheControl is set, Anthropic caches the prefix up to and including this
// block.
type anthropicSystemBlock struct {
	Type         string                 `json:"type"` // "text"
	Text         string                 `json:"text"`
	CacheControl *anthropicCacheControl `json:"cache_control,omitempty"`
}

type anthropicRequest struct {
	Model         string                 `json:"model"`
	MaxTokens     int                    `json:"max_tokens"`
	System        string                 `json:"-"` // marshaled via MarshalJSON
	SystemBlocks  []anthropicSystemBlock `json:"-"` // marshaled via MarshalJSON
	Messages      []anthropicMessage     `json:"messages"`
	Tools         []anthropicTool        `json:"tools,omitempty"`
	Temperature   *float64               `json:"temperature,omitempty"`
	TopP          *float64               `json:"top_p,omitempty"`
	StopSequences []string               `json:"stop_sequences,omitempty"`
	Stream        bool                   `json:"stream,omitempty"`
	// AnthropicVersion is Bedrock-only: the API version travels in-band
	// ("bedrock-2023-05-31") rather than as the anthropic-version header.
	AnthropicVersion string `json:"anthropic_version,omitempty"`
	// Extended thinking configuration
	Thinking *anthropicThinkingConfig `json:"thinking,omitempty"`
}

// MarshalJSON implements custom marshaling so that the system field is emitted
// as either a plain string (System) or an array of content blocks
// (SystemBlocks), matching the Anthropic API's polymorphic system parameter.
func (r *anthropicRequest) MarshalJSON() ([]byte, error) {
	type requestAlias struct {
		Model            string                   `json:"model"`
		MaxTokens        int                      `json:"max_tokens"`
		System           json.RawMessage          `json:"system,omitempty"`
		Messages         []anthropicMessage       `json:"messages"`
		Tools            []anthropicTool          `json:"tools,omitempty"`
		Temperature      *float64                 `json:"temperature,omitempty"`
		TopP             *float64                 `json:"top_p,omitempty"`
		StopSequences    []string                 `json:"stop_sequences,omitempty"`
		Stream           bool                     `json:"stream,omitempty"`
		AnthropicVersion string                   `json:"anthropic_version,omitempty"`
		Thinking         *anthropicThinkingConfig `json:"thinking,omitempty"`
	}

	a := requestAlias{
		Model:            r.Model,
		MaxTokens:        r.MaxTokens,
		Messages:         r.Messages,
		Tools:            r.Tools,
		Temperature:      r.Temperature,
		TopP:             r.TopP,
		StopSequences:    r.StopSequences,
		Stream:           r.Stream,
		AnthropicVersion: r.AnthropicVersion,
		Thinking:         r.Thinking,
	}

	if len(r.SystemBlocks) > 0 {
		raw, err := json.Marshal(r.SystemBlocks)
		if err != nil {
			return nil, fmt.Errorf("marshal system blocks: %w", err)
		}
		a.System = raw
	} else if r.System != "" {
		raw, err := json.Marshal(r.System)
		if err != nil {
			return nil, fmt.Errorf("marshal system string: %w", err)
		}
		a.System = raw
	}

	return json.Marshal(a)
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
	// For images
	Source *anthropicImageSource `json:"source,omitempty"`
}

// anthropicImageSource holds base64-encoded image data for the Anthropic API.
type anthropicImageSource struct {
	Type      string `json:"type"`       // "base64"
	MediaType string `json:"media_type"` // "image/png", etc.
	Data      string `json:"data"`       // base64-encoded image bytes
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
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
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
			// EC-6: IsError is now set from the structured IsToolError field
			// (populated by ExecutionResult.ToChatMessage via Success bool)
			// instead of substring content matching.
			apiMessages = append(apiMessages, anthropicMessage{
				Role: "user",
				Content: []anthropicContent{{
					Type:      "tool_result",
					ToolUseID: msg.ToolCallID,
					Content:   msg.Content,
					IsError:   msg.IsToolError,
				}},
			})
		case RoleUser, RoleAssistant:
			msgIndexToAPIIndex[i] = len(apiMessages)
			if len(msg.Parts) > 0 {
				apiMessages = append(apiMessages, anthropicMessage{
					Role:    string(msg.Role),
					Content: c.partsToAnthropicContent(msg.Parts, c.uploadStore),
				})
			} else {
				apiMessages = append(apiMessages, anthropicMessage{
					Role: string(msg.Role),
					Content: []anthropicContent{{
						Type: ContentTypeText,
						Text: msg.Content,
					}},
				})
			}
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
			// B-12 FIX: Empty Arguments must produce valid JSON for the
			// Anthropic "input" field. An empty string is not valid JSON.
			// The streaming response path defaults to "{}" (see
			// buildResponseFromBlocks); mirror that here.
			input := json.RawMessage(tc.Function.Arguments)
			if len(input) == 0 {
				input = json.RawMessage("{}")
			}
			content = append(content, anthropicContent{
				Type:  ContentTypeToolUse,
				ID:    tc.ID,
				Name:  tc.Function.Name,
				Input: input,
			})
		}
		if apiIdx < len(apiMessages) && apiMessages[apiIdx].Role == "assistant" {
			apiMessages[apiIdx].Content = content
		}
	}

	req := &anthropicRequest{
		Model:       c.config.ModelID,
		MaxTokens:   opts.maxTokens,
		Messages:    apiMessages,
		Stream:      stream,
		Temperature: &opts.temperature,
	}
	if c.config.ProviderID == ProviderIDBedrock {
		// Bedrock carries the API version in-band instead of the
		// anthropic-version HTTP header (which its endpoint rejects).
		req.AnthropicVersion = bedrockAnthropicVersion
	}

	// When prompt caching is enabled and the system prompt contains the
	// boundary marker, split into cacheable blocks with cache_control markers.
	// The boundary is consumed by BuildSystemPromptBlocks and never sent to
	// the API. When caching is disabled or no boundary is present, strip any
	// stray boundary markers and send as a plain string.
	if c.config.PromptCache.IsEnabled() && strings.Contains(systemPrompt, PromptCacheBoundary) {
		sections := strings.Split(systemPrompt, "\n\n")
		blocks := BuildSystemPromptBlocks(sections)
		for _, b := range blocks {
			block := anthropicSystemBlock{Type: "text", Text: b.Text}
			if b.CacheScope == CacheScopeStatic {
				block.CacheControl = &anthropicCacheControl{Type: "ephemeral"}
			}
			req.SystemBlocks = append(req.SystemBlocks, block)
		}
	} else {
		req.System = StripPromptCacheBoundary(systemPrompt)
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

	// Apply reasoning effort from chatOptions (spec §2, line 737).
	// When no ReasoningConfig is provided, fall back to capability-based
	// detection (legacy behavior).
	if opts.reasoning != nil {
		applyAnthropicReasoning(req, c.config, opts.reasoning, nil)
	} else if c.config.HasCapability("extended_thinking") {
		req.Thinking = &anthropicThinkingConfig{
			Type: "enabled",
			// BudgetTokens is optional - let Anthropic use default
		}
	}

	return req, nil
}

// partsToAnthropicContent converts ContentParts to Anthropic content blocks.
// When an image has a Description, it is substituted as text (cached vision result).
// When store is available and description is empty, image bytes are loaded and
// sent as an image source block.
func (c *AnthropicClient) partsToAnthropicContent(parts []ContentPart, store UploadStore) []anthropicContent {
	var out []anthropicContent
	for _, p := range parts {
		switch p.Type {
		case "text":
			out = append(out, anthropicContent{
				Type: ContentTypeText,
				Text: p.Text,
			})
		case "image_url":
			if p.ImageURL == nil {
				continue
			}
			if p.ImageURL.Description != "" {
				out = append(out, anthropicContent{
					Type: ContentTypeText,
					Text: fmt.Sprintf("[image: %s]", p.ImageURL.Description),
				})
			} else {
				dataURL, err := resolveImageURL(p.ImageURL.URL, store)
				if err != nil {
					c.logger.Warn("Failed to resolve image URL", "url", p.ImageURL.URL, "error", err)
					out = append(out, anthropicContent{
						Type: ContentTypeText,
						Text: fmt.Sprintf("[image: unable to load %s]", p.ImageURL.URL),
					})
					continue
				}
				mimeType, data := parseDataURL(dataURL)
				out = append(out, anthropicContent{
					Type: "image",
					Source: &anthropicImageSource{
						Type:      "base64",
						MediaType: mimeType,
						Data:      data,
					},
				})
			}
		}
	}
	return out
}

// doRequest performs a non-streaming HTTP request to Anthropic's API.
// anthropicOAuthBeta is the beta header value required for Claude
// subscription (Pro/Max) OAuth access tokens on the Messages API.
const anthropicOAuthBeta = "oauth-2025-04-20"

// applyAnthropicAuth sets authentication headers. When an OAuth token
// resolver is configured (Claude Pro/Max subscription auth), Bearer auth
// with the oauth beta header replaces x-api-key; anthropic-version stays
// set in both modes. Bedrock requests are signed in place with AWS SigV4
// (stdlib implementation, see sigv4.go) instead of using Anthropic headers.
func (c *AnthropicClient) applyAnthropicAuth(ctx context.Context, httpReq *http.Request, body []byte) error {
	if c.config.ProviderID == ProviderIDBedrock {
		return c.applyBedrockSigV4(httpReq, body)
	}
	httpReq.Header.Set("anthropic-version", anthropicAPIVersion)
	if c.tokenResolver != nil && c.oauthProvider != "" {
		token, err := c.tokenResolver.ResolveToken(ctx, c.oauthProvider)
		if err != nil {
			return &ClientError{Message: "failed to resolve OAuth token", Cause: err}
		}
		httpReq.Header.Set("Authorization", "Bearer "+token)
		httpReq.Header.Set("anthropic-beta", anthropicOAuthBeta)
		return nil
	}
	httpReq.Header.Set("x-api-key", c.config.APIKey)
	return nil
}

// applyAnthropicExtraHeaders writes the static configured extra HTTP
// headers (ModelConfig.ExtraHeaders, models.json5 `extra_headers`) onto
// req. Called after applyAnthropicAuth so config headers can override the
// defaults (e.g. a custom anthropic-version). Values are sent verbatim —
// no per-request substitution. On Bedrock the SigV4 signature covers a
// fixed header set (content-type, host, x-amz-*), so config headers ride
// unsigned — valid, but overriding a signed x-amz-* header would break
// auth, so x-amz-* keys should not be configured on Bedrock providers.
func (c *AnthropicClient) applyAnthropicExtraHeaders(httpReq *http.Request) {
	for k, v := range c.config.ExtraHeaders {
		if v == "" {
			continue
		}
		httpReq.Header.Set(k, v)
	}
}

func (c *AnthropicClient) doRequest(ctx context.Context, reqBody *anthropicRequest) (*Response, error) {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, &ClientError{Message: "failed to marshal request", Cause: err}
	}

	url := c.anthropicRequestURL(false)

	c.logger.Debug("Making Anthropic request", "url", url, "model", c.config.ModelID)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, &ClientError{Message: "failed to create request", Cause: err}
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	if err := c.applyAnthropicAuth(ctx, httpReq, body); err != nil {
		return nil, err
	}
	c.applyAnthropicExtraHeaders(httpReq)

	start := time.Now()
	resp, err := c.httpClient.Do(httpReq)
	latencyMs := time.Since(start).Milliseconds()

	// Record error metrics if request failed (non-200 responses and network errors).
	// Successful request metrics are recorded after parsing the response body below.
	if c.metricsStore != nil && (err != nil || (resp != nil && resp.StatusCode != http.StatusOK)) {
		errType := metrics.ErrorTypeNone
		if err != nil {
			errType = metrics.ClassifyError(err, 0)
		} else if resp != nil {
			errType = metrics.ClassifyError(nil, resp.StatusCode)
		}
		record := metrics.RequestRecord{
			Timestamp:  time.Now(),
			ProviderID: c.config.ProviderID,
			ModelID:    c.config.ModelID,
			LatencyMs:  latencyMs,
			HTTPStatus: 0,
			ErrorType:  errType,
			Success:    false,
			CostUSD:    0, // no usage data on error path
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

	if resp != nil {
		defer resp.Body.Close()
	}

	if err != nil {
		return nil, &ClientError{Message: "request failed", Cause: err}
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &ClientError{Message: "failed to read response", Cause: err}
	}

	c.logger.Debug("Anthropic response received", "status", resp.StatusCode)

	// Handle rate limit (429) specifically with Retry-After and structured error.
	// Quota-window classification first (quota-reset-resilience): spend-cap
	// shapes become QuotaResetError; short-cycle retriable limits keep the
	// existing RateLimitError path.
	if resp.StatusCode == http.StatusTooManyRequests {
		if classifyQuotaDecision(resp.StatusCode, respBody, ParseRateLimitBody(respBody)) {
			if qe := ParseQuotaResponse(resp.StatusCode, resp.Header, respBody, QuotaContext{
				ProviderID: c.config.ProviderID,
				ModelID:    c.config.ModelID,
				MaxWait:    c.quotaMaxWait,
			}); qe != nil {
				qe.Cause = &APIError{StatusCode: resp.StatusCode, Detail: c.quotaDetailFromBody(respBody)}
				return nil, qe
			}
		}
		return nil, c.buildRateLimitError(respBody, resp.StatusCode, resp.Header.Get("Retry-After"))
	}

	// Quota payment-required (402): treat as retry-with-estimate.
	if resp.StatusCode == http.StatusPaymentRequired {
		qe := ParseQuotaResponse(resp.StatusCode, resp.Header, respBody, QuotaContext{
			ProviderID: c.config.ProviderID,
			ModelID:    c.config.ModelID,
			MaxWait:    c.quotaMaxWait,
		})
		if qe != nil {
			qe.Cause = &APIError{StatusCode: resp.StatusCode, Detail: c.quotaDetailFromBody(respBody)}
			return nil, qe
		}
	}

	// Check for other retryable status codes (500, 502, 503, 504, 529)
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

	// Record successful request metrics with actual usage data
	if c.metricsStore != nil {
		costUSD := float64(apiResp.Usage.InputTokens)*c.config.CostPerMillionInput/1_000_000 + float64(apiResp.Usage.OutputTokens)*c.config.CostPerMillionOutput/1_000_000
		record := metrics.RequestRecord{
			Timestamp:        time.Now(),
			ProviderID:       c.config.ProviderID,
			ModelID:          c.config.ModelID,
			PromptTokens:     apiResp.Usage.InputTokens,
			CompletionTokens: apiResp.Usage.OutputTokens,
			CachedTokens:     apiResp.Usage.CacheReadInputTokens,
			LatencyMs:        latencyMs,
			HTTPStatus:       resp.StatusCode,
			ErrorType:        metrics.ErrorTypeNone,
			Success:          true,
			CostUSD:          costUSD,
		}
		store := c.metricsStore
		logger := c.logger
		go func() {
			if rerr := store.Record(context.Background(), record); rerr != nil {
				logger.Debug("metrics record failed", "error", rerr)
			}
		}()
	}

	// Log prompt cache metrics when present.
	if apiResp.Usage.CacheCreationInputTokens > 0 || apiResp.Usage.CacheReadInputTokens > 0 {
		c.logger.Debug("prompt cache",
			"cache_creation", apiResp.Usage.CacheCreationInputTokens,
			"cache_read", apiResp.Usage.CacheReadInputTokens,
			"input", apiResp.Usage.InputTokens,
		)
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

	url := c.anthropicRequestURL(true)

	c.logger.Debug("Making Anthropic streaming request", "url", url, "model", c.config.ModelID)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, &ClientError{Message: "failed to create request", Cause: err}
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if err := c.applyAnthropicAuth(ctx, httpReq, body); err != nil {
		return nil, err
	}
	c.applyAnthropicExtraHeaders(httpReq)

	start := time.Now()
	resp, err := c.httpClient.Do(httpReq)
	latencyMs := time.Since(start).Milliseconds()

	// Record error metrics if request failed (non-200 responses and network errors).
	// Successful request metrics are recorded after parsing the stream below.
	if c.metricsStore != nil && (err != nil || (resp != nil && resp.StatusCode != http.StatusOK)) {
		errType := metrics.ErrorTypeNone
		if err != nil {
			errType = metrics.ClassifyError(err, 0)
		} else if resp != nil {
			errType = metrics.ClassifyError(nil, resp.StatusCode)
		}
		record := metrics.RequestRecord{
			Timestamp:  time.Now(),
			ProviderID: c.config.ProviderID,
			ModelID:    c.config.ModelID,
			LatencyMs:  latencyMs,
			ErrorType:  errType,
			Success:    false,
			CostUSD:    0, // no usage data on error path
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

	if resp != nil {
		defer resp.Body.Close()
	}

	if err != nil {
		return nil, &ClientError{Message: "request failed", Cause: err}
	}

	// Check for error status before streaming
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)

		// Handle rate limit (429) specifically with Retry-After and structured
		// error. Quota-window classification first (quota-reset-resilience).
		if resp.StatusCode == http.StatusTooManyRequests {
			if classifyQuotaDecision(resp.StatusCode, respBody, ParseRateLimitBody(respBody)) {
				if qe := ParseQuotaResponse(resp.StatusCode, resp.Header, respBody, QuotaContext{
					ProviderID: c.config.ProviderID,
					ModelID:    c.config.ModelID,
					MaxWait:    c.quotaMaxWait,
				}); qe != nil {
					qe.Cause = &APIError{StatusCode: resp.StatusCode, Detail: c.quotaDetailFromBody(respBody)}
					return nil, qe
				}
			}
			return nil, c.buildRateLimitError(respBody, resp.StatusCode, resp.Header.Get("Retry-After"))
		}

		// Quota payment-required (402): treat as retry-with-estimate.
		if resp.StatusCode == http.StatusPaymentRequired {
			if qe := ParseQuotaResponse(resp.StatusCode, resp.Header, respBody, QuotaContext{
				ProviderID: c.config.ProviderID,
				ModelID:    c.config.ModelID,
				MaxWait:    c.quotaMaxWait,
			}); qe != nil {
				qe.Cause = &APIError{StatusCode: resp.StatusCode, Detail: c.quotaDetailFromBody(respBody)}
				return nil, qe
			}
		}

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
		detail := string(respBody)
		if len(detail) > 1000 {
			detail = detail[:1000]
		}
		return nil, &APIError{StatusCode: resp.StatusCode, Detail: detail}
	}

	// Parse the stream. Bedrock wraps the Anthropic SSE events in AWS
	// event-stream binary framing (vnd.amazon.eventstream); the adapter
	// unwraps it into SSE-shaped bytes so the shared parser is unchanged.
	streamBody := io.Reader(resp.Body)
	if c.config.ProviderID == ProviderIDBedrock || hasBedrockEventStreamBody(resp.Header) {
		streamBody = newBedrockEventStreamAdapter(resp.Body)
	}
	parsedResp, parseErr := c.parseStreamingResponse(streamBody, progress)

	// Record successful request metrics with actual usage from the stream
	if c.metricsStore != nil && parseErr == nil && parsedResp != nil {
		costUSD := float64(parsedResp.Usage.PromptTokens)*c.config.CostPerMillionInput/1_000_000 + float64(parsedResp.Usage.CompletionTokens)*c.config.CostPerMillionOutput/1_000_000
		record := metrics.RequestRecord{
			Timestamp:        time.Now(),
			ProviderID:       c.config.ProviderID,
			ModelID:          c.config.ModelID,
			PromptTokens:     parsedResp.Usage.PromptTokens,
			CompletionTokens: parsedResp.Usage.CompletionTokens,
			CachedTokens:     parsedResp.Usage.CachedTokens,
			LatencyMs:        latencyMs,
			HTTPStatus:       resp.StatusCode,
			ErrorType:        metrics.ErrorTypeNone,
			Success:          true,
			CostUSD:          costUSD,
		}
		store := c.metricsStore
		logger := c.logger
		go func() {
			if rerr := store.Record(context.Background(), record); rerr != nil {
				logger.Debug("metrics record failed", "error", rerr)
			}
		}()
	}

	return parsedResp, parseErr
}

// buildRateLimitError constructs a *RateLimitError from a 429 response,
// parsing the Retry-After header and Anthropic's structured JSON error body.
func (c *AnthropicClient) buildRateLimitError(respBody []byte, statusCode int, retryAfterHeader string) *RateLimitError {
	retryAfter := parseRetryAfter(retryAfterHeader)

	detail := &ProviderError{}
	var anthErr anthropicErrorResponse
	if err := json.Unmarshal(respBody, &anthErr); err == nil && anthErr.Error.Type != "" {
		detail.Type = anthErr.Error.Type
		detail.Message = anthErr.Error.Message
	}

	apiErr := &APIError{StatusCode: statusCode, Detail: detail.Error()}
	if detail.Message == "" {
		bodyStr := string(respBody)
		if len(bodyStr) > 500 {
			bodyStr = bodyStr[:500]
		}
		apiErr.Detail = bodyStr
	}

	rlErr := &RateLimitError{
		ProviderID: c.config.ProviderID,
		ModelID:    c.config.ModelID,
		RetryAfter: retryAfter,
		LimitType:  detail.Type,
		Cause:      apiErr,
	}
	if detail.RetryStrategy != nil {
		rlErr.RetryStrategy = detail.RetryStrategy
	}
	if detail.LimitBudget != nil {
		rlErr.LimitBudget = detail.LimitBudget
	}
	return rlErr
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
				usage.CacheCreationInputTokens = streamEvent.Message.Usage.CacheCreationInputTokens
				usage.CacheReadInputTokens = streamEvent.Message.Usage.CacheReadInputTokens
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
	resp, err := c.buildResponseFromBlocks(blocks, stopReason, usage)
	if err != nil {
		return nil, err
	}

	// Log prompt cache metrics when present.
	if usage.CacheCreationInputTokens > 0 || usage.CacheReadInputTokens > 0 {
		c.logger.Debug("prompt cache",
			"cache_creation", usage.CacheCreationInputTokens,
			"cache_read", usage.CacheReadInputTokens,
			"input", usage.InputTokens,
		)
	}

	return resp, nil
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
			CachedTokens:     usage.CacheReadInputTokens,
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
			CachedTokens:     apiResp.Usage.CacheReadInputTokens,
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
			// Tree-02 leaf-03 fix: process the bytes returned WITH io.EOF
			// before honoring the EOF. Readers like http bodies and
			// iotest.DataErrReader deliver the final bytes TOGETHER with
			// EOF; the old code discarded them, silently dropping every
			// event in the trailing read (streaming responses parsed as
			// empty). n>0 must always be appended first.
			if n > 0 {
				s.buffer = append(s.buffer, chunk[:n]...)
			}
			if err == io.EOF {
				if len(s.buffer) == 0 {
					// B-06 FIX: Flush any pending line into the event, then
					// return the event if it has data. Without this, the last
					// SSE event is silently dropped when the connection ends
					// without a trailing blank line.
					if currentLine.Len() > 0 {
						s.processLine(currentLine.String())
					}
					return s.event.Data != ""
				}
				// Bytes arrived with the EOF — fall through and parse them;
				// the NEXT Scan's Read returns (0, io.EOF) and the flush
				// above still runs for any trailing line.
				err = nil
			}
			if err != nil {
				s.err = err
				return false
			}
		}

		// Process buffer
		// Leaf-03 fix (tree 02): the old loop dropped the byte at
		// i == len(s.buffer)-1 (s.buffer[:0] discarded it before it was
		// ever written to currentLine), so ANY line split across two
		// Read() chunks lost its tail — chunked SSE delivery (real HTTP
		// bodies) silently yielded zero events. Iterate by index and
		// keep the unconsumed remainder in the buffer instead.
		i := 0
		for i < len(s.buffer) {
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
				i++
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
				i++
			default:
				currentLine.WriteByte(c)
				i++
			}
		}
		// Chunk fully consumed — every byte was either folded into
		// currentLine / s.event or terminated a line above.
		s.buffer = s.buffer[:0]
	}
}

func (s *sseScanner) processLine(line string) {
	// RFC 8895 allows optional space after "data:" / "event:" colon.
	// Use CutPrefix without trailing space, then trim at most one leading space.
	if after, ok := strings.CutPrefix(line, "event:"); ok {
		s.event.Type = strings.TrimPrefix(after, " ")
	} else if after, ok := strings.CutPrefix(line, "data:"); ok {
		data := strings.TrimPrefix(after, " ")
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
