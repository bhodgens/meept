# Direct-call wait decorator - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** The `Resolver` exposes a direct-model path (non-alias) that bypasses alias rotation — today a quota error would fail hard. Option A: the path sleeps until the quota window lifts (bounded) and retries the request exactly once. Caller context cancellation always wins.
- **Dependencies:** 01 (QuotaResetError), 02 (config)
- **Estimated Context:** 50K
- **Concurrency Group:** B

## Goal

Code that grabs a Chatter directly (`broker.ChatterForModel(ref)`) bypasses
broker rotation — today a quota error would fail hard. Option A: the
decorator sleeps until the quota window lifts (bounded) and retries the
request exactly once. Caller context cancellation always wins.

## Context

`internal/llm/resolver.go` has direct-model resolution paths (e.g. `ResolveRef`,
  `DefaultModel`) that bypass alias rotation; code hitting these directly today
  gets a hard quota-failure. The decorator must live at the Resolver layer so
  all call paths (alias + direct) respect the same block map — no separate
  decorator file needed if the logic is shared via a single helper.

Key files to understand before implementing:

- `internal/llm/resolver.go` — ResolveRef + any direct-model paths (Search
  for callers that bypass ResolveForAlias; e.g. DefaultModel, a prospective
  ResolveForModel). Wrap these paths in quota-aware retry.
- `internal/llm/resolver.go` — the block state added by leaf 03 (entryBlocks,
  credentialBlocks on AliasHealth); read-only from here.
- `internal/llm/errors.go` — QuotaResetError fields (ResetAt, RetryAfter,
  MaxWait).
- Config from leaf 02 via Resolver config (leaf 03 adds the field; this
  leaf reads the same field).

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// File: internal/llm/resolver.go (or resolver_direct.go, package llm)

// resolveWithQuotaWait wraps any direct-model resolution path (ResolveRef,
// DefaultModel, etc.): on QuotaResetError, if the computed wait <= MaxWait
// AND the caller ctx has no earlier deadline, sleep ctx-aware until the wait
// elapses, then retry ONCE. Otherwise return the QuotaResetError immediately
// (annotated with unblock time). Config-gated by llm.quota_retry.enabled.
// No retry loop — exactly one wait+retry. Shared with alias paths via
// Resolver's existing block map.
```

### What This Leaf Consumes

```
// From 01: QuotaResetError, AsQuotaResetError
// From 02/03: QuotaRetryConfig on Resolver
```

## Tasks

### Task 1: Decorator type + wait policy

**Objective:** Implement quotaWaitChatter with the exact wait policy.

**Files:**
- Modify: `internal/llm/resolver.go` (direct-model wrappers)
- Test: `internal/llm/resolver_direct_test.go`

**Step 1: Write failing test**

Fake inner chatter returning QuotaResetError then success. Table-driven:
(a) waits-then-retries: second call succeeds, elapsed >= (short) wait —
use tiny waits (5-20ms) in tests; (b) ctx cancelled during wait -> error
returned promptly, inner called exactly once; (c) caller deadline < wait ->
immediate error, one inner call; (d) wait > MaxWait -> immediate error;
(e) ResetAt in the past -> immediate error; (f) non-quota error -> returned
immediately, no wait; (g) success first call -> zero overhead (no timer).

**Step 2: Run test to verify failure**

Run: `go test ./internal/llm/ -run TestQuotaWaitChatter -v`
Expected: FAIL

**Step 3: Write minimal implementation**

Per contract. Use `time.Timer` + `select`. Never busy-wait.

**Step 4: Run test to verify pass**

Run: `go test ./internal/llm/ -run TestQuotaWaitChatter -v`
Expected: PASS

### Task 2: Wire into ChatterForModel

**Objective:** Direct lookups get the decorator when enabled.

**Files:**
- Modify: `internal/llm/resolver.go` (direct-model wrappers)
- Test: same test file

**Step 1: Write failing test**

(a) With quota enabled: ChatterForModel returns non-nil; a quota error from
the underlying entry triggers wait+retry once. (b) Disabled: raw chatter
behavior (immediate error). (c) Unknown model ref -> nil (unchanged).

**Step 2: Run test to verify failure**

Run: `go test ./internal/llm/ -run TestChatterForModelQuota -v`
Expected: FAIL

**Step 3: Write minimal implementation**

Wrap at return; keep nil-return for unknown refs.

**Step 4: Run test to verify pass**

Run: `go test ./internal/llm/ -run 'TestQuotaWaitChatter|TestChatterForModelQuota' -count=1 -v`
Expected: PASS
Also: `go test -race ./internal/llm/ -run 'TestQuotaWait|TestChatterForModelQuota' -count=1`

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts (above) satisfied exactly
- [ ] All files at exact specified paths
- [ ] No deviations from spec (or deviations documented below)
- [ ] No scope creep — only what the tasks specify
- [ ] Exactly one wait + one retry, ever. No retry loops.
- [ ] ctx cancellation always wins over the timer

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

The review agent will verify against this leaf document:

- [ ] Every Task above is implemented
- [ ] Every test in the task is present and passing
- [ ] Interface contracts match exactly
- [ ] Both Chatter methods decorated (Chat AND ChatWithProgress)
- [ ] No busy-wait; timer+select only
- [ ] Disabled path is byte-equivalent to old behavior

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- **Production wiring deviation (documented in master.md):** the decorator
  is wired only via `ModelBroker.ChatterForModel`, and the broker is dead
  in production. Production quota-wait instead happens through turn
  parking (`QuotaResumeWatcher`, leaf 06) plus the agent-loop quota
  branch. The decorator and its tests remain for the broker/test path.
- The decorator intentionally does NOT consult the broker's block maps —
  direct calls are per-caller, wait happens in the caller's goroutine and
  context. The broker path (leaf 03) is the zero-wait rotation path; this
  leaf is the wait-in-place path. Both consume the same error type.
- If ChatWithProgress's progress callback contract makes forwarding awkward,
  document the deviation rather than inventing new progress stages.
