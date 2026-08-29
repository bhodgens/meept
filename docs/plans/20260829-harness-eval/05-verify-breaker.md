# Verify + Circuit Breaker - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks via TDD. Do NOT commit.
> Do NOT use read_file on existing source. After writing, do not read back.

## Meta

- **Parent:** ../master.md
- **Scope:** Verify uses structured tool data only. Circuit-break identical tool retries. Owns loop.go this group.
- **Dependencies:** none
- **Estimated Context:** 55K
- **Concurrency Group:** A

## Goal

The book’s verify/correct pair: do not trust model prose for “did it work,” and stop retry storms.

## Context

`internal/agent/loop.go` is large. Add small helpers in new files and one call site each. Existing cycle detector and loop guards (loop-economics 07) stay; this is a tool-retry breaker beside them, not a duplicate SHA256 cycle detector.

Key files: `internal/agent/loop.go` (tool result handling), `internal/agent/verification_config.go`, `docs/features.md` loop guards section (read via cat, do not rewrite in this leaf).

## Interface Contracts (From Parent)

None from C1–C9. This leaf exposes:

```go
// internal/agent/tool_breaker.go
type ToolRetryBreaker struct {
    WarnAt, VetoAt int // defaults 3, 5
}
// Key = tool name + canonical JSON of args. Identical consecutive failures increment.
// On VetoAt: skip the tool, inject a system note, do not count as success.
func (b *ToolRetryBreaker) Observe(name string, args map[string]any, failed bool) (veto bool)

// Structured verify: checkers may read ToolResult fields / exit codes / oracle JSON.
// They must not parse assistant Content for pass/fail.
```

### What This Leaf Consumes

Existing cycle detector. Do not replace it.

## Tasks

### Task 1: ToolRetryBreaker tests + impl

**Files:** Create `internal/agent/tool_breaker.go`, `tool_breaker_test.go`

Canonicalize args with sorted keys so map order does not break identity.

### Task 2: Wire Observe on tool failure

**Files:** Modify `internal/agent/loop.go` — one Observe call after tool error. If veto, skip re-invoke.

### Task 3: Structured-verify guard test

**Files:** Create `internal/agent/verify_structured_test.go`

A fake verifier that would pass if it read assistant Content “tests passed” but the tool JSON says exit 1 must FAIL. Hook the existing verification path or a narrow helper `verifyFromToolResults(results []ToolResult) bool`.

## Self-Verification Checklist

- [ ] -race tests for breaker + verify helper
- [ ] loop.go diff is small
- [ ] No duplicate cycle detector
- [ ] Do NOT commit

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** none

## Review Checklist (For Review Agent)

- [ ] Args canonicalization is deterministic
- [ ] Veto injects a system note, not a user bubble
- [ ] Verify helper ignores assistant prose
