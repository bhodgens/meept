# Context Firewall processMessages Logs at INFO for Wrap-Up Threshold But Not for Actual Compaction Fallback

**Date**: 2026-05-15
**Phase**: 10 (context firewall and pressure management)
**Severity**: low
**Component**: `internal/llm/context_firewall.go` (processMessages)

## Description

The `processMessages` method has inconsistent log levels for context pressure events:

1. **Wrap-up threshold (50-80%)**: logged at `INFO` level -- this is the least severe state
2. **Hard limit drop**: logged at `WARN` level -- appropriate for data loss
3. **Compaction fallback**: logged at `DEBUG` level -- this is a significant event that indicates compaction failed
4. **Proactive compression applied**: logged at `DEBUG` level -- compression activity is important for monitoring

The wrap-up threshold (50%) fires at `INFO` even though it only logs a warning message and takes no action. Meanwhile, compaction failure (which means the system is falling through to less optimal strategies) is logged at `DEBUG`. An operator running at default log levels would see wrap-up warnings but would not see compaction failures or compression events.

Additionally, the proactive compression result (lines 527-536) is logged at `DEBUG` but includes valuable metrics (stage, tokens before/after, dropped count) that would be useful at `INFO` level when compression actually fires.

## Reproduction

1. Start daemon with default log level
2. Build up context to 50% utilization
3. Observe wrap-up threshold fires at INFO
4. Continue to trigger compaction -- if compaction returns without compacting, the fallback is only visible at DEBUG
5. If proactive compression fires, it is only visible at DEBUG

## Evidence

In `context_firewall.go` `processMessages`:
- Line 501: compaction success -> `INFO` (correct)
- Line 515: compaction fallback (failure) -> `DEBUG` (should be `WARN`)
- Line 529: proactive compression applied -> `DEBUG` (should be `INFO`)
- Line 560: wrap-up threshold exceeded -> `INFO` (appropriate)

## Root Cause

Inconsistent log level choices during implementation. The compaction fallback path was treated as a normal control flow rather than a degradation event. Proactive compression was treated as debug detail rather than a significant context management event.

## Proposed Fix

1. Change compaction fallback log level from `DEBUG` to `WARN` (line 515)
2. Change proactive compression log level from `DEBUG` to `INFO` when `cr.Compressed` is true (line 529)

## Applied Fix

- `context_firewall.go` compaction fallback now logs at `WARN` level
- `context_firewall.go` proactive compression log now logs at `INFO` level
- Message text improved to clarify "falling back to compressor"

## Model vs Harness
[X] Harness bug  [ ] Model quality issue  [ ] Both
