# Plan: Structured Error Handling for OpenRouter and Provider-Specific Responses

## Problem

Meept's LLM error handling has several gaps that cause poor behavior with OpenRouter-style responses:

1. **No JSON error body parsing** for OpenAI-compatible providers. OpenRouter returns rich structured errors with `retry_after`, `backoff` strategy, and `retriable` flags — all ignored. Only the HTTP `Retry-After` header is checked.

2. **Anthropic 429 errors skip agent-level failover**. The Anthropic client produces `APIError{429}` not `RateLimitError`, so `chatWithFailover()`'s `errors.As(err, &rateLimitErr)` never matches. Anthropic rate limits get no model rotation or agent-level backoff.

3. **No jitter** in any backoff logic. Under concurrent load, multiple agents retry in lockstep, amplifying rate limit bursts.

4. **Raw error strings shown to users**. The TUI displays `Error: All 3 attempts failed: HTTP 429: {"error":{...}}` — opaque JSON blobs instead of actionable messages.

5. **Provider Manager treats all errors equally** for failover. A 401 auth failure triggers provider rotation just like a 429 rate limit.

## Architecture Overview

```
HTTP Response (429 with JSON body)
    │
    ▼
doRequest() ── parse structured error ──► RateLimitError (rich metadata)
    │                                        │
    ▼                                        ▼
Client Chat() retry loop              chatWithFailover()
  (3 retries, fixed backoff)           (5 retries, rotation, backoff)
    │                                        │
    ▼                                        ▼
ProviderManager (rotate providers)     Agent loop → TUI/CLI
                                           │
                                           ▼
                                    User-friendly error message
```

## Phase 1: Rich Error Types and JSON Body Parsing

**Goal**: Parse structured error responses from providers into typed errors with actionable metadata.

### 1.1 Extend `RateLimitError` with provider metadata

**File**: `internal/llm/errors.go`

Add fields to `RateLimitError`:
- `LimitType string` — e.g., `"tpm_uncached"`, `"rpm"`, `"concurrent"`
- `RetryStrategy RetryStrategy` — structured backoff advice from provider
- `LimitBudget *LimitBudget` — current vs. limit values

New types:

```go
type RetryStrategy struct {
    Type           string        // "tpm_uncached", "rpm", etc.
    InitialDelay   time.Duration // Provider-suggested initial delay
    MaxDelay       time.Duration // Provider-suggested max delay
    Backoff        string        // "exponential", "linear", "fixed"
    BackoffBase    float64       // Exponential base (e.g., 2.0)
    UseJitter      bool          // Provider recommends jitter
}

type LimitBudget struct {
    Used   int // Current usage (e.g., 289280 tokens)
    Limit  int // Maximum allowed (e.g., 200000 tokens)
    Window string // e.g., "per_minute", "per_day"
}
```

### 1.2 Add OpenRouter error response parser

**File**: `internal/llm/errors.go` (new function)

```go
// ParseOpenRouterError extracts structured error info from OpenRouter-style JSON bodies.
// OpenRouter wraps provider errors: {"error":{"message":"...","code":"tpm_uncached_exceeded",...}}
// The inner provider error is often a nested JSON string inside the outer "message" field.
func ParseOpenRouterError(body []byte) *ProviderErrorDetail { ... }
```

OpenRouter error structure (observed from the user's example):
```json
{
  "error": {
    "message": "Error from provider(nw,moonshotai/Kimi-K2.6: 429): {\"error\":{\"type\":\"rate_limit_error\",\"code\":\"tpm_uncached_exceeded\",...}}",
    "code": 429
  }
}
```

The inner JSON string contains:
- `type`: `"rate_limit_error"`, `"authentication_error"`, etc.
- `code`: `"tpm_uncached_exceeded"`, `"rpm_limit"`, etc.
- `message`: Human-readable description
- `retry_after`: Seconds to wait
- `retry_strategy`: `{type, suggested_initial_delay_s, max_delay_s, backoff, backoff_base, jitter}`
- `retriable`: boolean
- `context`: `{budget, in_flight, model, limit_type, tpm_window_tokens, tpm_limit}`

### 1.3 Add Anthropic error response parser

**File**: `internal/llm/errors.go` (new function)

Anthropic errors are simpler:
```json
{"type":"error","error":{"type":"rate_limit_error","message":"..."}}
```

### 1.4 Add provider-agnostic `ProviderErrorDetail` type

```go
type ProviderErrorDetail struct {
    Type         string        // "rate_limit_error", "authentication_error", etc.
    Code         string        // "tpm_uncached_exceeded", "insufficient_quota", etc.
    Message      string        // Human-readable message
    Retriable    bool          // Whether the provider says retry is worthwhile
    RetryAfter   time.Duration // Explicit retry delay
    RetryStrategy *RetryStrategy
    LimitBudget  *LimitBudget
}
```

### 1.5 Wire parsers into `doRequest()`

**File**: `internal/llm/client.go` (OpenAI-compatible client)

In the 429 handler (line ~590), after reading the response body:
1. Try `ParseOpenRouterError(body)` — matches if outer JSON has `error.message` containing provider prefix pattern
2. Try generic JSON parse for `{error:{type,message,code}}`
3. Fall back to raw body string if neither matches
4. Construct `RateLimitError` with all parsed metadata

**File**: `internal/llm/anthropic.go` (Anthropic client)

In the retryable status code handler (currently just stores raw body):
1. If status is 429, construct `RateLimitError` instead of bare `APIError`
2. Parse `Retry-After` header (currently ignored)
3. Extract Anthropic error type from JSON body

**Files to modify**:
- `internal/llm/errors.go` — new types, parsers, tests
- `internal/llm/client.go` — wire OpenRouter parser into 429 handler
- `internal/llm/anthropic.go` — construct `RateLimitError` on 429, parse Retry-After
- `internal/llm/errors_test.go` — test all parsers with real response fixtures

---

## Phase 2: Backoff with Jitter and Provider-Aware Delays

**Goal**: Use provider-recommended backoff strategies and add jitter to prevent thundering herd.

### 2.1 Add jitter to backoff calculations

**File**: `internal/llm/errors.go` (new function)

```go
// BackoffWithJitter computes a backoff duration with optional jitter.
// Uses full jitter strategy: uniform random in [0, delay].
func BackoffWithJitter(delay time.Duration, maxDelay time.Duration, useJitter bool) time.Duration { ... }
```

### 2.2 Update client retry loops to use `RateLimitError.RetryStrategy`

**File**: `internal/llm/client.go`, `internal/llm/anthropic.go`

When a `RateLimitError` has a `RetryStrategy`:
1. Use `RetryStrategy.InitialDelay` as the base delay (instead of fixed 2s)
2. Use `RetryStrategy.BackoffBase` for exponential growth (instead of hardcoded 2.0)
3. Apply jitter if `RetryStrategy.UseJitter` is true
4. Cap at `RetryStrategy.MaxDelay` (instead of no cap)

When `RateLimitError.RetryAfter` is set, use it as a minimum delay.

When no strategy is provided, keep current behavior (2s base, 2x growth).

### 2.3 Update agent failover to respect provider backoff

**File**: `internal/agent/loop.go` (`chatWithFailover()`)

In the rate limit handler (line ~3031):
1. Check `rateLimitErr.RetryStrategy` — if present, use its parameters
2. Apply jitter from the strategy
3. Log the strategy being used for debugging

### 2.4 Add `Retry-After` header parsing to Anthropic client

**File**: `internal/llm/anthropic.go`

In the 429 handler, add:
```go
retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
```

**Files to modify**:
- `internal/llm/errors.go` — `BackoffWithJitter()` function
- `internal/llm/client.go` — use RetryStrategy in backoff calculation
- `internal/llm/anthropic.go` — parse Retry-After, use RetryStrategy
- `internal/agent/loop.go` — respect provider backoff parameters
- Tests for all backoff changes

---

## Phase 3: Consistent RateLimitError Construction Across Providers

**Goal**: Ensure all provider clients produce `RateLimitError` on 429 so agent-level failover works uniformly.

### 3.1 Fix Anthropic client to return `RateLimitError` on 429

**File**: `internal/llm/anthropic.go`

Currently returns `&APIError{StatusCode: 429, Detail: body}`. Change to:
```go
if resp.StatusCode == http.StatusTooManyRequests {
    retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
    detail := parseAnthropicErrorDetail(respBody)
    return nil, &RateLimitError{
        ProviderID: c.providerID,
        ModelID:    c.modelID,
        RetryAfter: retryAfter,
        Cause:      &APIError{StatusCode: resp.StatusCode, Detail: detail},
    }
}
```

### 3.2 Fix agent loop to use `IsRateLimitError()` instead of `errors.As`

**File**: `internal/agent/loop.go` (`chatWithFailover()`)

Change from:
```go
var rateLimitErr *llm.RateLimitError
if errors.As(err, &rateLimitErr) {
```

To:
```go
if llm.IsRateLimitError(err) {
    var rateLimitErr *llm.RateLimitError
    errors.As(err, &rateLimitErr)
    // ... use rateLimitErr if non-nil, otherwise construct from APIError
```

This ensures that Anthropic 429 errors (which come as `APIError{429}` after client retries) are also caught by the agent failover logic.

### 3.3 Ensure ProviderManager differentiates error types

**File**: `internal/llm/provider_manager.go`

In the failover loop (line ~211):
- On `RateLimitError`: rotate to next provider immediately (rate limit is provider-specific)
- On `APIError{429}`: same treatment via `IsRateLimitError()`
- On auth errors (401/403): mark provider as unhealthy, rotate
- On server errors (5xx): rotate with backoff
- On client errors (400): don't rotate (likely a request-level issue)

**Files to modify**:
- `internal/llm/anthropic.go` — return `RateLimitError` on 429
- `internal/agent/loop.go` — use `IsRateLimitError()` in failover
- `internal/llm/provider_manager.go` — error-type-aware failover
- Tests for each change

---

## Phase 4: User-Facing Error Messages

**Goal**: Show actionable, human-readable error messages instead of raw JSON.

### 4.1 Add `UserMessage()` to `RateLimitError`

**File**: `internal/llm/errors.go`

```go
func (e *RateLimitError) UserMessage() string {
    parts := []string{"rate limit hit"}
    if e.ModelID != "" {
        parts = append(parts, fmt.Sprintf("on %s", e.ModelID))
    }
    if e.LimitType != "" {
        parts = append(parts, fmt.Sprintf("(%s limit)", e.LimitType))
    }
    if e.RetryAfter > 0 {
        parts = append(parts, fmt.Sprintf("— retrying in %s", e.RetryAfter.Round(time.Second)))
    }
    return strings.Join(parts, " ")
}
```

### 4.2 Add `UserMessage()` to `APIError` and `ClientError`

**File**: `internal/llm/client.go`

```go
func (e *APIError) UserMessage() string {
    switch e.StatusCode {
    case 401: return "authentication failed — check your API key"
    case 403: return "access denied — check your API key permissions"
    case 404: return "model not found — check your model configuration"
    case 429: return "rate limit exceeded — please wait and try again"
    case 500, 502, 503: return "provider is experiencing issues — will retry"
    default:  return fmt.Sprintf("API error (status %d)", e.StatusCode)
    }
}

func (e *ClientError) UserMessage() string {
    return e.Message
}
```

### 4.3 Add `UserMessage()` helper that unwraps any LLM error

**File**: `internal/llm/errors.go`

```go
func UserMessage(err error) string {
    // Try each error type via errors.As, return UserMessage() if available
    // Fall back to err.Error()
}
```

### 4.4 Update TUI to use `UserMessage()`

**File**: `internal/tui/models/chat.go` (line ~971)

Change from:
```go
m.addMessage(RoleSystem, fmt.Sprintf("Error: %v", msg.Err))
```

To:
```go
m.addMessage(RoleSystem, llm.UserMessage(msg.Err))
```

### 4.5 Update CLI to use `UserMessage()`

**File**: `cmd/meept/chat.go` (line ~123)

Change from:
```go
return fmt.Errorf("chat error: %w", err)
```

To:
```go
return fmt.Errorf("%s", llm.UserMessage(err))
```

### 4.6 Add retry progress indicators to TUI

**File**: `internal/tui/models/chat.go`

When a rate limit retry is in progress, show:
```
⏳ rate limit hit on Kimi-K2.6 (tpm_uncached limit) — retrying in 4s (attempt 2/5)...
```

This requires propagating retry status through the RPC layer. The `ChatWithProgress()` method already has `reportProgress()` — extend it to emit rate limit retry events through the bus.

**Files to modify**:
- `internal/llm/errors.go` — `UserMessage()` methods
- `internal/llm/client.go` — `UserMessage()` on `APIError`, `ClientError`
- `internal/tui/models/chat.go` — use `UserMessage()`
- `cmd/meept/chat.go` — use `UserMessage()`
- `internal/agent/loop.go` — emit retry progress events

---

## Phase 5: Error Metrics and Observability

**Goal**: Track rate limit patterns to inform model selection and alerting.

### 5.1 Extend error metrics with rate limit details

**File**: `internal/llm/metrics/store.go`

Add fields to `ErrorRecord`:
- `LimitType string` — what kind of limit was hit
- `RetryAfter time.Duration` — how long we waited
- `RetryAttempts int` — how many retries before success/failure
- `FinalOutcome string` — "success_after_retry", "exhausted_retries", "non_retryable"

### 5.2 Record structured error events

**File**: `internal/llm/client.go`, `internal/agent/loop.go`

After retry loops complete, record:
- Rate limit events with full metadata
- Retry outcomes (success after N retries, exhausted, etc.)

### 5.3 Add rate limit summary to `/api/v1/metrics/live`

**File**: `internal/comm/http/api_handlers.go`

Add fields:
```json
{
  "rate_limits": {
    "last_24h": 15,
    "by_provider": {"openrouter": 12, "anthropic": 3},
    "by_limit_type": {"tpm_uncached": 8, "rpm": 7},
    "avg_retry_time_ms": 3200
  }
}
```

**Files to modify**:
- `internal/llm/metrics/store.go` — extend error records
- `internal/llm/client.go` — record structured events
- `internal/agent/loop.go` — record retry outcomes
- `internal/comm/http/api_handlers.go` — expose in live metrics

---

## Execution Order

| Phase | Priority | Complexity | Impact |
|-------|----------|-----------|--------|
| Phase 1 | Critical | Medium | Foundation — all other phases depend on structured errors |
| Phase 2 | High | Low | Fixes thundering herd, uses provider recommendations |
| Phase 3 | High | Low | Fixes Anthropic 429 bypass, unifies failover |
| Phase 4 | Medium | Medium | User experience — stops showing raw JSON |
| Phase 5 | Low | Low | Observability — helps with capacity planning |

Phases 1-3 should be implemented together as they form the core fix. Phase 4 can follow independently. Phase 5 is additive.

## Testing Strategy

1. **Unit tests** with real error response fixtures from:
   - OpenRouter (429 with nested provider JSON)
   - OpenRouter (non-retryable errors like 401, 402)
   - Anthropic (429 with overloaded type)
   - Generic OpenAI-compatible (429 with Retry-After header)
   - OpenRouter (503 provider timeout)

2. **Integration test** for `chatWithFailover()`:
   - Mock LLM client returning `RateLimitError` with strategy
   - Verify backoff uses provider parameters
   - Verify jitter produces non-deterministic delays
   - Verify model rotation on rate limit

3. **Error display test** for TUI:
   - Verify `UserMessage()` produces clean output for each error type
   - Verify retry progress messages render correctly
