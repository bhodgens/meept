package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/caimlas/meept/internal/errcls"
	"github.com/caimlas/meept/internal/llm"
)

// --- Error classification and retry budget ---

// ErrRetryBudgetExhausted indicates no retries remain.
var ErrRetryBudgetExhausted = errors.New("retry budget exhausted")

// RetryableError indicates an error that may be retried. Implementations
// provide a retry-after hint and a retryable flag so callers can make
// informed decisions without type-switching on concrete error types.
type RetryableError interface {
	error
	// RetryAfter returns the recommended wait time before retrying.
	// Returns 0 if no specific wait time is recommended.
	RetryAfter() time.Duration
	// IsRetryable returns true if this specific error instance should be retried.
	IsRetryable() bool
}

// retryableError wraps an error with retry metadata.
type retryableError struct {
	err        error
	retryable  bool
	retryAfter time.Duration
	reason     string
}

func (e *retryableError) Error() string {
	if e.reason != "" {
		return e.err.Error() + " (" + e.reason + ")"
	}
	return e.err.Error()
}

func (e *retryableError) Unwrap() error {
	return e.err
}

func (e *retryableError) RetryAfter() time.Duration {
	return e.retryAfter
}

func (e *retryableError) IsRetryable() bool {
	return e.retryable
}

// Retryable wraps an error as retryable with the given metadata.
// Returns nil if err is nil (nil guard).
func Retryable(err error, retryable bool, retryAfter time.Duration, reason string) error {
	if err == nil {
		return nil
	}
	return &retryableError{
		err:        err,
		retryable:  retryable,
		retryAfter: retryAfter,
		reason:     reason,
	}
}

// IsRetryable returns true if the error is retryable.
// It first checks the RetryableError interface, then delegates to
// errcls.IsRetryable for network/HTTP/rate-limit classification, and
// finally falls back to string matching for common transient error messages.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}

	// 1. Try the RetryableError interface (covers errors wrapped via Retryable()).
	var re RetryableError
	if errors.As(err, &re) {
		return re.IsRetryable()
	}

	// 2. llm.RateLimitError is always retryable.
	var rateLimitErr *llm.RateLimitError
	if errors.As(err, &rateLimitErr) {
		return true
	}

	// 3. Delegate to errcls for structured network/HTTP/context classification.
	if errcls.IsRetryable(err) {
		return true
	}

	// 4. HTTPError with a retryable status code.
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		return isRetryableHTTPStatus(httpErr.StatusCode)
	}

	// 5. String fallback for common transient error messages that lack
	// structured error types.
	errMsg := err.Error()
	if strings.Contains(errMsg, "connection reset") ||
		strings.Contains(errMsg, "connection refused") ||
		strings.Contains(errMsg, "EOF") {
		return true
	}

	return false
}

// GetRetryAfter extracts a retry-after hint from an error if available.
// Returns 0 when there is no hint.
func GetRetryAfter(err error) time.Duration {
	if err == nil {
		return 0
	}

	var re RetryableError
	if errors.As(err, &re) {
		return re.RetryAfter()
	}

	var rateLimitErr *llm.RateLimitError
	if errors.As(err, &rateLimitErr) {
		return rateLimitErr.RetryAfter
	}

	return 0
}

// isRetryableHTTPStatus returns true for HTTP status codes that are worth retrying.
func isRetryableHTTPStatus(code int) bool {
	switch code {
	case http.StatusTooManyRequests, // 429
		http.StatusBadGateway,          // 502
		http.StatusServiceUnavailable,  // 503
		http.StatusGatewayTimeout,      // 504
		http.StatusRequestTimeout,      // 408
		http.StatusInternalServerError, // 500 (sometimes transient)
		http.StatusConflict:            // 409 (sometimes transient, e.g., concurrent modification)
		return true
	default:
		return false
	}
}

// HTTPError represents an HTTP error response with status code, body, and URL.
// It implements the IsRetryable() bool method but does NOT satisfy the
// RetryableError interface (no RetryAfter method).
type HTTPError struct {
	StatusCode int
	Body       string
	URL        string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Body)
}

// IsRetryable returns true if the HTTP status code warrants a retry.
func (e *HTTPError) IsRetryable() bool {
	return isRetryableHTTPStatus(e.StatusCode)
}

// RetryBudget tracks how many retries remain for an operation chain.
type RetryBudget struct {
	mu      sync.RWMutex
	max     int
	current int
	usedBy  map[string]int // operation -> attempts used
}

// NewRetryBudget creates a new retry budget.
func NewRetryBudget(max int) *RetryBudget {
	return &RetryBudget{
		max:     max,
		current: max,
		usedBy:  make(map[string]int),
	}
}

// TryUse attempts to use one retry for the given operation (label).
// Returns true if successful (budget available), false if exhausted.
func (b *RetryBudget) TryUse(operation string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.current <= 0 {
		return false
	}

	b.current--
	b.usedBy[operation]++
	return true
}

// Remaining returns the number of retries left.
func (b *RetryBudget) Remaining() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.current
}

// Used returns the number of retries used.
func (b *RetryBudget) Used() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.max - b.current
}

// UsedBy returns how many retries a specific operation used.
func (b *RetryBudget) UsedBy(operation string) int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.usedBy[operation]
}

// Reset restores the budget to full.
func (b *RetryBudget) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.current = b.max
	b.usedBy = make(map[string]int)
}

// BackoffConfig configures exponential backoff behavior.
type BackoffConfig struct {
	// BaseDelay is the initial delay before the first retry.
	BaseDelay time.Duration
	// MaxDelay caps the maximum delay between retries.
	MaxDelay time.Duration
	// Multiplier is the factor by which delay increases each attempt.
	Multiplier float64
	// Jitter is the randomness factor (0.0 to 1.0).
	// 0 = no jitter, 1 = full jitter (random between 0 and calculated delay)
	Jitter float64
	// MaxAttempts is the maximum number of retry attempts (0 = unlimited).
	MaxAttempts int
}

// defaultBackoffOverride holds optional config-driven defaults that take
// precedence over the hardcoded presets when set. Package-level to avoid
// threading the config through every call site; the daemon sets this once
// at startup via SetDefaultBackoffOverride.
var defaultBackoffOverride atomic.Pointer[BackoffConfig]

// perOperationOverrides holds config-driven backoff overrides keyed by
// operation category ("llm", "tool_web", "tool_shell", "http"). The daemon
// populates this from config.Agent.Retry.PerOperation.
var perOperationOverrides sync.Map // map[string]BackoffConfig

// SetDefaultBackoffOverride installs config-driven backoff defaults. When
// non-nil, LLMBackoffConfig() and DefaultBackoffConfig() merge the override
// over their hardcoded presets (override fields with non-zero values win).
// Pass a zero-value BackoffConfig or call clearDefaultBackoffOverride to
// reset. Safe for concurrent use.
func SetDefaultBackoffOverride(cfg BackoffConfig) {
	defaultBackoffOverride.Store(&cfg)
}

// clearDefaultBackoffOverride is a test-only helper to reset the override.
func clearDefaultBackoffOverride() {
	defaultBackoffOverride.Store(nil)
}

// SetPerOperationBackoffOverride installs a config-driven backoff override
// for a specific operation category. Preset functions (LLMBackoffConfig,
// ToolBackoffConfig, HTTPHookBackoffConfig, and tool-profile lookups via
// getRetryConfigForTool) consult this map first; the global override
// applies as a fallback. Safe for concurrent use.
func SetPerOperationBackoffOverride(key string, cfg BackoffConfig) {
	perOperationOverrides.Store(key, cfg)
}

// getPerOperationOverride returns the override for a key, or nil if unset.
func getPerOperationOverride(key string) *BackoffConfig {
	if v, ok := perOperationOverrides.Load(key); ok {
		c := v.(BackoffConfig)
		return &c
	}
	return nil
}

// clearPerOperationOverrides is a test-only helper.
func clearPerOperationOverrides() {
	perOperationOverrides.Range(func(k, _ any) bool {
		perOperationOverrides.Delete(k)
		return true
	})
}

// applyOverride applies non-zero fields from the override pointer (if non-nil)
// to a base config. Zero-value fields in the override are ignored so callers
// can override only the fields they care about. If ov is nil, base is returned
// unchanged.
func applyOverride(base BackoffConfig, ov *BackoffConfig) BackoffConfig {
	if ov == nil {
		return base
	}
	if ov.BaseDelay > 0 {
		base.BaseDelay = ov.BaseDelay
	}
	if ov.MaxDelay > 0 {
		base.MaxDelay = ov.MaxDelay
	}
	if ov.Multiplier > 0 {
		base.Multiplier = ov.Multiplier
	}
	if ov.Jitter > 0 {
		base.Jitter = ov.Jitter
	}
	if ov.MaxAttempts > 0 {
		base.MaxAttempts = ov.MaxAttempts
	}
	return base
}

// mergeOverride applies the global override (if set) to a preset config.
// Kept as a convenience wrapper around applyOverride for backward
// compatibility.
func mergeOverride(preset BackoffConfig) BackoffConfig {
	return applyOverride(preset, defaultBackoffOverride.Load())
}

// DefaultBackoffConfig returns sensible defaults for general backoff.
// When a config-driven override is installed via SetDefaultBackoffOverride,
// non-zero fields from the override take precedence.
func DefaultBackoffConfig() BackoffConfig {
	return mergeOverride(BackoffConfig{
		BaseDelay:   1 * time.Second,
		MaxDelay:    30 * time.Second,
		Multiplier:  2.0,
		Jitter:      0.3, // 30% jitter to prevent thundering herd
		MaxAttempts: 10,
	})
}

// AggressiveBackoffConfig returns a more aggressive backoff for time-sensitive operations.
func AggressiveBackoffConfig() BackoffConfig {
	return BackoffConfig{
		BaseDelay:   500 * time.Millisecond,
		MaxDelay:    10 * time.Second,
		Multiplier:  1.5,
		Jitter:      0.2,
		MaxAttempts: 5,
	}
}

// ConservativeBackoffConfig returns a conservative backoff for non-urgent operations.
func ConservativeBackoffConfig() BackoffConfig {
	return BackoffConfig{
		BaseDelay:   5 * time.Second,
		MaxDelay:    120 * time.Second,
		Multiplier:  2.0,
		Jitter:      0.5,
		MaxAttempts: 15,
	}
}

// LLMBackoffConfig returns a backoff profile tuned for LLM API calls.
// Resolution order: per-operation override for "llm" takes highest precedence,
// then global override, then hardcoded preset.
func LLMBackoffConfig() BackoffConfig {
	preset := BackoffConfig{
		BaseDelay:   1 * time.Second,
		MaxDelay:    30 * time.Second,
		Multiplier:  2.0,
		Jitter:      0.3,
		MaxAttempts: 5,
	}
	merged := mergeOverride(preset) // apply global first
	return applyOverride(merged, getPerOperationOverride("llm"))
}

// ToolBackoffConfig returns a backoff profile tuned for tool execution.
func ToolBackoffConfig() BackoffConfig {
	return BackoffConfig{
		BaseDelay:   1 * time.Second,
		MaxDelay:    30 * time.Second,
		Multiplier:  2.0,
		Jitter:      0.3,
		MaxAttempts: 5,
	}
}

// HTTPHookBackoffConfig returns a backoff profile for HTTP hook requests.
// More aggressive than default to support the retry_count pattern of HTTP hooks.
// Resolution order: per-operation override for "http" → hardcoded preset.
func HTTPHookBackoffConfig(count int) BackoffConfig {
	preset := BackoffConfig{
		BaseDelay:   500 * time.Millisecond,
		MaxDelay:    10 * time.Second,
		Multiplier:  2.0,
		Jitter:      0.2,
		MaxAttempts: count,
	}
	return applyOverride(preset, getPerOperationOverride("http"))
}

// Backoff implements exponential backoff with jitter.
type Backoff struct {
	config  BackoffConfig
	attempt int
	mu      sync.Mutex
	rng     *rand.Rand
	logger  *slog.Logger
}

// NewBackoff creates a new backoff instance with a deterministic-seeded RNG.
// The logger defaults to slog.Default(); override with WithLogger.
func NewBackoff(config BackoffConfig) *Backoff {
	return &Backoff{
		config:  config,
		rng:     rand.New(rand.NewSource(time.Now().UnixNano())),
		attempt: 0,
		logger:  slog.Default(),
	}
}

// WithLogger sets the structured logger for backoff events. Returns the same
// Backoff pointer for chaining. If l is nil, the logger is left unchanged.
func (b *Backoff) WithLogger(l *slog.Logger) *Backoff {
	if l != nil {
		b.logger = l
	}
	return b
}

// NextDelay returns the next delay duration and whether more retries are allowed.
// The delay is exponential with jitter applied. Returns (0, false) when
// max attempts have been exhausted.
func (b *Backoff) NextDelay() (delay time.Duration, ok bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Check if max attempts exceeded
	if b.config.MaxAttempts > 0 && b.attempt >= b.config.MaxAttempts {
		return 0, false
	}

	// Calculate exponential delay: baseDelay * multiplier^attempt
	multiplier := 1.0
	for i := 0; i < b.attempt; i++ {
		multiplier *= b.config.Multiplier
	}
	delay = time.Duration(float64(b.config.BaseDelay) * multiplier)

	// Apply max delay cap
	if delay > b.config.MaxDelay {
		delay = b.config.MaxDelay
	}

	// Apply jitter: range is [-jitterRange, +jitterRange] around the base delay
	if b.config.Jitter > 0 {
		jitterRange := float64(delay) * b.config.Jitter
		jitter := b.rng.Float64()*jitterRange*2 - jitterRange
		delay = delay + time.Duration(jitter)
		// Enforce a 100ms floor to prevent retry storms.
		if delay < 100*time.Millisecond {
			delay = 100 * time.Millisecond
		}
	}

	b.attempt++
	return delay, true
}

// Reset resets the backoff to initial state (zero attempts).
func (b *Backoff) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.attempt = 0
}

// Sleep waits for the backoff delay or until context is cancelled.
// Returns true if the wait completed and another retry should proceed,
// false if context was cancelled or max attempts reached.
func (b *Backoff) Sleep(ctx context.Context) bool {
	delay, ok := b.NextDelay()
	if !ok {
		b.logger.Debug("backoff max attempts reached",
			"attempt", b.attempt,
			"max_attempts", b.config.MaxAttempts,
		)
		return false
	}

	b.logger.Debug("backoff waiting",
		"attempt", b.attempt,
		"delay_ms", delay.Milliseconds(),
	)

	select {
	case <-ctx.Done():
		b.logger.Debug("backoff interrupted by context cancellation")
		return false
	case <-time.After(delay):
		return true
	}
}

// RunWithRetry executes a function with exponential backoff retry.
// It retries when the function returns a retryable error, using the configured
// backoff between attempts. Non-retryable errors are returned immediately
// without waiting. If a retry budget is attached to the context, it is checked
// before each retry attempt.
//
// The function is called at least once. Retries happen only on retryable errors.
// If the context is cancelled during a wait, RunWithRetry returns ctx.Err().
func RunWithRetry[T any](ctx context.Context, config BackoffConfig, fn func() (T, error)) (T, error) {
	var zero T
	backoff := NewBackoff(config)

	var lastErr error
	var first bool = true
	var attempt int

	for {
		result, err := fn()
		if err == nil {
			if attempt > 0 {
				backoff.logger.Debug("retry succeeding after attempts",
					"attempts", attempt+1,
				)
			}
			return result, nil
		}

		if first {
			first = false
			lastErr = err
		}

		// Check if error is retryable
		if !IsRetryable(err) {
			backoff.logger.Debug("retry stopped: non-retryable error",
				"error", err.Error(),
			)
			return zero, err
		}

		// Check retry budget if present
		if budget := GetRetryBudget(ctx); budget != nil {
			if !budget.TryUse("operation") {
				return zero, ErrRetryBudgetExhausted
			}
		}

		backoff.logger.Debug("retrying after error",
			"attempt", attempt,
			"error", err.Error(),
		)
		attempt++

		// If error carries a Retry-After hint, honor it instead of backoff.
		if retryAfter := GetRetryAfter(err); retryAfter > 0 {
			select {
			case <-ctx.Done():
				return zero, ctx.Err()
			case <-time.After(retryAfter):
				continue
			}
		}

		// Use backoff delay
		if !backoff.Sleep(ctx) {
			if ctx.Err() != nil {
				return zero, ctx.Err()
			}
			// Max attempts exhausted
			return zero, lastErr
		}
	}
}

// retryBudgetKey is the context key type for retry budget.
type retryBudgetKey struct{}

// WithRetryBudget attaches a retry budget to a context.
func WithRetryBudget(ctx context.Context, budget *RetryBudget) context.Context {
	return context.WithValue(ctx, retryBudgetKey{}, budget)
}

// GetRetryBudget extracts retry budget from context.
func GetRetryBudget(ctx context.Context) *RetryBudget {
	if v := ctx.Value(retryBudgetKey{}); v != nil {
		return v.(*RetryBudget)
	}
	return nil
}
