# Context Firewall Summarization Silently Continues on Failure, Allowing Unbounded Context Growth

**Date**: 2026-05-15
**Phase**: 10 (context firewall and pressure management)
**Severity**: medium
**Component**: `internal/llm/context_firewall.go` (processMessages, summarizeOldHistory)

## Description

When the summarization stage in `processMessages` fails, the firewall logs a warning and continues with the unsummarized context. The code comment on line 596 explicitly acknowledges this as a "deliberate scope limitation." However, this means that if summarization consistently fails (e.g., because the summary model is down, misconfigured, or also hitting budget limits), the context will continue growing unchecked until it hits the hard limit.

At the hard limit (80%), the system drops old context entirely, losing all conversation history except system + last 2 messages. This is much more destructive than summarization failure. The gap between "summarization failed" and "hard limit drop" is bridged only by the hope that the next request won't push further over the limit.

In the observed test, the summarization model (`local/lfm-code`) had a missing API key warning (`GALA_API_KEY not set`) and a very small context limit (8192 tokens). The summarizer LLM is likely failing silently, causing the firewall to never actually summarize, and all context reduction falls to the hard-limit dropper.

## Reproduction

1. Configure the daemon with a summarizer model that is unavailable (wrong API key, model down, etc.)
2. Build up conversation context gradually
3. Observe that `summarizationFailures` counter increments but context continues to grow
4. Eventually the hard limit is hit and all context is dropped (keeping only system + last 2)
5. The operator has no visibility into this degradation until context is lost

## Evidence

`context_firewall.go` lines 596-613:
```go
if f.config.SummarizeHistory && currentTokens > int(float64(f.model.ContextLimit)*f.config.HardLimit) {
    summarized, err := f.summarizeOldHistory(ctx, result)
    if err != nil {
        f.summarizationFailures.Add(1)
        f.logger.Warn("summarization failed, continuing without summarization",
            "error", err,
            "failures_total", f.summarizationFailures.Load(),
        )
        // Continue without summarization
    } else {
        result = summarized
        ...
    }
}
```

Daemon logs show the summarizer model configuration:
```
model_name=LFM2.5-1.2B-Instruct.Q8_0 context_limit=8192 max_output=2048
API key not set or not expanded expected_env=GALA_API_KEY
```

The summarizer has a 8192 context limit and no API key -- it is likely failing on every call, but there's no escalation mechanism.

## Root Cause

Two issues compound:

1. **Summarization failure is non-fatal by design**: The firewall prefers to continue rather than block, but this means persistent failures silently accumulate.

2. **No fallback mechanism**: When LLM summarization fails, there is no fallback to a simpler strategy (e.g., extractive summarization, keyword extraction, or even keeping a larger tail). The only other option is the hard-limit dropper.

3. **No health check or alerting**: The `summarizationFailures` counter exists but is not exposed through any monitoring interface (see bug 0026). An operator cannot see that summarization is consistently failing.

## Proposed Fix

1. Add a circuit breaker for summarization failures: after N consecutive failures (e.g., 3), switch to a fallback strategy like `keepTail(messages, 8)` instead of continuing with no reduction.

2. Elevate the log level after consecutive failures (e.g., first failure = WARN, third+ failure = ERROR).

3. Expose summarization failure count in status/RPC (see bug 0026).

4. Consider adding a health check for the summarizer model during daemon startup.

## Model vs Harness
[X] Harness bug  [ ] Model quality issue  [ ] Both
