<!-- C7 bounded cross-session reference. Sourced from dsh context/session-reference
     (config.ts, index.ts, projection.ts, spill.ts, uri.ts) and audit §III. Plan author only. -->
# Cross-Session Bounded Reference — Implementation Leaf (03)

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. The gate tests ALREADY EXIST (00T-tests
> G4, G6) and are committed — make them pass; do NOT delete, skip, weaken, or
> rewrite any gate test. Do NOT commit. Do NOT use read_file on existing source
> — explore with search_files or terminal cat. After writing a file, do NOT
> read it back — write once and stop. Report deviations.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** A standalone cross-session bounded reference: parse a mention,
  project the referenced session's user/assistant text only, bound it to
  max(64KiB, floor(window*4*0.2)) bytes per source, drop-oldest then head-tail
  truncate-longest with an omitted-bytes notice, and on truncation spill the
  full transcript to `$MEEPT_HOME/spill` + return a locator (untrusted
  framing).
- **Dependencies:** 02-session-projection (S2 — reads the projected
  user/assistant surface for the source snapshot), 01 (S1 — version-safe read).
- **Estimated Context:** 60K
- **Concurrency Group:** C (after 02 REVIEWED, parallel with 04).
- **Audit references:** token-management audit §III session-reference (index
  :55, :387; config.ts:4,8; projection.ts:94-180; spill.ts:8-29,52-60; uri.ts).

## Goal

dsh `session-reference` injects a byte-bounded snapshot of another session:
`MAX_REFERENCES = 3` (config.ts:4); per-source budget = `max(DEFAULT_MAX_REFERENCE_BYTES,
floor(contextWindow * 4 * fraction))` with `referenceContextFraction = 0.2`
(index.ts:55, 387; config.ts:8) — 4 bytes/token sizing, NOT token counting;
projection keeps user/assistant text ONLY and excludes tools/reasoning/system
(projection.ts:38-64); byte-fit drops oldest NON-checkpoint messages then
head-tail truncates the longest with the omitted-byte notice
(projection.ts:94-146, `truncateWithNotice` :152-180); on truncation the full
transcript is spilled read-only with `REFERENCE_WARNING`
(spill.ts:23-29,8-10) and the omission notice carries `{omittedBytes,
omittedMessages, fullSnapshot:{status,locator}}` (spill.ts:52-60). meept has NO
cross-session recall (audit §III GAP: FULL). This leaf brings the resolver;
the injection point (master C7 "injects a bounded snapshot") is the branch
Open Question #1 — the resolver ships + is gate-tested standalone, and the
integration step wires it into the agent's turn-preparation (see T6).

## Context

- `internal/context/sessionref/` (created) — new package under `internal/`.
- `internal/llm/spill.go` — `SpillService` EXISTS: `Spill(ctx, sessionID,
  content) (string, error)` writes `<home>/spill/<sanitizedSessionID>/<sha16>.txt`
  mode 0600 and returns a re-read pointer line (gray lines 98-113);
  `sanitizeSpillSessionID` (line 33-43) is the path-safe encoder to reuse. This
  is the spill backend C7 uses — do NOT build a second spill writer; the leaf
  renders its own reference-transcript file via `SpillService.Spill` (or returns
  the pointer + locator).
- `internal/session/projection.go` (leaf 02) — `Snapshot(sessionID, keys...)`
  over the `session-summary` + user/assistant text surface. The resolver reads
  the referenced session's projected surface for `Prepare`.
- `/tmp/deepseek-harness/packages/context/session-reference/` — uri.ts
  (mention grammar `@[label](dsh-session:base64url)`), config.ts, index.ts
  (Prepare/budget/listCandidates), projection.ts, spill.ts — the faithful
  port surface. `meept-session:` replaces `dsh-session:` as the scheme.
- `internal/agent` turn-preparation — the intended injection seam (see T6).
  The exact line is the Open Question #1; the implementer reads the turn
  prep code and places the resolver call where the user message is normalized,
  appending the reference context immediately after the citing message.

## Verified Anchors

- `/tmp/deepseek-harness/packages/context/session-reference/src/config.ts:4,8`
  — `MAX_REFERENCES = 3`, `DEFAULT_MAX_REFERENCE_BYTES = 65_536`.
- `/tmp/deepseek-harness/packages/context/session-reference/src/index.ts:55,387`
  — default fraction 0.2; `max(65536, floor(ctx*window*4*fraction))`.
- `/tmp/deepseek-harness/packages/context/session-reference/src/projection.ts:38-64,94-180`
  — user/assistant-only projection; drop-oldest; `truncateWithNotice`.
- `/tmp/deepseek-harness/packages/context/session-reference/src/spill.ts:8-10,23-29,52-60`
  — `REFERENCE_WARNING`, spill-only-on-truncation, omission `fullSnapshot`.
- `/tmp/deepseek-harness/packages/context/session-reference/src/uri.ts:69-87`
  — mention grammar + canonical URI.
- `/Users/caimlas/git/meept/internal/llm/spill.go:98-113` — `SpillService.Spill`
  reuse.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/context/sessionref/
const MAX_REFERENCES = 3
const DefaultMaxReferenceBytes = 65_536
const ReferenceContextFraction = 0.2
const SourceKind = "session-reference"

func ParseMentionText(text string) (clean string, refs []SessionReferenceInput, err error)
func NewSessionReferenceResolver(proj SessionProjectionSurface, spill *llm.SpillService) *SessionReferenceResolver
func (r *SessionReferenceResolver) Prepare(ctx context.Context, ownerID string, refs []SessionReferenceInput, contextWindowTokens int64) (*PreparedReference, error)
func (r *SessionReferenceResolver) ListCandidates(ctx context.Context, ownerID, query string, limit int) ([]SessionReferenceCandidate, error)
```

### What This Leaf Consumes

- S2 from 02 — the projected surface read (user/assistant text) for the
  source snapshot.
- S3 from parent — signatures + frozen constants once in orchestrator.md.
- master C7 prose — the budget/truncate/spill contract.
- gate tests 00T-tests G4, G6.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Mention grammar parser with canonical `meept-session:` URIs | Create `internal/context/sessionref/uri.go` | `go test -p 2 ./internal/context/sessionref -v` | FAIL: `ParseMentionText` undefined | PASS: `@[lbl](meept-session:...)` and bare `meept-session:` parse to refs; a malformed explicit mention errors; self-reference is rejected | Given a text with a canonical mention; When parsed; Then refs parse, the mention is normalized, self-references error |
| T2 | Resolver core: projection read + per-source budget (max(64KiB, floor(window*4*0.2))) + user/assistant-only projection + drop-oldest + head-tail truncate + spill-on-truncation + notices (all in ONE resolver.go to hold the 3-file leaf budget) | Create `internal/context/sessionref/resolver.go` | `go test -p 2 ./internal/context/sessionref -v` | FAIL: `NewSessionReferenceResolver`/`Prepare` undefined | PASS: each source is bounded per-source (bytes) + refs capped at MAX_REFERENCES (>3 errors); projection excludes tools/system/developer; drop-oldest then head-tail truncate regards exact omissions; truncated previews spill via `SpillService.Spill` with `{omittedBytes, omittedMessages, fullSnapshot:{status,locator}}` + untrusted `REFERENCE_WARNING`; intact previews do NOT spill; save-failure reports `unavailable` | Given an over-budget second session; When `Prepare` runs; Then it is bounded, tools absent, truncated (or intact) per budget, and a truncation carries a spill locator instead of a fabricated one |
| T3 | Budget + truncate + spill constants and helpers (byte math, omit notices) | Modify `internal/context/sessionref/resolver.go` (same file — budget `int64`, `truncateWithNotice`, omission structs) | `go test -p 2 ./internal/context/sessionref -v` | FAIL: truncate/omission symbols undefined | PASS: any over-budget text is head-tail truncated with an exact omitted-bytes count and the append `\n[… omitted N UTF-8 bytes …]` stays within budget | Given an over-budget string; When truncated; Then head+tail survive, omitted bytes are exact, and the notice fits the budget |
| T4 | Spill of full transcript on truncation only | Modify `internal/context/sessionref/resolver.go` (spill branch, reuses `llm.SpillService.Spill`) | `go test -p 2 ./internal/context/sessionref -v` | FAIL: spill branch / locator field missing | PASS: `if !source.stats.truncated return` (no spill on intact); on truncation the full transcript is spilled + locator returned; a save error reports `unavailable`, never a fabricated locator | Given a truncated vs intact preview; When prepared; Then only the truncated one spills and returns a locator |
| T5 | `ListCandidates` ranked by same-cwd affinity (labels from projected title) | Modify `internal/context/sessionref/resolver.go` | `go test -p 2 ./internal/context/sessionref -v` | FAIL: `ListCandidates` undefined | PASS: candidates sorted same-cwd first (rank 0 same-cwd, 1 no-cwd, 2 different), self excluded, label from projected title or the session id | Given a candidate set; When listed; Then same-cwd sorts first and self is excluded |
| T6 | Integration: wire resolver into agent turn-preparation (append snapshot after the citing message) | Modify ONE file in `internal/agent/` (the turn-preparation seam, Open Question #1 — read turn-prep code and place the resolver call) | `go test -p 2 ./internal/context/sessionref ./internal/agent -v` | FAIL: resolver not invoked in the turn path / compile break | PASS: a user message containing a mention is parsed, prepared with the routed model's context window, and the reference context is injected immediately after the citing message | Given a turn with a mention; When prepared; Then the mention is replaced and the snapshot context is appended after the citing message |

T1-T5 must satisfy gate G4 (budget/mention/truncate); T6 satisfies G6's
integration surface (a resolver wired against a real second session). If T6
exceeds the 3-file budget or a clean seam is not reachable, DOCUMENT the
deferral per Open Question #1 — the resolver still ships and the gate passes.

## Self-Verification Checklist

- [ ] All tasks implemented; gate tests G4, G6 pass GREEN
- [ ] `max(3 refs, 0.2x window, 64KiB floor)` per-source budget exact (frozen)
- [ ] Projection is user/assistant ONLY (no tools/reasoning/system leaked)
- [ ] Spill only on truncation; locator + "use read on this path" hint on the
      model; untrusted `REFERENCE_WARNING` framing
- [ ] Reuses `llm.SpillService.Spill` — no second spill writer
- [ ] Files at exact paths; no sibling-leaf regression
- [ ] No deviations (or documented, incl. T6 deferral)

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [verify commands; last 20 lines]
3. **Deviations:** [none / list with rationale, incl. T6 status]

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; gate tests untouched and GREEN
- [ ] S3 signatures + constants match exactly (bytes, not tokens; 4 bytes/token)
- [ ] Untrusted framing present on all injected content (REFERENCE_WARNING)
- [ ] Spill failure is best-effort: a save-failure never fabricates a locator
      (reports `unavailable`), never fails the turn
- [ ] Path-sanitized spill session ids (reuse `sanitizeSpillSessionID`)
- [ ] No mutex across I/O; no ignored errors; no debug artifacts

Output: APPROVED or specific gaps with file + line references.

## Notes

- 4 bytes/token is a SIZING heuristic, not token counting (dsh index.ts:387) —
  do not redirect through a real tokenizer.
- `informational` — the budget is PER SOURCE, not aggregate: three refs each
  get up to `max(64KiB, floor(window*4*0.2))`.
- The drop/truncate order is load-bearing: drop oldest non-checkpoint WHOLE
  messages first, THEN head-tail truncate the longest retained, then append the
  omitted-bytes notice — never reverse (dsh projection.ts:94-146).

## Test Gate (branch F7)

| Row | Feature | Unit coverage | E2E coverage | Status |
|-----|---------|--------------|--------------|--------|
| F7 | Cross-session bounded reference (3-ref cap, 0.2x/64KiB budget, user/assistant projection, drop-then-head-tail truncate, spill+locator) | gate G4 (budget math, mention grammar, truncate) | gate G6 (prepare over a real second session + spill) | PENDING |

Traceability: R6 (session-ref, master C7) → [T1-T6]; branch F7 → gate G4, G6.
