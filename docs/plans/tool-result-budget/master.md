# Tool-Result Budget Fixes - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.

## Meta

- **Role:** Root
- **Parent:** none (root)
- **Children:** 2 leaf documents under this node
- **Scope:** Fix the four failure modes of the dynamic tool-result
  budget (internal/agent/loop.go:4027-4035): silent page-tail loss from
  budget-dependent clipping, non-determinism from order-dependent
  truncation, random key loss in compressMapResult, and starvation of
  late-turn verification loops.

## Goal

`dynamicToolBudget = max(3000 × (1 − totalTokens/convBudget), 600)`
decays as a turn consumes budget. Every tool result is then compressed
to that budget (loop.go:4070 ToCompressedJSON; loop.go:4058 compression
pipeline). Consequences today:

1. **Silent page-tail loss.** transcript_fetch promises "offset +
   max_chars = the window you get." When the loop clips a page late in
   a turn, the model computes the next offset from the SHORTENED text
   and the un-clipped tail is skipped forever.
2. **Order-dependent non-determinism.** Identical calls return
   different content depending on turn position — breaking
   reproducibility (meept-bench, deterministic_cache posture).
3. **Random key loss.** compressMapResult (executor.go:527) iterates
   the map in Go's randomized key order and truncates wholesale at the
   budget — which keys survive is random. The model can lose `path`
   (the pointer to the full transcript) while keeping `timestamps`.
4. **Starvation.** Late-turn verification reads (failing test output)
   arrive most-degraded, punishing thorough turns.

Fix shape (two leaves, both surgical):

- **Leaf 01 — tool-declared result floors:** tools may declare
  `MaxResultTokens() int` (new optional interface in
  internal/tools/interface.go following the Categorizer pattern). The
  loop consults it via toolName → registry lookup at the consumption
  site and uses `max(dynamicToolBudget, declared)` per result — the
  decay gradient stays for everything else. transcript_fetch declares
  ~1400 tokens (covers a full 4k-char digest + metadata at 3 chars/
  token with headroom).
- **Leaf 01 also — metadata-key preservation:** compressMapResult
  gains a stable contract: the PRIMARY content key (first string value
  among "content"/"output"/"result", else the longest string value) is
  the only value eligible for truncation; every OTHER key is always
  preserved whole (they are small metadata: paths, counts, offsets,
  flags), with `_truncated: true` added when anything was clipped.
  Deterministic order: sort keys, primary key last. This makes map
  truncation reproducible AND metadata-safe.
- **Leaf 02 — skill + docs:** learn-from-video pages ~2k chars per
  slice (fits the 600-token floor ≈ 1800 chars), so even floor-degraded
  reads are whole pages; docs note the floor behavior.

## Architecture

Single seam, minimal diff. The loop already extracts `toolName` per
result at the consumption site (loop.go:4046-4049) and already holds
`l.registry`. The new optional interface mirrors the existing
`Categorizer` optional-interface pattern (interface.go:167) and the
registry type-assert pattern (loop.go:1043). compressMapResult is
called from exactly one site (executor.go:494); its callers pass
through ExecutionResult.Result maps only.

Out of scope: changing ToolResultMaxTokens, the 30k iteration window,
the 50k turn budget, or the compression pipeline's LLM path. The
gradient decay remains for undeclared tools — it is correct for them.

## Interface Contracts

### Contract 1: MaxResultTokens optional interface (C1)

```
// File: internal/tools/interface.go — following the Categorizer pattern:
// ResultSizer is an optional interface tools can implement to declare
// a minimum token budget for their results. The agent loop never
// compresses a result below this floor, regardless of the dynamic
// budget; declared floors larger than ToolResultMaxTokens are capped
// at ToolResultMaxTokens (a tool cannot exceed the global ceiling).
type ResultSizer interface {
    MaxResultTokens() int
}

// Helper following GetCategory:
func GetMaxResultTokens(t Tool) int  // 0 when not implemented or <= 0

// File: internal/tools/builtin/transcript_fetch.go:
// TranscriptFetchTool implements ResultSizer returning
// TranscriptResultTokens = 1400 (named constant; covers a 4k-char
// digest + metadata keys with 3-chars/token headroom).
```

### Contract 2: loop consultation (C2)

```
// File: internal/agent/loop.go — at the consumption site (~4027-4076):
// per-result effective budget:
//   budget := dynamicToolBudget
//   if tool := l.toolByName(toolName); tool != nil {
//       if floor := tools.GetMaxResultTokens(tool); floor > budget {
//           budget = floor
//       }
//   }
// (l.toolByName: new tiny helper wrapping l.registry.Get — the loop
// already type-asserts optional registry capabilities at loop.go:1043;
// registry may be nil, guard.)
// The SAME budget value feeds the compression-pipeline call (~4058)
// and ToCompressedJSON (~4070) so both paths agree.
```

### Contract 3: deterministic, metadata-preserving map compression (C3)

```
// File: internal/agent/executor.go — compressMapResult rewrite:
//   1. primary key := first present among "content", "output",
//      "result" whose value is a string; else the LONGEST string
//      value (ties: lexicographically first key). Deterministic.
//   2. all keys sorted; primary key processed LAST.
//   3. non-primary keys are copied whole and COUNT toward the budget
//      (they are small; if they alone exceed maxChars they are still
//      preserved — metadata integrity outranks the budget) and
//      compressed["_truncated"] = true is set in that extreme case.
//   4. the primary key alone absorbs truncation: when the remaining
//      budget after metadata is smaller than the primary value, the
//      primary value is truncated with the existing marker mechanics
//      (truncateWithMarker/compressCodeResult) and _truncated=true.
//   5. empty map / no string values: current behavior (copy all).
// Callers unchanged (executor.go:494 site passes the same map).
```

### Contract 4: skill page size + docs (C4)

```
// File: config/skills/learn-from-video/SKILL.md — step 2: "~400-line
// slices" becomes "~200-line slices (~2k chars)" so a whole page fits
// even at the 600-token budget floor (~1800 chars).
// File: internal/skills/learn_from_video_catalog_test.go — update the
// slice-size assertion if it pins the old number (grep first).
// File: docs/workflows/external-integrations.md — the Local
// summarization / File-backed output sections gain one sentence:
// "Results are never compressed below the tool's declared floor
// (1400 tokens for transcript_fetch); non-content metadata keys
// (path, offset, total_chars, chunk_count) always survive intact."
```

## Child Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 01 | 01-result-floor-and-map-keys.md | leaf | none | 40K | A |
| 02 | 02-skill-page-size-and-docs.md | leaf | 01 COMMITTED (asserts its behavior) | 20K | B |

**Concurrency groups:** group A = 01 alone (owns interface.go,
executor.go, loop.go, transcript_fetch.go). Group B = 02 after 01
commits.

## Dispatch Protocol

Same discipline as prior trees: leaf docs carry full TDD tasks, no-commit
rules, read-once discipline, scoped tests. Review in-session; commit
per leaf with explicit paths; integration gate after leaf 02.

## Review Checklist

- [ ] ResultSizer follows the Categorizer optional-interface pattern;
      GetMaxResultTokens caps at ToolResultMaxTokens and returns 0 for
      unimplemented/non-positive
- [ ] Loop consults per-result via toolName; nil-registry and
      unknown-tool guarded; same budget for pipeline + ToCompressedJSON
- [ ] compressMapResult: deterministic (sorted keys, primary last),
      non-primary keys always whole, _truncated set on any clip
- [ ] transcript_fetch declares 1400
- [ ] No changes to ToolResultMaxTokens, iteration window, turn budget
- [ ] Set* methods nil-guarded; no `_ =` errors; gofmt clean; no
      line-number corruption; existing suites green unmodified

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 01-result-floor-and-map-keys | COMPLETE (2026-09-06) | 1 | dd688f0a (tools pkg: ResultSizer + GetMaxResultTokens + transcript 1400) + c2ea0ace (agent pkg: helper, loop wiring, compressMapResult rewrite, 13 tests — committed by sibling session from the shared working tree; verified identical work). Review fix applied: compression-pipeline call lifted to the per-result floor too (it destructively rewrites Result; the floor never reached it otherwise). Deviations approved: JSON marshal sorts keys so the spec's order assertion was replaced with tight-budget byte-identical determinism + all-keys-present assertions; 200-char wrapper reserve corrected test math. |
| 02-skill-page-size-and-docs | COMPLETE (2026-09-06) | 1 | 5983587c. ~2k-char slices + floor rationale in the skill; floor/metadata sentence in both workflow docs; catalog test green. |

## Integration Test Plan

1. `go build ./internal/... && go vet ./internal/agent/
   ./internal/tools/...` — clean.
2. Suites: internal/tools/... internal/agent/ internal/skills/ -
   count=1 -p 2 — pass.
3. Determinism probe: repeat-map-compression test — same input,
   100 iterations, byte-identical output (strike the random-order
   failure mode).
4. Metadata probe: map with content 50k chars + path/offset keys at
   maxChars=2000 → output contains the FULL path/offset values +
   _truncated=true + a truncated content.
5. Floor probe: simulated late-turn budget (600) with a
   ResultSizer-declaring tool → result not compressed below 1400
   tokens; an undeclared tool at the same position → floor 600 as
   today.
6. Wiring greps: GetMaxResultTokens consulted at exactly one loop
   site; compressMapResult callers unchanged.
7. Catalog test still green with the updated slice size.

## Open Questions

- Should file_read/shell_execute also declare floors? Later tree —
  this one fixes the mechanism + the transcript consumer. Keep scope.
- Does the compression pipeline's LLM path need the same floor
  handshake? It receives dynamicToolBudget today; leaf 01 passes the
  effective (floored) budget so both paths stay consistent. No
  pipeline changes.

## Notes

- The 3 chars/token heuristic is baked into this repo's budget math
  (conversation.go:669); 1400 tokens ≈ 4200 chars — the 4k digest plus
  metadata fits with margin.
- Do NOT touch internal/agent/executor.go's ExecutionResult shape;
  compressMapResult's signature may gain nothing — keep it
  (map, int) → map.
- Scope guard: no new packages, no config schema changes, no RPC, no
  evolver. AGENTS.md: interface.go gains an exported interface + one
  helper in an existing package — no new package rows; leaf 01 states
  the verification.
