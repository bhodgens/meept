# Recall Outcome Telemetry - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. The parent gate tests (00T) ALREADY
> EXIST - your job includes making the F5 assertions GREEN. Do NOT delete,
> skip, weaken, or rewrite any gate test. Do NOT commit. Do NOT use read_file
> on existing source files. Write once and stop.

## Meta

- **Parent:** ../master.md
- **Scope:** Record one JSONL row per injected memory per turn stating whether the final reply cited it, feeding the calibration loop.
- **Dependencies:** 03-stable-volatile-split.md (COMPLETE - telemetry hooks the split assembly)
- **Estimated Context:** 65K
- **Concurrency Group:** C
- **Audit references:** F5 of master Test Gate; R5

## Goal

Nothing measures whether an injected memory helped. This leaf writes
`<memory.data_dir>/recall_outcomes.jsonl` (C5 shape) at turn terminal: for
each injected memory item, was it cited in the final assistant reply? This is
the raw signal for recall-value calibration (the same JSONL pattern as
rejected_candidates/claim_verdicts/raw_responses: 10 MiB cap, one rotated
generation, gitignored, contains user text).

## Context

- The calibration JSONL pattern (size cap + rotation) lives where
  rejected_candidates.jsonl etc. are written (grep calibrationLog /
  rejected_candidates in internal/memory - calibration_log.go).
- Turn-terminal capture already exists: internal/daemon/memory_capture.go
  subscribes to agent.TopicTurnTerminal, gates Status=="completed", reads the
  exchange from the session message store, dedupes by (ConversationID, Reply)
  LRU. Telemetry rides the SAME subscriber pattern - extend the capture
  pipeline or add a sibling subscriber (do NOT re-implement dedup; reuse the
  LRU or share it).
- TurnTerminalEvent does NOT carry the user message; the exchange comes from
  the store (with the GetByConversationID fallback - live-fixed defect class).

## Verified Anchors

- internal/memory/calibration_log.go - JSONL cap/rotation pattern to reuse.
- internal/daemon/memory_capture.go - turn.terminal subscriber, LRU dedup,
  store-read + conversation-id fallback (the wiring point).
- internal/agent/loop.go recall assembly (the site that knows WHICH memory
  ids were injected - the telemetry needs id + injected text; thread it from
  the assembly into a snapshot the terminal path can read).

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// C5 (verbatim): <memory.data_dir>/recall_outcomes.jsonl per line:
{"ts":"RFC3339","session_id":"...","memory_id":"...","query":"...","injected":true,"cited":true,"tokens":123}
// cited = injected item's salient text overlaps the final reply
// (lowercase, >=8 shared word tokens -> cited; conservative: tie goes to
// cited=false). tokens = the item's injected token estimate.
// 10 MiB cap + one .1 rotation; gitignored path; write failures logged,
// never fail the turn.
```

### What This Leaf Consumes

- C5 from ../master.md; the split assembly from 03 (item list + ids).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Outcome writer (JSONL, cap, rotation) | Create: internal/memory/recall_outcome.go (+ wire into data dir resolution) | `go test -p 2 ./internal/memory/ -run TestRecallOutcome` (bug-fix class unit: writer is store plumbing; feature e2e is F5) | writer undefined | rows append; >10MiB rotates to .1; malformed input never panics | Given 11MiB of rows; When appended; Then cap honored, .1 exists, newest intact |
| T2 | Injected-items snapshot from assembly | Modify: internal/agent/loop.go (recall assembly records item ids + text + query onto the turn snapshot) | `go test -p 2 ./internal/agent/ -run TestMemory` | no snapshot | turn snapshot carries injected items (id, text, query, tokens) | Given recall injected 3 items; When turn snapshot freezes; Then 3 entries present |
| T3 | Citation match + terminal write | Modify: internal/daemon/memory_capture.go (or sibling subscriber) | gate suite F5 | no outcomes file | on completed turn with injected items: one row per item, cited per overlap rule, deduped like capture | Given reply restating item text; When terminal fires; Then row cited=true; unrelated reply -> cited=false |
| T4 | Dedup + non-completed turns | Modify: same as T3 | same suite | duplicate rows on dual emissions | relay+direct double terminals produce ONE row set; failed turns produce none | Given duplicate terminal events; When both processed; Then single row set (LRU hit) |

## Self-Verification Checklist

- [ ] Gate F5 assertions GREEN
- [ ] C5 keys verbatim; ts RFC3339; conservative citation tie -> false
- [ ] Write failures never fail the turn (log + continue)
- [ ] Existing capture behavior unchanged (L0 recording still exact)
- [ ] gofmt clean; no scope creep

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched**
2. **Verify command + output** [last 20 lines]
3. **Deviations:** [none / list]

## Review Checklist (For Review Agent)

- [ ] C5 verbatim; rotation correct (cap BEFORE append, one generation)
- [ ] Subscriber pattern respected (no private ticker, no private retry)
- [ ] Dedup reuse (not a second LRU) or a justified shared-LRU extension
- [ ] conversation-id fallback present in the store read
- [ ] No debug artifacts

Output: APPROVED or specific gaps.

## Notes

- The FUTURE consumer (feeding scores back into recall ranking) is OUT OF
  SCOPE - this leaf only produces the signal. Note analysis-tool ideas in the
  report; do not build them.
- User text in rows: the file lives under the memory data dir (gitignored);
  NEVER commit a sample row with real text - fixtures use synthetic text.
