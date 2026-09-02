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
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/llm/metrics"
)

const (
	defaultTimeout = 120 * time.Second
	// defaultShortRetries is the nil-safe ShortRetries budget for the
	// policy-engine retry loops (leaf 03 Task 5; mirrors
	// config.DefaultFailurePolicyShortRetries, D4/D8). The old
	// maxRetries/streamMaxRetries/30s-cap constants left with the loops
	// they hardcoded.
	defaultShortRetries = 3
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
	config        *ModelConfig
	configMu      sync.RWMutex
	budget        *Budget
	httpClient    *http.Client
	logger        *slog.Logger
	metricsStore  *metrics.Store
	timeoutCalc   *metrics.Calculator
	tokenCache    ResponseCache
	keyBuilder    *CacheKeyBuilder
	tokenResolver TokenResolver
	oauthProvider string
	extraHeaders  map[string]string
	uploadStore   UploadStore
	// quotaMaxWait is the upper bound applied to derived quota waits
	// (llm.quota_retry.max_wait). Zero falls back to DefaultQuotaMaxWait.
	// Classification itself is always on: a quota-window error must be
	// distinguishable regardless of retry config (retry-on-quota is the
	// caller's decision).
	quotaMaxWait time.Duration
	// failurePolicy is the injected tree-02 policy config (leaf 03 Task 5):
	// ShortRetries bounds every loop's retry budget; nil-safe default 3.
	failurePolicy *FailurePolicyConfig
	// concurrencyGate limits concurrent requests for this model/provider.
	// When nil, no limit is enforced. Two-lane slot gate (tree 04 leaf
	// 03): replaces the raw buffered channel so interactive acquires
	// can jump the wait list under a starvation guard.
	concurrencyGate *slotGate
}

// DefaultQuotaMaxWait mirrors config.DefaultQuotaRetryMaxWait without
// importing internal/config (which would create a cycle via tools/mcp).
const DefaultQuotaMaxWait = 24 * time.Hour

// SetQuotaMaxWait sets the quota wait upper bound. Nil-receiver safe.
func (c *Client) SetQuotaMaxWait(d time.Duration) {
	if c == nil || d <= 0 {
		return
	}
	c.quotaMaxWait = d
}

// SetFailurePolicyConfig injects the tree-02 failure-policy config (leaf 03
// Task 5): ShortRetries bounds all retry loops; a nil or invalid (<=0)
// ShortRetries keeps the nil-safe default of 3 (config.DefaultFailurePolicy-
// ShortRetries). Nil-receiver safe. The wiring point mirrors SetQuotaMaxWait;
// the daemon maps config.FailurePolicyConfig onto *llm.FailurePolicyConfig
// there (internal/llm cannot import internal/config — import cycle).
func (c *Client) SetFailurePolicyConfig(cfg *FailurePolicyConfig) {
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
func (c *Client) shortRetryBudget() int {
	if c != nil && c.failurePolicy != nil && c.failurePolicy.ShortRetries > 0 {
		return c.failurePolicy.ShortRetries
	}
	return defaultShortRetries
}

// policyCfg resolves the injected policy config or the package defaults.
func (c *Client) policyCfg() FailurePolicyConfig {
	if c != nil && c.failurePolicy != nil {
		return *c.failurePolicy
	}
	return FailurePolicyConfig{
		Horizon:      24 * time.Hour,
		BaseThrottle: 30 * time.Second,
		PollFloor:    time.Hour,
	}
}

// shortThrottleSleep is the ONE throttle wait policy for all five retry
// loops (leaf 03: no sixth divergent copy). given the failed response
// headers, the plan, and the attempt index, it returns how long to wait:
//   - the server Retry-After (ParseRetryAfter, D6) always wins when later
//     than the plan's computed step — never hit a provider before it says;
//   - but never longer than the plan's Max step, so a huge Retry-After
//     cannot burn the short budget wall-clock (capped by the plan, D8).
//
// On a bare throttle with no server schedule the plan's exponential step
// applies. The returned time.Time is the absolute wake instant.
func shortThrottleSleep(header http.Header, plan BackoffPlan, now time.Time, attempt int) time.Duration {
	step := plan.NextAttempt(now, attempt, time.Time{}).Sub(now)
	if date, delta, present := ParseRetryAfter(header); present {
		serverAt := date
		if date.IsZero() {
			serverAt = now.Add(delta)
		}
		if wait := serverAt.Sub(now); wait > step {
			step = wait
		}
	}
	if step < 0 {
		step = 0
	}
	return step
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

// WithUploadStore sets the upload store for resolving image file references.
func WithUploadStore(store UploadStore) ClientOption {
	return func(c *Client) {
		if store != nil {
			c.uploadStore = store
		}
	}
}

// WithConcurrencyLimit sets the maximum concurrent requests for this client.
// When maxConcurrency is 0 or negative, no limit is enforced (unlimited).
// The limit is enforced using a two-lane slot gate (tree 04 leaf 03):
// interactive acquires jump background waiters, bounded by a starvation
// guard (slot_gate.go).
func WithConcurrencyLimit(maxConcurrency int) ClientOption {
	return func(c *Client) {
		if maxConcurrency > 0 {
			c.concurrencyGate = newSlotGate(maxConcurrency)
		}
	}
}

// NewClient creates a new LLM client.
func NewClient(config *ModelConfig, opts ...ClientOption) *Client {
	if config == nil {
		config = &ModelConfig{}
	}
	c := &Client{
		config: config,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
		logger: slog.Default(),
	}

	// Initialize concurrency gate from config if set (tree 04 leaf 03:
	// slotGate replaces the raw channel semaphore)
	if config.MaxConcurrency > 0 {
		c.concurrencyGate = newSlotGate(config.MaxConcurrency)
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// Reconfigure swaps the client's underlying ModelConfig under configMu so
// subsequent Chat calls target the new endpoint/model. Used by alias
// failover (leaf 03 of classifier-reliability): the classifier and intent
// analyzer keep a single *llm.Client and rotate its config when the active
// candidate fails. Safe for concurrent readers: Chat snapshots cfg under
// c.configMu.RLock, and all other reads go through the same lock.
func (c *Client) Reconfigure(cfg *ModelConfig) {
	if cfg == nil {
		return
	}
	c.configMu.Lock()
	c.config = cfg
	c.configMu.Unlock()
}

// buildChatRequest constructs the chat options and JSON payload shared by
// Chat(), ChatWithProgress(), and ChatWithDeltaCallback().
// If addStream is true, the payload includes "stream": true.
func (c *Client) buildChatRequest(messages []ChatMessage, cfg *ModelConfig, opts []ChatOption, addStream bool) (*chatOptions, map[string]any, error) {
	// Build chatOptions from config defaults
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

	// Build request payload
	msgDicts := make([]map[string]any, len(messages))
	for i, msg := range messages {
		// Strip the internal prompt cache boundary from system messages so
		// it is never leaked to the API. This keeps the system prompt prefix
		// stable across calls regardless of internal section ordering.
		if msg.Role == RoleSystem {
			msg.Content = StripPromptCacheBoundary(msg.Content)
		}
		msgDicts[i] = msg.ToOpenAIDictWithStore(c.uploadStore)
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

	if addStream {
		payload["stream"] = true
		// B-02 FIX: Request usage in the final stream chunk so budget
		// accounting and cost tracking work for streaming completions.
		payload["stream_options"] = map[string]any{
			"include_usage": true,
		}
	}

	// Apply reasoning effort translation for OpenAI-compatible vendors
	// (spec §2). This covers OpenAI, Gemini, GLM, Kimi, Qwen, DeepSeek,
	// OpenRouter, etc. The Anthropic path is handled separately in
	// anthropic.go buildRequest via applyAnthropicReasoning.
	if chatOpts.reasoning != nil {
		applyOpenAICompatReasoning(payload, cfg, chatOpts.reasoning, nil)
	}

	// Thread LoRA adapter path through to providers that support it.
	// Providers without adapter support silently ignore this key.
	if chatOpts.adapterPath != "" {
		payload["adapter_path"] = chatOpts.adapterPath
	}

	c.attachToolGrammar(payload, cfg, chatOpts)
	attachRawGrammar(payload, chatOpts)

	return chatOpts, payload, nil
}

// Chat sends a chat completion request and returns the parsed response.
func (c *Client) Chat(ctx context.Context, messages []ChatMessage, opts ...ChatOption) (*Response, error) {
	c.configMu.RLock()
	cfg := c.config
	c.configMu.RUnlock()

	chatOpts, payload, err := c.buildChatRequest(messages, cfg, opts, false)
	if err != nil {
		return nil, err
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

	var lastErr error

	// D4 (leaf 03): every failure classifies through the policy engine
	// FIRST; the legacy local backoff constants are gone. Quota exits
	// immediately (quota-reset-resilience contract 1); throttle retries
	// within the short budget and then escalate to ThrottleBackoffError;
	// server errors keep the bounded retry + ClientError shape.
	shortRetries := c.shortRetryBudget()
	now := time.Now()
	plan := DefaultBackoffPlan(FailureThrottle, now, c.policyCfg())

	for attempt := 1; attempt <= shortRetries; attempt++ {
		resp, err := c.doRequest(ctx, payload, cfg, chatOpts.priority)
		if err != nil {
			// Quota errors never re-enter the short-retry loop: the window
			// is hours, not seconds. Return immediately; the caller decides
			// whether to wait/rotate (quota-reset-resilience contract 1).
			var quotaErr *QuotaResetError
			if errors.As(err, &quotaErr) {
				return nil, err
			}

			// D4/D7: Classify FIRST on the failure. The request layer
			// already resolved quota-shaped responses to QuotaResetError,
			// so a RateLimitError/APIError here is a bare throttle or
			// server error (D7: a Retry-After alone is never a quota
			// signal).
			status, header := errorStatusAndHeader(err)
			verdict := Classify(status, header, nil, time.Now())

			switch verdict.Class {
			case FailureThrottle:
				// Short-horizon retries honoring the server schedule; when
				// the short budget is exhausted, escalate to
				// ThrottleBackoffError so the CALLER can park (D4/D8) —
				// never a ClientError.
				lastErr = err
				c.logger.Warn("Retryable throttle",
					"status", status,
					"attempt", attempt,
					"max_retries", shortRetries,
				)
				if attempt < shortRetries {
					wait := shortThrottleSleep(header, plan, time.Now(), attempt-1)
					c.logger.Debug("Retry backoff", "attempt", attempt, "sleep", wait)
					select {
					case <-time.After(wait):
						continue
					case <-ctx.Done():
						return nil, ctx.Err()
					}
				}
				return nil, &ThrottleBackoffError{
					ProviderID: cfg.ProviderID,
					ModelID:    cfg.ModelID,
					RetryAt:    plan.NextAttempt(time.Now(), attempt, errorRetryAt(err)),
					Attempt:    attempt,
					Cause:      err,
				}
			case FailureServerError:
				// Bounded retries via the plan; give up → the historical
				// ClientError shape (leaf Notes: only THROTTLE exhaustion
				// changes type).
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

		// Record usage with scope
		if c.budget != nil {
			c.budget.RecordUsageWithScope(resp.Usage, chatOpts.taskID, chatOpts.sessionID)
			// Record cost with scope if model pricing is available
			if c.config != nil {
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

	// D8 exhaustion cap: every attempt was classified (throttle escalations
	// already returned ThrottleBackoffError above); reaching here means the
	// remaining retries were server errors — the historical ClientError
	// shape stands (leaf Notes).
	return nil, &ClientError{
		Message: fmt.Sprintf("All %d attempts failed", shortRetries),
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

	chatOpts, payload, err := c.buildChatRequest(messages, cfg, opts, false)
	if err != nil {
		return nil, err
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

	if len(chatOpts.tools) > 0 {
		reportProgress(ProgressStageToolCall, fmt.Sprintf("Request includes %d tools", len(chatOpts.tools)))
	}

	var lastErr error

	// D4 (leaf 03): classify-first policy engine, identical shape to the
	// non-streaming Chat loop — throttle escalates to ThrottleBackoffError,
	// server errors keep the bounded ClientError exhaustion shape.
	shortRetries := c.shortRetryBudget()
	now := time.Now()
	plan := DefaultBackoffPlan(FailureThrottle, now, c.policyCfg())

	for attempt := 1; attempt <= shortRetries; attempt++ {
		if attempt > 1 {
			reportProgress(ProgressStageThinking, fmt.Sprintf("Retry attempt %d/%d...", attempt, shortRetries))
		} else {
			reportProgress(ProgressStageThinking, "Model is thinking...")
		}

		resp, err := c.doRequest(ctx, payload, cfg, chatOpts.priority)
		if err != nil {
			// Quota errors never re-enter the short-retry loop (streaming
			// path): hours-scale window, return immediately.
			var quotaErr *QuotaResetError
			if errors.As(err, &quotaErr) {
				reportProgress(ProgressStageDone, fmt.Sprintf("Error: %v", err))
				return nil, err
			}

			// D4/D7: classify FIRST; the request layer already resolved
			// quota shapes to QuotaResetError (see Chat loop).
			status, header := errorStatusAndHeader(err)
			verdict := Classify(status, header, nil, time.Now())

			switch verdict.Class {
			case FailureThrottle:
				// Short-horizon retries honoring the server schedule;
				// exhaustion escalates to ThrottleBackoffError (D4/D8),
				// never a ClientError.
				lastErr = err
				c.logger.Warn("Retryable throttle",
					"status", status,
					"attempt", attempt,
					"max_retries", shortRetries,
				)
				if attempt < shortRetries {
					wait := shortThrottleSleep(header, plan, time.Now(), attempt-1)
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
					ProviderID: cfg.ProviderID,
					ModelID:    cfg.ModelID,
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

		// Streaming stage - response received
		reportProgress(ProgressStageStreaming, "Receiving response...")

		// Record usage with scope
		if c.budget != nil {
			c.budget.RecordUsageWithScope(resp.Usage, chatOpts.taskID, chatOpts.sessionID)
			// Record cost with scope if model pricing is available
			if c.config != nil {
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

		// Check if response contains tool calls
		if resp.HasToolCalls() {
			reportProgress(ProgressStageToolCall, fmt.Sprintf("Response contains %d tool calls", len(resp.ToolCalls)))
		}

		// Report completion with token count
		reportProgress(ProgressStageDone, fmt.Sprintf("Complete: %d tokens", resp.Usage.TotalTokens))

		return resp, nil
	}

	reportProgress(ProgressStageDone, fmt.Sprintf("Failed after %d attempts", shortRetries))
	return nil, &ClientError{
		Message: fmt.Sprintf("All %d attempts failed", shortRetries),
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
	reasoning        *ReasoningConfig
	adapterPath      string // LoRA adapter path for providers that support it
	// grammarMode is the tool-call grammar constraint mode for this request
	// ("", see gbnf.go constraint constants). Set via WithGrammar.
	grammarMode string
	// rawGrammar is a caller-supplied grammar body attached verbatim when
	// GBNFConstrainedEnabled() is on (see WithRawGrammar). Unlike
	// grammarMode, it does NOT require tools on the request — for
	// structured-output constraints on tool-free calls (e.g. SKILL.state
	// response envelopes).
	rawGrammar string
	// priority marks this turn INTERACTIVE for model-slot acquisition
	// (tree 04 leaf 03, D11): chat turns pass true; queue/specialist/goal
	// work stays false. It gates slot-GATE lane choice ONLY — it never
	// touches the wire payload. The queue job's Interactive flag is a
	// different layer (stamped at enqueue from the originating session)
	// and does not flow here; see docs/workflows/llm-management.md.
	priority bool
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

// WithAdapter sets the LoRA adapter path to use for this request. The
// path is passed through to providers that support adapter selection
// (e.g. a local LFM inference server). Providers that do not support
// adapters silently ignore it.
func WithAdapter(path string) ChatOption {
	return func(o *chatOptions) {
		o.adapterPath = path
	}
}

// WithReasoning sets the reasoning/thinking effort for the chat request.
// The config is translated to vendor-specific wire formats via
// applyOpenAICompatReasoning (OpenAI-compatible path) or
// applyAnthropicReasoning (Anthropic path).
func WithReasoning(rc *ReasoningConfig) ChatOption {
	return func(o *chatOptions) {
		o.reasoning = rc
	}
}

// gbnfWarnOnce deduplicates the incomplete-grammar warning across calls
// (warn once per process/session, not per request).
var gbnfWarnOnce sync.Map // map[string]struct{}

// GBNFConstrained is the global kill-switch for grammar-constrained tool
// calling ([agent.tools] gbnf_constrained). Default FALSE: no grammar is
// attached anywhere until explicitly enabled. Set via SetGBNFConstrained
// at daemon wiring time.
var GBNFConstrained = false

// gbnfSwitchMu guards GBNFConstrained.
var gbncSwitchMu sync.RWMutex

// SetGBNFConstrained sets the global gbnf_constrained switch.
func SetGBNFConstrained(on bool) {
	gbncSwitchMu.Lock()
	GBNFConstrained = on
	gbncSwitchMu.Unlock()
}

// GBNFConstrainedEnabled reads the global switch atomically.
func GBNFConstrainedEnabled() bool {
	gbncSwitchMu.RLock()
	defer gbncSwitchMu.RUnlock()
	return GBNFConstrained
}

// attachToolGrammar attaches a grammar constraint to the request payload when:
//   - the caller opted in via WithGrammar (chatOpts.grammarMode != ""),
//   - the resolved model config declares a matching tool_constraint capability,
//   - tools are present on the request, and
//   - the global GBNFConstrained switch is enabled.
//
// Any other combination leaves the payload untouched (zero wire change for
// providers without declared capability or when the switch is off).
func (c *Client) attachToolGrammar(payload map[string]any, cfg *ModelConfig, chatOpts *chatOptions) {
	if chatOpts.grammarMode == "" || len(chatOpts.tools) == 0 {
		return
	}
	if !GBNFConstrainedEnabled() {
		return
	}
	mode := cfg.ToolConstraint
	if mode == "" && chatOpts.grammarMode != "" && cfg.HasCapability(CapToolConstraint) {
		// Model-level capability without an explicit mode: fall back to the
		// mode requested by the caller if it is recognized.
		mode = chatOpts.grammarMode
	}
	if mode == "" || mode != chatOpts.grammarMode {
		return
	}

	var grammar string
	complete := false
	switch mode {
	case ToolConstraintJSONSchea:
		grammar = JSONSchemaForTools(chatOpts.tools)
		complete = len(chatOpts.tools) > 0
	default:
		grammar, complete = GrammarForTools(chatOpts.tools)
	}

	if !complete {
		key := "incomplete:" + strings.Join(toolNames(chatOpts.tools), ",")
		if _, dup := gbnfWarnOnce.LoadOrStore(key, struct{}{}); !dup {
			c.logger.Warn("gbnf: tool schemas contain unsupported constructs; excluding affected tools from grammar",
				"mode", mode)
		}
	}
	AttachGrammar(payload, mode, grammar)
	return
}

// attachRawGrammar attaches a caller-supplied grammar to the payload when the
// global GBNFConstrained switch is on. This path is independent of tools and
// model capability: the caller opted in explicitly via WithRawGrammar and owns
// the grammar body.
func attachRawGrammar(payload map[string]any, chatOpts *chatOptions) {
	if chatOpts.rawGrammar == "" {
		return
	}
	if !GBNFConstrainedEnabled() {
		return
	}
	payload["grammar"] = chatOpts.rawGrammar
}

func toolNames(defs []ToolDefinition) []string {
	names := make([]string, 0, len(defs))
	for _, d := range defs {
		names = append(names, d.Function.Name)
	}
	return names
}

// WithGrammar enables GBNF/grammar-constrained tool calling for this request
// using the given constraint mode ("llamacpp", "vllm", or "json_schema").
// The grammar is only attached when tools are present AND the resolved model
// config declares a matching tool_constraint capability AND the global
// [agent.tools] gbnf_constrained switch is on. An incomplete grammar warns
// once per session and is skipped.
func WithGrammar(mode string) ChatOption {
	return func(o *chatOptions) {
		o.grammarMode = mode
	}
}

// WithRawGrammar attaches the caller's GBNF grammar body directly to the
// request payload (llamacpp wire format: payload["grammar"]) when the global
// GBNFConstrained switch is on. Unlike WithGrammar, this does NOT require
// tools on the request or a model tool_constraint capability — it is for
// constraining free-form structured output (e.g. the SKILL.state response
// envelope) on tool-free calls. An empty grammar is a no-op.
func WithRawGrammar(grammar string) ChatOption {
	return func(o *chatOptions) {
		o.rawGrammar = grammar
	}
}

// WithPriority marks this chat turn INTERACTIVE for model-slot
// acquisition (tree 04 leaf 03): when model concurrency is capped, an
// interactive acquire is granted ahead of waiting background acquires,
// bounded by a starvation guard (3 interactive grants → 1 background).
// Default is false (background) for all callers that never pass this —
// byte-identical ordering semantics to the prior channel semaphore.
// This affects acquisition ordering only; nothing is added to the
// request payload. Per D11, exactly two tiers exist: interactive chat
// turns (true) and everything else (false, the default).
func WithPriority(interactive bool) ChatOption {
	return func(o *chatOptions) {
		o.priority = interactive
	}
}

// PriorityOf reports whether the given options mark the turn INTERACTIVE
// for model-slot acquisition (tree 04 leaf 03, D11). It is the inspection
// counterpart of WithPriority: a priority-less (nil / empty) option slice
// or one never passing WithPriority reads as false (background), which is
// exactly how the client's acquire path treats such callers. Test-facing
// seam for callers that stub the Chatter and assert on option contents.
func PriorityOf(opts []ChatOption) bool {
	var o chatOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	return o.priority
}

// doRequest performs the HTTP request and parses the response.
// cfg must be captured under lock by the caller.
// priority marks an interactive turn for slot-gate priority (tree 04
// leaf 03); priority-less callers pass false, unchanged behavior.
func (c *Client) doRequest(ctx context.Context, payload map[string]any, cfg *ModelConfig, priority bool) (*Response, error) {
	// Acquire concurrency slot (if configured)
	release, err := c.acquireConcurrencyLimit(ctx, priority)
	if err != nil {
		return nil, &ClientError{Message: "concurrency limit wait interrupted", Cause: err}
	}
	defer release()

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, &ClientError{Message: "failed to marshal request", Cause: err}
	}

	// Build URL - baseURL should be the full API base (e.g., http://host/v1 or http://host/api)
	// We just append /chat/completions to whatever baseURL is configured
	c.configMu.RLock()
	baseURL := strings.TrimSuffix(cfg.BaseURL, "/")
	extraHeaders := c.extraHeaders
	c.configMu.RUnlock()
	url := baseURL + "/chat/completions"
	// Use cfg for modelID, apiKey, providerID to avoid race with SwitchModel
	modelID := cfg.ModelID
	apiKey := cfg.APIKey
	providerID := cfg.ProviderID

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
	resp, err := c.httpClient.Do(req)
	latencyMs := time.Since(start).Milliseconds()

	// bodyclose requires unconditional defer for HTTP response bodies
	defer func() {
		if resp != nil {
			resp.Body.Close()
		}
	}()

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
		var providerDetail *ProviderError
		if len(respBody) > 0 {
			providerDetail = ParseRateLimitBody(respBody)
		}

		// Quota-window classification (quota-reset-resilience): short-cycle
		// retriable limits stay RateLimitError; usage-window/billing shapes
		// become QuotaResetError so callers rotate/block instead of retrying.
		if classifyQuotaDecision(resp.StatusCode, respBody, providerDetail) {
			qe := ParseQuotaResponse(resp.StatusCode, resp.Header, respBody, QuotaContext{
				ProviderID: providerID,
				ModelID:    modelID,
				MaxWait:    c.quotaMaxWait,
			})
			if qe != nil {
				qe.Cause = &APIError{StatusCode: resp.StatusCode, Detail: detail}
				return nil, qe
			}
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

	// Check for quota payment-required (402): billing exhaustion is treated
	// as retry-with-estimate (default posture: top-up resumes the queue).
	if resp.StatusCode == http.StatusPaymentRequired {
		detail := string(respBody)
		if len(detail) > 500 {
			detail = detail[:500]
		}
		qe := ParseQuotaResponse(resp.StatusCode, resp.Header, respBody, QuotaContext{
			ProviderID: providerID,
			ModelID:    modelID,
			MaxWait:    c.quotaMaxWait,
		})
		if qe != nil {
			qe.Cause = &APIError{StatusCode: resp.StatusCode, Detail: detail}
			return nil, qe
		}
	}

	// Check for other error status codes
	if resp.StatusCode != http.StatusOK {
		detail := string(respBody)
		if len(detail) > 1000 {
			detail = detail[:1000]
		}
		// Log full request payload for 400 errors to aid debugging
		if resp.StatusCode == http.StatusBadRequest {
			c.logger.Error("LLM request failed with 400 Bad Request",
				"url", url,
				"model", modelID,
				"provider", providerID,
				"status", resp.StatusCode,
				"error_detail", detail,
			)
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
		costUSD := float64(chatResp.Usage.PromptTokens)*cfg.CostPerMillionInput/1_000_000 + float64(chatResp.Usage.CompletionTokens)*cfg.CostPerMillionOutput/1_000_000
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

// ErrEmptyResponse is returned when the model replies with an empty body.
// It is a sentinel so callers (e.g. ClassifyClassificationFailure) can
// identify the failure kind without string matching.
var ErrEmptyResponse = &ClientError{Message: "empty content"}

// parseResponse converts a raw ChatResponse to a Response.
func (c *Client) parseResponse(chatResp *ChatResponse) (*Response, error) {
	if len(chatResp.Choices) == 0 {
		return nil, ErrEmptyResponse
	}

	choice := chatResp.Choices[0]
	msg := choice.Message

	content := msg.ContentString()

	// Empty content with no tool calls is the "model said nothing" failure.
	// Surface the sentinel so ClassifyClassificationFailure sees
	// ClassificationFailureEmptyResponse instead of falling back to
	// string-matching heuristics.
	if content == "" && len(msg.ToolCalls) == 0 {
		return nil, ErrEmptyResponse
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
		Reasoning:    msg.ReasoningContent,
	}, nil
}

// ChatWithDeltaCallback sends a streaming chat completion request and invokes
// onDelta for each content chunk. If onDelta returns a non-nil error, the
// stream is cancelled and that error is returned. The final accumulated
// Response is returned on successful completion.
// D4: Added retry with resume capability for transient errors.
func (c *Client) ChatWithDeltaCallback(ctx context.Context, messages []ChatMessage, onDelta DeltaCallback, opts ...ChatOption) (*Response, error) {
	// Time the HTTP request
	start := time.Now()

	if onDelta == nil {
		// Fallback to non-streaming when no callback provided
		return c.Chat(ctx, messages, opts...)
	}

	c.configMu.RLock()
	cfg := c.config
	c.configMu.RUnlock()

	chatOpts, payload, err := c.buildChatRequest(messages, cfg, opts, true)
	if err != nil {
		return nil, err
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

	// D4: Retry loop for transient errors with resume capability.
	// Leaf 03: the count comes from the policy config (shortRetries; the
	// old streamMaxRetries constant is gone) and every failure goes through
	// Classify FIRST. Streaming subtlety: the pre-first-token gating is
	// preserved exactly — doStreamRequest only returns HTTP-status errors
	// BEFORE the SSE scanner starts, so a throttle that survived the short
	// retries surfaces as ThrottleBackoffError only when no tokens flowed;
	// a mid-stream failure returns its own error (existing behavior).
	var lastErr error
	retryState := &streamRetryState{
		toolCallAccums: make(map[int]*toolCallAccum),
	}
	shortRetries := c.shortRetryBudget()
	now := time.Now()
	plan := DefaultBackoffPlan(FailureThrottle, now, c.policyCfg())

	for attempt := range shortRetries {
		if attempt > 0 {
			// D4: Set resume flag for retry attempts
			retryState.isResume = true
			c.logger.Debug("stream retry attempt", "attempt", attempt+1, "max", shortRetries)
		}
		resp, httpStatus, err := c.doStreamRequest(ctx, body, onDelta, retryState, cfg, chatOpts.priority)
		if err == nil {
			// Record usage with scope
			if c.budget != nil && resp != nil {
				c.budget.RecordUsageWithScope(resp.Usage, chatOpts.taskID, chatOpts.sessionID)
				// Record cost with scope if model pricing is available
				if c.config != nil {
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

			// Record metrics on success
			if c.metricsStore != nil && resp != nil && httpStatus > 0 {
				latencyMs := time.Since(start).Milliseconds()
				costUSD := float64(resp.Usage.PromptTokens)*cfg.CostPerMillionInput/1_000_000 + float64(resp.Usage.CompletionTokens)*cfg.CostPerMillionOutput/1_000_000
				// Snapshot values accessed by the goroutine to avoid races
				// on resp/httpResp which are scoped to the outer for loop.
				promptTokens := resp.Usage.PromptTokens
				completionTokens := resp.Usage.CompletionTokens
				cachedTokens := resp.Usage.CachedTokens
				//nolint:gosec // goroutine outlives request context
				go func() {
					record := metrics.RequestRecord{
						Timestamp:        time.Now(),
						ProviderID:       cfg.ProviderID,
						ModelID:          cfg.ModelID,
						PromptTokens:     promptTokens,
						CompletionTokens: completionTokens,
						CachedTokens:     cachedTokens,
						LatencyMs:        latencyMs,
						HTTPStatus:       httpStatus,
						ErrorType:        metrics.ErrorTypeNone,
						Success:          true,
						CostUSD:          costUSD,
					}
					_ = c.metricsStore.Record(context.Background(), record)
				}()
			}
			return resp, nil
		}
		lastErr = err

		// Quota errors never re-enter the short-retry loop (streaming
		// delta path): the window is hours, not seconds. Return immediately;
		// the caller decides whether to wait/rotate (quota-reset-resilience
		// contract 1). QuotaResetError wraps a 429 APIError, so without this
		// branch the retryable check would short-retry it. Placement is
		// BEFORE any retryable check — semantically unchanged (leaf 03).
		var quotaErr *QuotaResetError
		if errors.As(err, &quotaErr) {
			return nil, err
		}

		// D4/D7 (leaf 03): classify FIRST, then obey. The quota early-exit
		// above already peeled off quota-shaped responses, so a throttle
		// verdict here is a bare 429/503 (Retry-After alone is never a
		// quota signal). Preserves the loop's own mid-stream gating: a
		// mid-stream failure is a ClientError whose message carries
		// "stream"/EOF markers and Classify(0,...) returns FailureNone,
		// which returns it unchanged (existing behavior).
		status, header := errorStatusAndHeader(err)
		verdict := Classify(status, header, nil, time.Now())

		switch verdict.Class {
		case FailureThrottle:
			// D4: Don't retry if we're on the last attempt — exhaust the
			// short budget into ThrottleBackoffError (NOT a wrapped
			// ClientError), letting the caller park until RetryAt (D4/D8).
			if attempt >= shortRetries-1 {
				c.logger.Warn("stream throttle budget exhausted",
					"attempt", attempt+1,
					"max", shortRetries,
				)
				return nil, &ThrottleBackoffError{
					ProviderID: cfg.ProviderID,
					ModelID:    cfg.ModelID,
					RetryAt:    plan.NextAttempt(time.Now(), attempt, errorRetryAt(err)),
					Attempt:    attempt + 1,
					Cause:      err,
				}
			}

			// D4: backoff honoring the server Retry-After (ParseRetryAfter)
			// over the plan's computed step, capped by the plan (D8).
			backoff := shortThrottleSleep(header, plan, time.Now(), attempt)
			if rl, ok := err.(*RateLimitError); ok && rl.RetryAfter > 0 && rl.RetryAfter < backoff {
				// Parity with the legacy loop: a server-suggested shorter
				// wait (body retry_after) is honored — never wait longer
				// than the provider demands when it gave a smaller number.
				backoff = rl.RetryAfter
			}
			c.logger.Debug("stream throttle backoff", "attempt", attempt+1, "sleep", backoff)

			select {
			case <-time.After(backoff):
				// Continue to next attempt
			case <-ctx.Done():
				return nil, ctx.Err()
			}

		case FailureServerError:
			// D4: Don't retry if we're on the last attempt
			if attempt >= shortRetries-1 {
				break
			}

			// D4: bounded backoff via the plan (server-error path carries
			// no Retry-After schedule).
			backoff := shortThrottleSleep(nil, plan, time.Now(), attempt)
			c.logger.Debug("stream server-error backoff", "attempt", attempt+1, "sleep", backoff)

			select {
			case <-time.After(backoff):
				// Continue to next attempt
			case <-ctx.Done():
				return nil, ctx.Err()
			}

		default:
			// FailureFatal and everything the engine does not schedule
			// (transport errors, mid-stream aborts): return immediately
			// (existing behavior).
			c.logger.Debug("non-retryable stream error", "error", err)
			return nil, err
		}
	}

	return nil, &ClientError{
		Message: fmt.Sprintf("streaming failed after %d attempts", shortRetries),
		Cause:   lastErr,
	}
}

// doStreamRequest performs a single streaming HTTP request and invokes onDelta for each chunk.
// D4: Extracted to enable retry with resume capability.
// retryState tracks state from prior attempts (accumulated content, tool calls, usage).
// If retryState.isResume is true, the request includes Last-Event-ID header for resume.
// NOTE: resp.Body is closed before the function returns (line 1270). Callers only access
// resp.StatusCode and must not read resp.Body.
// Returns HTTP status code as int so callers don't manage *http.Response lifecycle.
// cfg must be captured under lock by the caller.
// priority marks an interactive turn for slot-gate priority (tree 04 leaf 03).
func (c *Client) doStreamRequest(ctx context.Context, body []byte, onDelta DeltaCallback, retryState *streamRetryState, cfg *ModelConfig, priority bool) (*Response, int, error) {
	// Acquire concurrency slot (if configured)
	release, err := c.acquireConcurrencyLimit(ctx, priority)
	if err != nil {
		c.logger.Debug("stream concurrency limit wait interrupted", "error", err)
		// Release the RPM slot reserved by WaitForRateLimit since the request
		// will not reach the API.
		if c.budget != nil {
			c.budget.ReleaseRateLimitSlot()
		}
		return nil, 0, &ClientError{Message: "concurrency limit wait interrupted", Cause: err}
	}
	defer release()

	c.configMu.RLock()
	baseURL := strings.TrimSuffix(cfg.BaseURL, "/")
	extraHeaders := c.extraHeaders
	c.configMu.RUnlock()
	url := baseURL + "/chat/completions"
	// Use cfg for modelID, apiKey, providerID to avoid race with SwitchModel
	modelID := cfg.ModelID
	apiKey := cfg.APIKey
	providerID := cfg.ProviderID

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		c.logger.Debug("stream request failed at creation", "error", err)
		return nil, 0, &ClientError{Message: "failed to create request", Cause: err}
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
			return nil, 0, &ClientError{Message: "failed to resolve OAuth token", Cause: err}
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
		return nil, 0, &ClientError{Message: "request failed", Cause: err}
	}

	if resp.StatusCode != http.StatusOK {
		retryAfter := extractRetryAfter(resp)
		detail, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if len(detail) > 1000 {
			detail = detail[:1000]
		}

		apiErr := &APIError{StatusCode: resp.StatusCode, Detail: string(detail)}

		// Quota-window classification (quota-reset-resilience): 429/402
		// usage-window/billing shapes become QuotaResetError so callers
		// rotate/block instead of short-retrying; short-cycle retriable
		// OpenRouter limits keep the RateLimitError path below.
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusPaymentRequired {
			if classifyQuotaDecision(resp.StatusCode, detail, ParseRateLimitBody(detail)) {
				if qe := ParseQuotaResponse(resp.StatusCode, resp.Header, detail, QuotaContext{
					ProviderID: providerID,
					ModelID:    modelID,
					MaxWait:    c.quotaMaxWait,
				}); qe != nil {
					qe.Cause = apiErr
					return nil, 0, qe
				}
			}
		}

		// Wrap in RateLimitError for 429 to preserve Retry-After
		if resp.StatusCode == 429 && retryAfter > 0 {
			return nil, 0, &RateLimitError{
				ProviderID: providerID,
				ModelID:    modelID,
				RetryAfter: retryAfter,
				Cause:      apiErr,
			}
		}
		// S3-9 FIX: return nil resp — body is already closed; returning it
		// would let callers dereference a closed-body http.Response.
		return nil, 0, apiErr
	}

	// Parse stream
	var accumulated strings.Builder
	var reasoningBuilder strings.Builder
	var finishReason string
	var usage TokenUsage

	// Pre-populate from retryState if resuming
	if retryState != nil && retryState.accumulated.Len() > 0 {
		accumulated.WriteString(retryState.accumulated.String())
		usage = retryState.usage
	}

	scanner := bufio.NewScanner(resp.Body)
	// B-05 FIX: Raise scanner buffer to 10 MiB so large SSE lines
	// (e.g. tool-call arguments) don't fail the stream.
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
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

	// savePartialState copies accumulated content, tool calls, usage, and
	// deltasSent into retryState so a retry attempt can skip already-delivered
	// deltas (B-03 fix).
	savePartialState := func() {
		if retryState == nil {
			return
		}
		retryState.accumulated = strings.Builder{}
		retryState.accumulated.WriteString(accumulated.String())
		retryState.usage = usage
		retryState.deltasSent = deltasSent
		retryState.toolCallAccums = make(map[int]*toolCallAccum, len(toolCallAccums))
		for idx, accum := range toolCallAccums {
			clone := &toolCallAccum{
				ID:   accum.ID,
				Name: accum.Name,
			}
			clone.Arguments.WriteString(accum.Arguments.String())
			retryState.toolCallAccums[idx] = clone
		}
	}

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			resp.Body.Close()
			// B-03 FIX: Save partial state so retry can skip delivered deltas.
			savePartialState()
			// S3-9 FIX: body is now closed; don't return resp.
			return nil, 0, ctx.Err()
		default:
		}

		line := scanner.Text()
		// RFC 8895 allows optional space after "data:" colon.
		data, ok := strings.CutPrefix(line, "data:")
		if !ok {
			continue
		}
		data = strings.TrimPrefix(data, " ")
		if data == "[DONE]" {
			break
		}

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content          string `json:"content"`
					ReasoningContent string `json:"reasoning_content,omitempty"`
					Role             string `json:"role"`
					ToolCalls        []struct {
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
		// B-02 FIX: Capture usage BEFORE the empty-choices check.
		// OpenAI sends the final usage in a chunk with empty choices.
		if chunk.Usage != nil {
			usage = TokenUsage{
				PromptTokens:     chunk.Usage.PromptTokens,
				CompletionTokens: chunk.Usage.CompletionTokens,
				TotalTokens:      chunk.Usage.TotalTokens,
			}
		}
		if len(chunk.Choices) == 0 {
			continue
		}

		// Accumulate reasoning_content (DeepSeek/o1 emit this during streaming)
		if chunk.Choices[0].Delta.ReasoningContent != "" {
			reasoningBuilder.WriteString(chunk.Choices[0].Delta.ReasoningContent)
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
					return nil, 0, err
				}
				deltasSent++
			}
		}

		// Handle tool call deltas
		for _, tcDelta := range chunk.Choices[0].Delta.ToolCalls {
			idx := tcDelta.Index
			if accum, exists := toolCallAccums[idx]; exists {
				// B-10 FIX: Update ID and name if provided in later deltas.
				// Some providers send the ID and name in separate chunks.
				if tcDelta.ID != "" {
					accum.ID = tcDelta.ID
				}
				if tcDelta.Function.Name != "" {
					accum.Name = tcDelta.Function.Name
				}
				accum.Arguments.WriteString(tcDelta.Function.Arguments)
			} else {
				toolCallAccums[idx] = &toolCallAccum{
					ID:   tcDelta.ID,
					Name: tcDelta.Function.Name,
				}
				toolCallAccums[idx].Arguments.WriteString(tcDelta.Function.Arguments)
			}
		}

		if chunk.Choices[0].FinishReason != nil {
			finishReason = *chunk.Choices[0].FinishReason
		}
	}
	resp.Body.Close()

	if err := scanner.Err(); err != nil {
		// B-03 FIX: Save partial state so retry can skip delivered deltas.
		savePartialState()
		// S3-9 FIX: body is already closed above; don't return resp.
		return nil, 0, &ClientError{Message: "stream read failed", Cause: err}
	}

	// Build tool calls from accumulators, sorted by index for deterministic order.
	// B-09 FIX: Ranging over map[int]*toolCallAccum gives non-deterministic
	// iteration order, which can reverse multi-tool turns.
	var toolCalls []ToolCall
	indices := make([]int, 0, len(toolCallAccums))
	for idx := range toolCallAccums {
		indices = append(indices, idx)
	}
	sort.Ints(indices)
	for _, idx := range indices {
		accum := toolCallAccums[idx]
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
		Reasoning:    reasoningBuilder.String(),
	}

	return result, resp.StatusCode, nil
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

// errorStatusAndHeader extracts the failure's HTTP status and headers so the
// loops can call Classify (D4). RateLimitError wraps the APIError cause, and
// doStreamRequest returns bare APIErrors; the status alone is authoritative
// here (quota-shaped responses were already resolved to QuotaResetError by
// the request layer, and Classify only needs the status for the
// throttle/server/fatal buckets once quota is off the table).
func errorStatusAndHeader(err error) (int, http.Header) {
	var rlErr *RateLimitError
	if errors.As(err, &rlErr) {
		var apiErr *APIError
		if errors.As(err, &apiErr) {
			return apiErr.StatusCode, nil
		}
		return http.StatusTooManyRequests, nil
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode, nil
	}
	return 0, nil
}

// errorRetryAt surfaces a server-provided retry instant from the error chain
// for the plan's prior-instant input: a RateLimitError carries RetryAfter as
// a duration from the exhaustion moment. Zero when absent (plan step only).
func errorRetryAt(err error) time.Time {
	if err == nil {
		return time.Time{}
	}
	var rlErr *RateLimitError
	if errors.As(err, &rlErr) && rlErr.RetryAfter > 0 {
		return time.Now().Add(rlErr.RetryAfter)
	}
	return time.Time{}
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

// acquireConcurrencyLimit blocks until the gate grants a slot or context is
// cancelled. Returns a release function that must be called when the request
// completes. If no limit is configured (gate is nil), returns a no-op release
// function. priority=true (interactive chat turn) jumps background waiters,
// bounded by the starvation guard (tree 04 leaf 03, D11 two-tier rule).
func (c *Client) acquireConcurrencyLimit(ctx context.Context, priority bool) (release func(), err error) {
	if c.concurrencyGate == nil {
		return func() {}, nil
	}

	if err := c.concurrencyGate.acquire(ctx, priority); err != nil {
		return nil, err
	}
	return func() {
		c.concurrencyGate.release()
	}, nil
}
