# Plan: Agent Model Backoffs for Rate Limit Exhaustion

## Context

Implement robust model backoff and recovery mechanisms for the agent system to handle rate limit exhaustions gracefully. The system should cycle to the next model when rate limited, and if no other models are available in an alias group, apply exponential backoff until the issue resolves. Service level issues should never result in permanent job failure, and completed work should always be preserved.

## Current State Analysis

### Existing Infrastructure (Can Reuse)

1. **Model Alias Resolution** (`internal/llm/resolver.go`):
   - Already has `AliasHealth` tracking with `ConsecutiveFails` and `CooldownUntil`
   - `RecordAliasFailure()` already implements exponential backoff
   - `ResolveForAlias()` rotates to next model when in cooldown

2. **LLM Client Retry** (`internal/llm/client.go`):
   - Already has retry logic for 429/5xx status codes
   - Linear backoff (2s, 4s, 6s) - not truly exponential
   - Returns `APIError` for HTTP errors

3. **Job Queue** (`internal/queue/`):
   - Already has `RetryCount`, `MaxRetries`, `NextRetryAt`
   - `Retry()` method already implements exponential backoff (2s, 4s, 8s capped at 8s)
   - `CanRetry()` checks if retries remaining

4. **Task Step Persistence** (`internal/task/step.go`):
   - Step states already persist in SQLite
   - `StepCompleted` preserved across retries

### Identified Gaps

1. **No RateLimitError type**: HTTP 429 returns generic `APIError`, cannot distinguish at higher layers
2. **No model failover in agent loop**: `reasoningCycle()` returns immediately on error after recording alias failure
3. **No job retry on rate limit**: `OnJobFailed()` marks step permanently failed regardless of error type
4. **No exponential backoff in client**: Linear backoff calculation is misleading

## Implementation Plan

### Layer A: Error Classification

**File**: `internal/llm/errors.go` (new file)

Create `RateLimitError` type:
- Struct with `ProviderID`, `ModelID`, `RetryAfter`, `Cause`
- `Error()` method returning formatted message
- `IsRateLimitError(err error) bool` helper checking for RateLimitError or APIError with StatusCode 429

### Layer B: LLM Client - HTTP 429 Detection

**File**: `internal/llm/client.go`

Modify `doRequest()` around line 443:
- Detect HTTP 429 specifically (currently treated same as other retryable codes)
- Return `RateLimitError` with provider/model info and parsed Retry-After header
- Add `parseRetryAfter(header string) time.Duration` helper

### Layer C: Resolver - Enhanced Alias Rotation

**File**: `internal/llm/resolver.go`

Add methods:
- `HasHealthyModels(aliasName string) bool` - Check if alias has models not in cooldown
- `RotateToNextModel(aliasName string) (*ModelConfig, error)` - Force rotation to next model, reset counters

### Layer D: Agent Loop - Model Failover with Backoff

**File**: `internal/agent/loop.go`

Add `chatWithFailover(ctx, messages, opts)` method:
- Wrap `l.llm.Chat()` call
- On success: record alias success, return response
- On rate limit error with alias:
  - If attempts  - If attempts < 3: rotate model via `RotateToNextModel()`, retry immediately
  - If attempts >= 3: apply exponential backoff (2s, 4s, 8s... up to 30s), reset attempts, continue
- On non-rate-limit error: fail immediately

Modify `reasoningCycle()` line 888:
- Replace `l.llm.Chat()` with `l.chatWithFailover()`

### Layer E: Tactical Scheduler - Job Retry on Rate Limit

**File**: `internal/agent/tactical.go`

Modify `OnJobFailed()`:
- Add rate limit detection at start of function
- If rate limit detected:
  - Get job from queue
  - Check `job.CanRetry()`
  - Call `queue.Retry(ctx, jobID)` which sets `NextRetryAt` with backoff
  - Reset step state to `StepScheduled`
  - Return early to prevent permanent failure

Add `isRateLimitError(errMsg string) bool` helper:
- Check for: "rate limit", "429", "too many requests", "quota exceeded"

## Files to Modify

| File | Change |
|------|--------|
| `internal/llm/errors.go` | New file: RateLimitError type, IsRateLimitError helper |
| `internal/llm/client.go` | Return RateLimitError for 429, add parseRetryAfter |
| `internal/llm/resolver.go` | Add HasHealthyModels, RotateToNextModel methods |
| `internal/agent/loop.go` | Add chatWithFailover, modify reasoningCycle |
| `internal/agent/tactical.go` | Modify OnJobFailed, add isRateLimitError |

## Key Behaviors

1. **Single model in alias**: Rate limit -> backoff (2s, 4s, 8s... 30s max) -> retry same model
2. **Multiple models in alias**: Rate limit -> rotate to next model -> retry immediately -> repeat until exhausted -> job-level backoff
3. **All models exhausted**: Re-queue job with backoff (2s -> 4s -> 8s... 5min max) -> preserve completed work
4. **Non-rate-limit errors**: Fail immediately without retry
5. **Max retries exceeded**: Move to dead letter queue for manual intervention

## Guarantees

- **No lost work**: Steps with `StepCompleted` state preserved through retries
- **Auto-recovery**: System automatically recovers from rate limits
- **Dead letter for true failures**: Only non-recoverable errors (bugs, invalid requests) end up there
- **Thundering herd prevention**: Exponential backoff prevents simultaneous retries

## Verification Plan

1. **Unit Tests**:
   - Test `RateLimitError` classification
   - Test `IsRateLimitError()` helper
   - Test `chatWithFailover()` model rotation logic
   - Test `OnJobFailed()` rate limit detection

2. **Integration Test**:
   - Mock LLM returning 429 on first call, success on second
   - Verify model rotation happens
   - Verify job completes after backoff

3. **Manual Testing**:
   - Configure alias with multiple models
   - Trigger rate limit exhaustion
   - Observe logs for "Rate limited, rotating model" and "Rate limit error, requeuing job with backoff"
   - Verify recovery after backoff expires

## Implementation Status

**COMPLETED** on 2026-04-05

All layers A-E have been implemented:
1. ✅ Error classification with RateLimitError type
2. ✅ LLM client HTTP 429 detection and Retry-After parsing
3. ✅ Resolver with enhanced alias rotation methods
4. ✅ Agent loop chatWithFailover with model rotation and backoff
5. ✅ Tactical scheduler job retry on rate limit

The system now provides robust rate limit handling with automatic model failover, exponential backoff, and job-level recovery mechanisms.
