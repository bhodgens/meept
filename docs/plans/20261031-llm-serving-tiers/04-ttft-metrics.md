# TTFT Instrumentation: First-Delta Timestamps + llm_calls.ttft_ms Column - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit - the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files - explore with search_files or terminal cat. After writing a
> file, do NOT read it back - write once and stop. Report what you built,
> files touched, and any deviations.

## Meta

- **Parent:** master.md
- **Scope:** Time-to-first-token instrumentation: first-delta timestamps in streaming clients, a ttft_ms column in llm_calls via the columnMissingGuard migration pattern, surfaced in metrics.
- **Dependencies:** R0-research.md (findings doc Section 3: TTFT conventions - READ FIRST)
- **Estimated Context:** 70K
- **Concurrency Group:** B
- **Audit references:** proposal 7 (TTFT instrumentation)

## Goal

TTFT is measured nowhere in the codebase today; llm_calls.latency_ms is
full-call duration only. This leaf timestamps the first content delta in
the streaming clients, records milliseconds from request start, persists
it as llm_calls.ttft_ms (INTEGER, -1 when not measured), and surfaces it in
the metrics read path. Non-streaming calls, cache hits, and errors record
-1. The migration follows the established columnMissingGuard pattern so
pre-registry databases gain the column on open.

## Context

- Streaming clients: internal/llm/client.go (OpenAI-compatible) and
  internal/llm/anthropic.go. Both already record per-call token usage into
  llm_calls (tokens_cached / cache_creation_tokens populate at
  client.go:2006 and anthropic.go:308) - follow that exact plumbing path
  for the new field.
- Metrics store: internal/metrics/store.go - llm_calls schema lives in its
  CREATE script; migrations are versioned with Guard functions
  (columnMissingGuard); llm_calls.latency_ms is written at store.go:260.
- R0 findings Section 3 pins the definition: first CONTENT delta after
  request start (excluding queue/serialize time before the HTTP request
  is sent), wall clock, milliseconds, -1 sentinel for not-measured.

## Verified Anchors

- internal/metrics/store.go:434-442 - columnMissingGuard pattern
  (llm_calls.session_id migration, Version 2; reasoning/cache token
  columns, Version 3) - the pattern to extend with the next version.
- internal/metrics/store.go:260 - latency_ms write - neighbor for the
  ttft_ms write.
- internal/llm/client.go:2006 - usage/token recording into the call record
  (OpenAI path) - the plumbing to extend.
- internal/llm/anthropic.go:308 - same, Anthropic path.
- grep ttft / first_token == zero hits repo-wide - nothing exists yet.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/llm (call-record struct shared by both clients) - Contract C6
TTFTMS int64 // ms from request start to first content delta; -1 = not
             // measured (non-streaming, cache hits, errors)

// internal/metrics/store.go migration (version = current max + 1)
ALTER TABLE llm_calls ADD COLUMN ttft_ms INTEGER NOT NULL DEFAULT -1
Guard: columnMissingGuard("llm_calls", "ttft_ms")
```

### What This Leaf Consumes

- None from sibling implementation leaves (05 consumes this leaf's contracts).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Migration: next-version llm_calls.ttft_ms migration using columnMissingGuard("llm_calls","ttft_ms"), column in the fresh-DB CREATE script too; guard test proves both paths (fresh DB and pre-column DB) end with the column present | Modify: internal/metrics/store.go; Test: internal/metrics/store_ttft_test.go | `go test -p 2 ./internal/metrics/ -run TestTTFT` | compile error: ttft_ms undefined in tests | migration tests green | Given a DB created before this version; When the store opens; Then llm_calls has ttft_ms INTEGER NOT NULL DEFAULT -1; and given a FRESH DB; When created; Then the CREATE script already includes ttft_ms and the guard is a no-op |
| T2 | First-delta capture in the OpenAI-compatible streaming client: timestamp the first non-empty content delta; compute TTFTMS at call completion (request start -> first delta); -1 for non-streaming, cache hits, and errors; thread through the call record | Modify: internal/llm/client.go, internal/llm/ (call-record struct); Test: internal/llm/ttft_test.go | `go test -p 2 ./internal/llm/ -run TestTTFT` | compile error: TTFTMS undefined | capture tests green | Given a streaming stub emitting 3 deltas; When the call completes; Then TTFTMS >= 0 and reflects first-delta timing; Given a non-streaming call / a cache hit / an errored call; Then TTFTMS == -1 in all three cases |
| T3 | Same capture in the Anthropic streaming client + store-write: persist TTFTMS into llm_calls alongside latency_ms (store.go:260 neighborhood) and surface ttft_ms in the metrics read path (whatever query/API serves latency_ms today gains ttft_ms symmetrically) | Modify: internal/llm/anthropic.go, internal/metrics/store.go; Test: internal/llm/ttft_test.go, internal/metrics/store_ttft_test.go | `go test -p 2 ./internal/llm/ ./internal/metrics/ -run TestTTFT` | anthropic path not captured / row write absent | capture + persistence tests green | Given an Anthropic streaming stub; When the call completes; Then TTFTMS is captured identically; and given a recorded call; When the store row lands; Then ttft_ms matches the call record and the metrics read path returns it |
| T4 | STRETCH (skip if leaf exceeds ~300 LOC or budget): parse llama-server logs for n_past/cache-hit hints; skip unless trivially clean | optional | n/a | n/a | n/a | Given the stretch goal; When assessed; Then implement ONLY if it stays under budget and is explicitly reported as done or skipped |

**Worked example (T1 shape):**

1. Write the failing guard test: open a store over a DB built from the
   PREVIOUS schema version; assert the ttft_ms column exists after open.
2. Verify RED: `go test -p 2 ./internal/metrics/ -run TestTTFT`
   Expected: FAIL - migration absent / column missing.
3. Implement: append the next-version migration (mirror the session_id
   migration block exactly, including the comment style); add ttft_ms to
   the fresh-DB CREATE script.
4. Verify PASS: `go test -p 2 ./internal/metrics/` all green.

## Self-Verification Checklist

- [ ] All tasks implemented and tests passing (`go test -p 2` on touched packages)
- [ ] Interface contract satisfied exactly (TTFTMS int64, -1 sentinel semantics)
- [ ] All files at exact specified paths
- [ ] Migration uses columnMissingGuard and covers fresh + pre-existing DBs
- [ ] -1 recorded for non-streaming, cache hits, and errors (all three tested)
- [ ] Clock starts at request dispatch, NOT at queue entry (R0 Section 3)
- [ ] No mutex held across the HTTP read loop; timestamp capture is a plain
      field assignment on first delta
- [ ] gofmt clean; ASCII only in every touched file
- [ ] No scope creep; no deviations (or deviations documented)
- [ ] T4 stretch: explicitly reported as done or skipped

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [verify commands; last 20 lines of output]
3. **Deviations:** [none / list with rationale]
4. **Stretch T4:** done / skipped (reason)

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; tests passing
- [ ] Contract C6 matches exactly (field name, type, sentinel)
- [ ] Migration guard pattern matches store.go:434-442 exactly; fresh-DB
      CREATE includes the column
- [ ] Both streaming clients capture first content delta; cache-hit path
      records -1 and does NOT distort the timing
- [ ] Store write sits beside latency_ms; read path surfaced symmetrically
- [ ] No bugs, no security issues, no debug artifacts, no line-number
      corruption, ASCII only

Output: APPROVED or specific gaps with file + line references.

## Notes

- Do NOT touch the cache-miss site or token cache (leaf 02 owns it) and do
  NOT touch the alias-rotation loop (leaf 01 owns it). If a change there
  seems needed, stop and report.
- Cache-hit TTFT: a cache hit skips the model entirely - recording -1 is
  the pinned semantic; do not try to measure "time to cache response".
- Wall-clock (time.Since from dispatch) is the pinned clock; do not
  introduce monotonic plumbing beyond what time.Since already gives.
