# 0041: Classifier Client Uses Unreachable Local LLM, No Fallback Grace

| Field | Value |
|-------|-------|
| Date | 2026-05-16 |
| Phase | 3 |
| Severity | **Medium** |
| Component | `internal/daemon/components.go`, `internal/agent/dispatcher.go` |
| Evaluation Dimension | Robustness |
| Reporter | QA Phase 3 |

## Description

The classifier LLM client is configured to use `local/lfm-code` at `http://127.0.0.1:8080/v1`, but the local LLM server is not running. Every single request logs a classifier failure with "connection refused" and falls back to keyword classification. This adds ~500ms latency to every request and degrades routing accuracy.

Additionally, the classifier model's base URL (127.0.0.1:8080) is unreachable, but the daemon logs `API key not set or not expanded expected_env=GALA_API_KEY` which is misleading - the issue isn't the API key, it's the endpoint being down.

## Reproduction

```bash
# Start daemon with local LLM not running
~/git/meept/bin/meept daemon start
~/git/meept/bin/meept chat "hello"
# Check log - classifier fails every time
```

## Evidence

Every chat request shows:
```
msg="LLM classifier failed, trying keyword" error="request failed: Post \"http://127.0.0.1:8080/v1/chat/completions\": dial tcp 127.0.0.1:8080: connect: connection refused"
```

Startup log shows:
```
msg="Resolved model configuration" component=classifier-llm provider=local model_id=lfm-code base_url=http://127.0.0.1:8080/v1
msg="API key not set or not expanded" component=classifier-llm expected_env=GALA_API_KEY
msg="Classifier LLM client initialized" model=local/lfm-code
```

The classifier also hits 429 rate limits from the z.ai API when it tries to use the main client as fallback:
```
msg="Retryable error" status=429 attempt=1 max_retries=3
msg="Retryable error" status=429 attempt=2 max_retries=3
```

## Root Cause

1. The `small_model` config resolves to `local/lfm-code` which points to 127.0.0.1:8080
2. The local LLM is not running (no llama.cpp or equivalent)
3. No health check at startup to verify the classifier endpoint is reachable
4. No circuit breaker to stop repeated connection attempts

## Impact

- **Medium**: Every request has ~500ms added latency from failed classifier calls
- Keyword fallback is inaccurate (bug #0036)
- Log spam with connection refused errors
- Misleading API key warning

## Proposed Fix

1. Add health check for classifier endpoint at startup with warning
2. Implement circuit breaker: after N consecutive failures, stop trying classifier for a cooldown period
3. When local LLM is unreachable, use the main LLM client as classifier instead of keyword fallback
4. Fix misleading API key warning for local providers that don't need keys

## Classification

- Type: Bug (operational resilience)
- Regression: No
- Priority: P2 - degrades all request routing
