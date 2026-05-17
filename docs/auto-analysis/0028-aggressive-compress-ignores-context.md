# Aggressive Compress Ignores Context Parameter, Skips Compaction

**Date**: 2026-05-15
**Phase**: 10 (context firewall and pressure management)
**Severity**: low
**Component**: `internal/llm/context_compressor.go` (aggressiveCompress)

## Description

The `aggressiveCompress` method at Stage 3 (70% utilization) explicitly ignores its `context.Context` parameter and does not attempt LLM-based compaction or summarization. It immediately falls through to `keepTail(messages, 4)` which is a simple tail-truncation strategy that drops all but the last 4 non-system messages.

This means that at the aggressive compression stage (70-80% utilization), the compressor skips the smarter `ContextCompactor` entirely and goes straight to truncation. The compactor is only invoked at Stage 2 (summarize, 60-70%) via `summarizeOldHistory`. Once utilization crosses 70%, the system jumps to lossy truncation even if compaction could have produced a more information-preserving reduction.

## Reproduction

1. Create a conversation that reaches 70% context utilization
2. The compressor's Stage 3 (`aggressiveCompress`) is triggered
3. `aggressiveCompress` calls `keepTail(messages, 4)` without attempting compaction
4. All messages older than the last 4 are dropped without summarization

## Evidence

`context_compressor.go` lines 441-443:
```go
func (c *ContextCompressor) aggressiveCompress(_ context.Context, messages []ChatMessage) []ChatMessage {
    return keepTail(messages, 4)
}
```

Compare with Stage 2 (`summarizeOldHistory`) at lines 364-381 which tries compactor then summarizer then falls back to keepTail. Stage 3 skips all of that.

The quality metrics system tracks `CriticalDropped` but since critical messages are retained by `keepTail`, this metric will always show 0 for Stage 3 even though significant non-critical context is lost.

## Root Cause

The aggressive compression stage was likely implemented as a simpler, faster fallback. However, it should at least attempt compaction (which is designed for exactly this scenario) before falling back to raw truncation. The `_ context.Context` parameter signature confirms this was a deliberate choice, but it means the compactor's structured summarization (which preserves decisions, file paths, progress) is bypassed at the stage where it would be most valuable.

## Proposed Fix

Change `aggressiveCompress` to attempt compaction first, falling back to `keepTail` only if compaction fails:

## Applied Fix

`aggressiveCompress` in `internal/llm/context_compressor.go` now calls `c.compactor.Compact(ctx, messages)` before falling back to `keepTail(messages, 4)`. This matches the pattern already used in `summarizeOldHistory` (Stage 2), so Stage 3 (70-80% utilization) now benefits from smart contextual summarization instead of jumping straight to truncation.

```go
func (c *ContextCompressor) aggressiveCompress(ctx context.Context, messages []ChatMessage) []ChatMessage {
    if c.compactor != nil {
        result := c.compactor.Compact(ctx, messages)
        if result.Compacted {
            return result.Messages
        }
    }
    return keepTail(messages, 4)
}
```

## Model vs Harness
[X] Harness bug  [ ] Model quality issue  [ ] Both
