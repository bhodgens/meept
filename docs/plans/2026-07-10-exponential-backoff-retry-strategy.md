# Plan: Exponential Backoff and Retry Strategy

**Status:** Proposed
**Created:** 2026-07-10
**Priority:** High (directly impacts reliability during rate limit events)
**Risk:** Low (additive retry logic, existing code already has basic retry)

---

## Summary

Implement a comprehensive exponential backoff and retry strategy for Meept's agent operations, particularly for:

1. **LLM API calls** - Rate limits, transient errors, provider failures
2. **Tool executions** - Network timeouts, temporary unavailability
3. **MCP server operations** - Connection failures, server restarts

The system already has basic retry logic in `loop.go:chatWithFailover()` and `http_hooks.go`, but it should be enhanced with:

1. **Exponential backoff with jitter** - Prevents thundering herd, respects rate limits
2. **Retry budgets** - Limit total retry attempts across nested operations
3. **Error classification** - Distinguish retryable vs. non-retryable errors
4. **Per-operation policies** - Different retry strategies for different operation types

---

## Current State Analysis

### Existing Retry Logic

**Location:** `internal/agent/loop.go:3118-3200` (model failover with backoff)

```go
const maxBackoff = 30 * time.Second
baseBackoff := 2 * time.Second
// ...
currentBackoff := baseBackoff
for attempt := 0; ; attempt++ {
    response, err := l.chatWithFailover(ctx, messages, opts...)
    if err == nil {
        return response, nil
    }

    // Check if rate limit error
    var rateLimitErr *RateLimitError
    if errors.As(err, &rateLimitErr) {
        waitTime := rateLimitErr.RetryAfter
        if waitTime == 0 {
            waitTime = currentBackoff
            currentBackoff = time.Duration(float64(currentBackoff) * 2)
            currentBackoff = min(currentBackoff, maxBackoff)
        }
        time.Sleep(waitTime)
        continue
    }
    // ...
}
```

**Location:** `internal/agent/http_hooks.go:237-260` (HTTP request retry)

```go
for attempt := 0; attempt <= h.config.RetryCount; attempt++ {
    resp, err := httpClient.Do(req)
    if err != nil {
        if attempt < h.config.RetryCount {
            time.Sleep(1 * time.Second)  // Fixed delay, not exponential
        }
        continue
    }
    // ...
}
```

### Strengths
- ✅ Basic exponential backoff exists for rate limit errors
- ✅ Respects `Retry-After` header when provided
- ✅ Has maximum backoff cap (30s)

### Gaps
- ❌ Fixed delay in HTTP hooks (not exponential)
- ❌ No jitter - concurrent requests may sync up and thunder
- ❌ No retry budget tracking across nested operations
- ❌ Limited error classification (what's retryable vs. not)
- ❌ No per-operation retry policies
- ❌ No visibility into retry attempts (metrics/logging)

---

## Objectives

| Objective | Success Metric |
|-----------|----------------|
| **O1: Exponential Backoff with Jitter** | All retry logic uses exponential backoff + random jitter |
| **O2: Retry Budget Tracking** | Operations carry retry budgets that decrease with each retry |
| **O3: Error Classification** | Clear distinction between retryable and non-retryable errors |
| **O4: Per-Operation Policies** | Different operation types have appropriate retry configs |
| **O5: Observability** | Retry attempts are logged and exported as metrics |

---

## Implementation Phases

### Phase 1: Error Classification System

**Goal:** Define which errors are retryable and under what conditions.

#### 1.1: Define Error Types and Classification

**File:** `internal/agent/retry_errors.go` (new)

```go
package agent

import (
    "errors"
    "net"
    "net/http"
    "strings"
    "time"
)

// RetryableError indicates an error that may be retried.
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
func IsRetryable(err error) bool {
    if err == nil {
        return false
    }

    var re RetryableError
    if errors.As(err, &re) {
        return re.IsRetryable()
    }

    // Default classification for unwrapped errors
    return isRetryableByDefault(err)
}

// isRetryableByDefault classifies errors based on type.
func isRetryableByDefault(err error) bool {
    // Network errors are often transient
    var netErr net.Error
    if errors.As(err, &netErr) {
        return netErr.Temporary() || netErr.Timeout()
    }

    // HTTP errors
    var httpErr *HTTPError
    if errors.As(err, &httpErr) {
        return isRetryableHTTPStatus(httpErr.StatusCode)
    }

    // Rate limit errors
    var rateLimitErr *RateLimitError
    if errors.As(err, &rateLimitErr) {
        return true
    }

    // Connection errors
    errMsg := err.Error()
    if strings.Contains(errMsg, "connection reset") ||
       strings.Contains(errMsg, "connection refused") ||
       strings.Contains(errMsg, "EOF") {
        return true
    }

    return false
}

// isRetryableHTTPStatus returns true for HTTP status codes that are worth retrying.
func isRetryableHTTPStatus(code int) bool {
    switch code {
    case http.StatusTooManyRequests,      // 429
         http.StatusBadGateway,           // 502
         http.StatusServiceUnavailable,   // 503
         http.StatusGatewayTimeout,       // 504
         http.StatusRequestTimeout,       // 408
         http.StatusInternalServerError,  // 500 (sometimes transient)
         http.StatusConflict:             // 409 (sometimes transient, e.g., concurrent modification)
        return true
    default:
        return false
    }
}

// GetRetryAfter extracts retry-afterfrom an error if available.
func GetRetryAfter(err error) time.Duration {
    if err == nil {
        return 0
    }

    var re RetryableError
    if errors.As(err, &re) {
        return re.RetryAfter()
    }

    var rateLimitErr *RateLimitError
    if errors.As(err, &rateLimitErr) {
        return rateLimitErr.RetryAfter
    }

    return 0
}

// RetryBudget tracks how many retries remain for an operation chain.
type RetryBudget struct {
    mu       sync.Mutex
    max      int
    current  int
    usedBy   map[string]int // operation -> attempts used
}

// NewRetryBudget creates a new retry budget.
func NewRetryBudget(max int) *RetryBudget {
    return &RetryBudget{
        max:     max,
        current: max,
        usedBy:  make(map[string]int),
    }
}

// TryUse attempts to use one retry for the given operation.
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
    b.mu.Lock()
    defer b.mu.Unlock()
    return b.current
}

// Used returns the number of retries used.
func (b *RetryBudget) Used() int {
    b.mu.Lock()
    defer b.mu.Unlock()
    return b.max - b.current
}

// UsedBy returns how many retries a specific operation used.
func (b *RetryBudget) UsedBy(operation string) int {
    b.mu.Lock()
    defer b.mu.Unlock()
    return b.usedBy[operation]
}

// Reset restores the budget to full.
func (b *RetryBudget) Reset() {
    b.mu.Lock()
    defer b.mu.Unlock()
    b.current = b.max
    b.usedBy = make(map[string]int)
}
```

#### 1.2: Define Standard Error Types

```go
// RateLimitError indicates rate limit exceeded.
type RateLimitError struct {
    err        error
    RetryAfter time.Duration // Recommended wait time
    Limit      int           // Rate limit (if known)
    Remaining  int           // Remaining quota (if known)
    ResetAt    time.Time     // When limit resets (if known)
}

func (e *RateLimitError) Error() string {
    return fmt.Sprintf("rate limit exceeded: %v", e.err)
}

func (e *RateLimitError) Unwrap() error {
    return e.err
}

func (e *RateLimitError) RetryAfter() time.Duration {
    if e.RetryAfter > 0 {
        return e.RetryAfter
    }
    return 5 * time.Second // Default
}

func (e *RateLimitError) IsRetryable() bool {
    return true
}

// HTTPError represents an HTTP error response.
type HTTPError struct {
    StatusCode int
    Body       string
    URL        string
}

func (e *HTTPError) Error() string {
    return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Body)
}

func (e *HTTPError) IsRetryable() bool {
    return isRetryableHTTPStatus(e.StatusCode)
}
```

#### 1.3: Unit Tests

**File:** `internal/agent/retry_errors_test.go`

```go
func TestIsRetryable_NetworkErrors(t *testing.T) {
    err := context.DeadlineExceeded
    if !IsRetryable(err) {
        t.Error("deadline exceeded should be retryable")
    }
}

func TestIsRetryable_HTTPStatuses(t *testing.T) {
    tests := []struct {
        code int
        want bool
    }{
        {429, true},
        {502, true},
        {503, true},
        {400, false},
        {401, false},
        {404, false},
    }
    for _, tt := range tests {
        if got := isRetryableHTTPStatus(tt.code); got != tt.want {
            t.Errorf("HTTP %d: want %v, got %v", tt.code, tt.want, got)
        }
    }
}

func TestRetryBudget_Exhaustion(t *testing.T) {
    budget := NewRetryBudget(3)
    for i := 0; i < 3; i++ {
        if !budget.TryUse("op1") {
            t.Errorf("retry %d should succeed", i)
        }
    }
    if budget.TryUse("op1") {
        t.Error("retry should fail after budget exhausted")
    }
    if budget.Remaining() != 0 {
        t.Errorf("expected 0 remaining, got %d", budget.Remaining())
    }
}
```

**Verification:**
- [ ] All error classification tests pass
- [ ] Retry budget tracking works correctly
- [ ] `go test ./internal/agent -run Retry -v` passes

---

### Phase 2: Exponential Backoff with Jitter

**Goal:** Implement robust backoff algorithm.

#### 2.1: Create Backoff Utility

**File:** `internal/agent/backoff.go` (new)

```go
package agent

import (
    "context"
    "math/rand"
    "sync"
    "time"
)

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

// DefaultBackoffConfig returns sensible defaults for backoff.
func DefaultBackoffConfig() BackoffConfig {
    return BackoffConfig{
        BaseDelay:   1 * time.Second,
        MaxDelay:    30 * time.Second,
        Multiplier:  2.0,
        Jitter:      0.3, // 30% jitter
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

// Backoff implements exponential backoff with jitter.
type Backoff struct {
    config BackoffConfig
    attempt int
    mu      sync.Mutex
    rng     *rand.Rand
}

// NewBackoff creates a new backoff instance.
func NewBackoff(config BackoffConfig) *Backoff {
    return &Backoff{
        config: config,
        rng:    rand.New(rand.NewSource(time.Now().UnixNano())),
    }
}

// NextDelay returns the next delay duration and whether more retries are allowed.
func (b *Backoff) NextDelay() (delay time.Duration, ok bool) {
    b.mu.Lock()
    defer b.mu.Unlock()

    // Check if max attempts exceeded
    if b.config.MaxAttempts > 0 && b.attempt >= b.config.MaxAttempts {
        return 0, false
    }

    // Calculate exponential delay
    multiplier := 1.0
    for i := 0; i < b.attempt; i++ {
        multiplier *= b.config.Multiplier
    }
    delay = time.Duration(float64(b.config.BaseDelay) * multiplier)

    // Apply max delay cap
    if delay > b.config.MaxDelay {
        delay = b.config.MaxDelay
    }

    // Apply jitter
    if b.config.Jitter > 0 {
        jitterRange := float64(delay) * b.config.Jitter
        jitter := b.rng.Float64()*jitterRange*2 - jitterRange
        delay = delay + time.Duration(jitter)
        if delay < 0 {
            delay = 100 * time.Millisecond // minimum delay
        }
    }

    b.attempt++
    return delay, true
}

// Reset resets the backoff to initial state.
func (b *Backoff) Reset() {
    b.mu.Lock()
    defer b.mu.Unlock()
    b.attempt = 0
}

// Sleep waits for the backoff delay or until context is cancelled.
// Returns true if should continue, false if cancelled or max attempts reached.
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
func RunWithRetry[T any](ctx context.Context, config BackoffConfig, fn func() (T, error)) (T, error) {
    var zero T
    backoff := NewBackoff(config)

    var lastErr error
    for {
        result, err := fn()
        if err == nil {
            return result, nil
        }
        lastErr = err

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

        // Get retry-after from error if available
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
            return zero, ctx.Err()
        }
    }
}

// ErrRetryBudgetExhausted indicates no retries remain.
var ErrRetryBudgetExhausted = errors.New("retry budget exhausted")

// retryBudgetKey is the context key for retry budget.
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
```

#### 2.2: Unit Tests for Backoff

**File:** `internal/agent/backoff_test.go`

```go
func TestBackoff_ExponentialGrowth(t *testing.T) {
    b := NewBackoff(BackoffConfig{
        BaseDelay:  1 * time.Second,
        Multiplier: 2.0,
        MaxDelay:   60 * time.Second,
        Jitter:     0, // No jitter for deterministic test
    })

    expected := []time.Duration{
        1 * time.Second,
        2 * time.Second,
        4 * time.Second,
        8 * time.Second,
    }

    for _, want := range expected {
        got, ok := b.NextDelay()
        if !ok {
            t.Fatal("NextDelay returned false before max attempts")
        }
        if got != want {
            t.Errorf("delay: want %v, got %v", want, got)
        }
    }
}

func TestBackoff_MaxDelay(t *testing.T) {
    b := NewBackoff(BackoffConfig{
        BaseDelay:  1 * time.Second,
        Multiplier: 2.0,
        MaxDelay:   10 * time.Second,
        Jitter:     0,
    })

    // After many iterations, should cap at MaxDelay
    for i := 0; i < 20; i++ {
        delay, _ := b.NextDelay()
        if delay > 10*time.Second {
            t.Errorf("delay %v exceeds max 10s", delay)
        }
    }
}

func TestBackoff_MaxAttempts(t *testing.T) {
    b := NewBackoff(BackoffConfig{
        BaseDelay:   1 * time.Second,
        MaxAttempts: 3,
    })

    for i := 0; i < 3; i++ {
        _, ok := b.NextDelay()
        if !ok {
            t.Errorf("attempt %d should be allowed", i)
        }
    }

    _, ok := b.NextDelay()
    if ok {
        t.Error("attempt 4 should be rejected")
    }
}

func TestRunWithRetry_Success(t *testing.T) {
    attempts := 0
    result, err := RunWithRetry[string](context.Background(), DefaultBackoffConfig(), func() (string, error) {
        attempts++
        return "success", nil
    })

    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if result != "success" {
        t.Errorf("wrong result: %s", result)
    }
    if attempts != 1 {
        t.Errorf("expected 1 attempt, got %d", attempts)
    }
}

func TestRunWithRetry_RetryThenSuccess(t *testing.T) {
    attempts := 0
    result, err := RunWithRetry[string](context.Background(), BackoffConfig{
        BaseDelay:   10 * time.Millisecond,
        MaxDelay:    50 * time.Millisecond,
        MaxAttempts: 5,
        Jitter:      0,
    }, func() (string, error) {
        attempts++
        if attempts < 3 {
            return "", Retryable(errors.New("transient error"), true, 0, "transient")
        }
        return "success", nil
    })

    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if attempts != 3 {
        t.Errorf("expected 3 attempts, got %d", attempts)
    }
}
```

**Verification:**
- [ ] Backoff calculations are correct
- [ ] Max delay cap works
- [ ] Max attempts enforcement works
- [ ] Jitter produces variance

---

### Phase 3: Integrate with LLM Calls

**Goal:** Apply backoff to `chatWithFailover` and model failover.

#### 3.1: Update chatWithFailover

**File:** `internal/agent/loop.go`

```go
// chatWithFailover makes LLM calls with exponential backoff and provider failover.
func (l *AgentLoop) chatWithFailover(ctx context.Context, messages []llm.ChatMessage, opts ...llm.ChatOption) (*llm.ChatCompletionResponse, error) {
    // Create retry budget for this call chain
    budget := NewRetryBudget(5)
    ctx = WithRetryBudget(ctx, budget)

    // Use backoff config appropriate for LLM calls
    backoffConfig := DefaultBackoffConfig()

    result, err := RunWithRetry(ctx, backoffConfig, func() (*llm.ChatCompletionResponse, error) {
        // Existing chat logic with provider failover
        // ...
        return l.llmClient.ChatCompletion(ctx, messages, opts...)
    })

    return result, err
}
```

#### 3.2: Update Model Failover Loop

**File:** `internal/agent/loop.go:3118`

Replace the existing retry loop with backoff utility:

```go
// OLD: Manual backoff loop
// NEW: Use RunWithRetry with proper model failover handling

func (l *AgentLoop) chatWithModelFailover(ctx context.Context, messages []llm.ChatMessage, opts ...llm.ChatOption) (*llm.ChatCompletionResponse, error) {
    // Try configured model first, fail over to alternatives
    modelsToTry := l.getFailoverModels()

    for i, model := range modelsToTry {
        // Each model gets its own retry budget
        modelCtx := WithRetryBudget(ctx, NewRetryBudget(3))

        backoff := NewBackoff(DefaultBackoffConfig())

        for {
            response, err := l.llmClient.ChatCompletion(modelCtx, messages, opts...)
            if err == nil {
                if i > 0 {
                    l.logger.Info("Model failover succeeded",
                        "original_model", l.llmClient.Config().ModelID,
                        "fallback_model", model.ModelID)
                }
                return response, nil
            }

            // Check if error suggests trying another model
            if shouldTryNextModel(err) {
                l.logger.Info("Model failed, trying next",
                    "model", model.ModelID,
                    "error", err)
                break // Try next model
            }

            // Retry same model with backoff
            if !backoff.Sleep(modelCtx) {
                // Max retries exhausted for this model
                // If this was the last model, return error
                if i >= len(modelsToTry)-1 {
                    return nil, err
                }
                break
            }
        }
    }

    return nil, ErrAllModelsFailed
}
```

**Verification:**
- [ ] LLM calls use exponential backoff
- [ ] Model failover works correctly
- [ ] No regression in chat functionality

---

### Phase 4: Integrate with Tool Execution

**Goal:** Apply retry logic to tool execution.

#### 4.1: Update Executor Execute Method

**File:** `internal/agent/executor.go`

```go
func (e *Executor) Execute(ctx context.Context, toolCall llm.ToolCall) *ExecutionResult {
    // Determine retry config based on tool type
    config := e.getRetryConfigForTool(toolCall.Function.Name)

    result, err := RunWithRetry(ctx, config, func() (*ExecutionResult, error) {
        return e.executeWithPermissionCheck(ctx, toolCall), nil
    })

    if err != nil {
        return &ExecutionResult{
            ToolCallID: toolCall.ID,
            Success:    false,
            Error:      err.Error(),
        }
    }

    return result
}

func (e *Executor) getRetryConfigForTool(toolName string) BackoffConfig {
    // Network tools: more retries, longer delays
    if toolName == "web_search" || toolName == "web_fetch" {
        return BackoffConfig{
            BaseDelay:   2 * time.Second,
            MaxDelay:    30 * time.Second,
            Multiplier:  2.0,
            Jitter:      0.3,
            MaxAttempts: 5,
        }
    }

    // Shell commands: fewer retries, shorter delays
    if toolName == "shell" {
        return BackoffConfig{
            BaseDelay:   1 * time.Second,
            MaxDelay:    10 * time.Second,
            Multiplier:  1.5,
            Jitter:      0.2,
            MaxAttempts: 2,
        }
    }

    // Default
    return DefaultBackoffConfig()
}
```

**Verification:**
- [ ] Tool retries work correctly
- [ ] Different tools have appropriate retry configs

---

### Phase 5: Observability and Metrics

**Goal:** Track retry attempts and expose metrics.

#### 5.1: Add Retry Metrics

```go
// In internal/metrics/store.go or new file:

type RetryMetrics struct {
    TotalRetries    int64
    RetriesByType   map[string]int64  // "llm", "tool", "http"
    RetriesByError  map[string]int64  // error type -> count
    BackoffDuration int64             // Total time spent in backoff (ms)
}

func (m *MetricsStore) RecordRetry(ctx context.Context, opType string, err error, delay time.Duration) {
    // Record to SQLite
    m.db.Exec(`
        INSERT INTO retry_metrics (timestamp, operation_type, error_type, delay_ms)
        VALUES (?, ?, ?, ?)
    `, time.Now(), opType, fmt.Sprintf("%T", err), delay.Milliseconds())
}
```

#### 5.2: Add Retry Logging

```go
// In backoff.go:

type Backoff struct {
    // ...
    logger *slog.Logger
}

func (b *Backoff) Sleep(ctx context.Context) bool {
    delay, ok := b.NextDelay()
    if !ok {
        b.logger.Debug("Backoff max attempts reached")
        return false
    }

    b.logger.Debug("Backoff waiting",
        "attempt", b.attempt,
        "delay_ms", delay.Milliseconds())

    select {
    case <-ctx.Done():
        b.logger.Debug("Backoff interrupted by context cancellation")
        return false
    case <-time.After(delay):
        return true
    }
}
```

**Verification:**
- [ ] Retry metrics are recorded
- [ ] Logs show retry attempts with timing

---

## Testing Strategy

### Unit Tests

| Test Case | File | Description |
|-----------|------|-------------|
| `TestIsRetryable_NetworkErrors` | `retry_errors_test.go` | Classify network errors |
| `TestIsRetryable_HTTPStatuses` | `retry_errors_test.go` | Classify HTTP status codes |
| `TestRetryBudget_Exhaustion` | `retry_errors_test.go` | Budget tracking |
| `TestBackoff_ExponentialGrowth` | `backoff_test.go` | Delay calculation |
| `TestBackoff_MaxDelay` | `backoff_test.go` | Max delay cap |
| `TestRunWithRetry_Success` | `backoff_test.go` | Success path |
| `TestRunWithRetry_RetryThenSuccess` | `backoff_test.go` | Retry then success |

### Integration Tests

| Test Case | Description |
|-----------|-------------|
| `TestLLMCall_WithRateLimit` | LLM rate limit triggers backoff |
| `TestToolExecution_NetworkFailure` | Network tool retries on failure |

---

## Rollback Plan

If issues arise:

1. **Disable via config**: `agent.retry.enabled = false`
2. **Revert to original**: Restore original retry loop in `loop.go`
3. **Increase limits**: If too aggressive, increase retry budgets

---

## Configuration Changes

**File:** `config/agent.json5`

```json5
{
  "agent": {
    "retry": {
      "enabled": true,
      "default_budget": 5,
      "backoff": {
        "base_delay": "1s",
        "max_delay": "30s",
        "multiplier": 2.0,
        "jitter": 0.3
      },
      "per_operation": {
        "llm": { "budget": 5, "base_delay": "1s" },
        "tool_web": { "budget": 3, "base_delay": "2s" },
        "tool_shell": { "budget": 2, "base_delay": "1s" },
        "http": { "budget": 3, "base_delay": "1s" }
      }
    }
  }
}
```

---

## Success Criteria

- [ ] **Phase 1**: Error classification working
- [ ] **Phase 2**: Backoff with jitter implemented
- [ ] **Phase 3**: LLM calls use backoff
- [ ] **Phase 4**: Tool execution uses backoff
- [ ] **Phase 5**: Metrics and logging available
- [ ] **Overall**: Reduced failure rate during transient errors; no infinite retry loops
