package agent

import (
	"context"
	"errors"
	"math/rand"
	"sync"
	"time"
)

// --- WIP stubs: definitions for stubbed-in symbols from backoff enhancement plan ---

// RetryBudget tracks how many retries are still allowed.
type RetryBudget struct {
	mu      sync.Mutex
	allowed int
}

// TryUse decrements the budget and returns true if a retry is allowed.
func (rb *RetryBudget) TryUse(label string) bool {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	if rb.allowed > 0 {
		rb.allowed--
		return true
	}
	return false
}

var ErrRetryBudgetExhausted = errors.New("retry budget exhausted")

// IsRetryable checks whether an error qualifies for automatic retry.
func IsRetryable(err error) bool {
	return err != nil && errors.Is(err, context.DeadlineExceeded)
}

// GetRetryAfter extracts a Retry-After hint from an error, if present.
// Returns 0 when there is no hint.
func GetRetryAfter(err error) time.Duration { return 0 }

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

// DefaultBackoffConfig returns sensible defaults for general backoff.
func DefaultBackoffConfig() BackoffConfig {
	return BackoffConfig{
		BaseDelay:   1 * time.Second,
		MaxDelay:    30 * time.Second,
		Multiplier:  2.0,
		Jitter:      0.3, // 30% jitter to prevent thundering herd
		MaxAttempts: 10,
	}
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
func LLMBackoffConfig() BackoffConfig {
	return BackoffConfig{
		BaseDelay:   1 * time.Second,
		MaxDelay:    30 * time.Second,
		Multiplier:  2.0,
		Jitter:      0.3,
		MaxAttempts: 5,
	}
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
func HTTPHookBackoffConfig(count int) BackoffConfig {
	return BackoffConfig{
		BaseDelay:   500 * time.Millisecond,
		MaxDelay:    10 * time.Second,
		Multiplier:  2.0,
		Jitter:      0.2,
		MaxAttempts: count,
	}
}

// Backoff implements exponential backoff with jitter.
type Backoff struct {
	config  BackoffConfig
	attempt int
	mu      sync.Mutex
	rng     *rand.Rand
}

// NewBackoff creates a new backoff instance with a deterministic-seeded RNG.
func NewBackoff(config BackoffConfig) *Backoff {
	return &Backoff{
		config:  config,
		rng:     rand.New(rand.NewSource(time.Now().UnixNano())),
		attempt: 0,
	}
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
		// Ensure minimum delay (even with negative jitter)
		if delay < 100*time.Millisecond && b.config.BaseDelay < 100*time.Millisecond {
			delay = b.config.BaseDelay
		}
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
		return false
	}

	select {
	case <-ctx.Done():
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

	for {
		result, err := fn()
		if err == nil {
			return result, nil
		}

		if first {
			first = false
			lastErr = err
		}

		// Check if error is retryable
		if !IsRetryable(err) {
			return zero, err
		}

		// Check retry budget if present
		if budget := GetRetryBudget(ctx); budget != nil {
			if !budget.TryUse("operation") {
				return zero, ErrRetryBudgetExhausted
			}
		}

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
