# Local LLM Classifier Unavailable - Every Request Logs Error
**Date**: 2026-05-15
**Phase**: 1
**Severity**: low
**Component**: internal/agent/dispatcher.go
**Evaluation Dimension**: robustness, efficiency

## Description
The dispatcher's LLM classifier is configured to use a local model at `http://127.0.0.1:8080/v1/chat/completions` (LFM2.5-1.2B-Code), but the local server is not running. Every chat request triggers a connection attempt that fails, logging a WARN message and adding latency before falling back to keyword classification.

## Reproduction
```bash
# Ensure no server at 127.0.0.1:8080
~/git/meept/bin/meept chat "hello"
# Check daemon logs:
# WARN msg="LLM classifier failed, trying keyword" component=dispatcher error="request failed: Post \"http://127.0.0.1:8080/v1/chat/completions\": dial tcp 127.0.0.1:8080: connect: connection refused"
```

## Evidence
From daemon logs:
```
time=2026-05-16T00:19:57.170-06:00 level=INFO msg="Classifier LLM client initialized" model=local/lfm-code
time=2026-05-16T00:19:57.170-06:00 level=WARN msg="API key not set or not expanded" component=classifier-llm expected_env=GALA_API_KEY hint="Set GALA_API_KEY environment variable"
```

At request time:
```
time=2026-05-16T00:23:28.583-06:00 level=WARN msg="LLM classifier failed, trying keyword" component=dispatcher error="request failed: Post \"http://127.0.0.1:8080/v1/chat/completions\": dial tcp 127.0.0.1:8080: connect: connection refused"
```

## Root Cause
The classifier is configured to use `local/lfm-code` at `http://127.0.0.1:8080/v1` but the llama.cpp server is not running. There is no health check or startup validation to detect this condition.

## Impact on Platform Quality
- Every request has added latency from the failed connection attempt
- Log noise from repeated WARN messages
- Classification quality degrades (keyword classifier is less accurate than LLM)
- The classifier config was correct at some point but no validation occurs

## Proposed Fix
1. Add a startup health check for the classifier LLM endpoint
2. Cache the "unavailable" status and skip connection attempts for a cooldown period (e.g., 60 seconds)
3. Log a single WARN at startup rather than per-request
4. Allow disabling the LLM classifier via config if no local model is available

## Applied Fix

- `LLMClassifier` now tracks `unavailable` state with an `atomicBool` + mutex-guarded `unavailUntil` timestamp
- On every `Classify` or `ClassifyMulti` call, the classifier checks cooldown before attempting the LLM connection
- When a call fails, `unavailable` is set and `unavailUntil` is set to `now() + 60s` (default cooldown)
- When a call succeeds, `unavailable` is cleared
- Log level for classifier failure changed from DEBUG to WARN with retry-after hint
- `Classify` and `ClassifyMulti` both share the same cooldown mechanism
- `MarkUnavailable()` and `UnmarkUnavailable()` exported methods allow external health-check integration

## Classification
[ ] Harness bug  [ ] Model quality issue  [ ] Communication issue  [x] Efficiency issue  [ ] Design gap  [ ] Both
