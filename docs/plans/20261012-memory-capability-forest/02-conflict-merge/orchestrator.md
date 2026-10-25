# Tree 02 — LLM Conflict Merging (orchestrator)

Parent: `../master.md`. Conventions: `../../SHARED-CONVENTIONS.md` (§2.3
Decision contract, §5 waves).

**Goal:** Port TAM's batch conflict detection: before storing newly extracted
claims, recall similar existing claims and ask the LLM once per batch to
decide store / merge / supersede / skip, with merged content. Replaces the
current similarity-threshold-only duplicate rejection
(`internal/memory/distill.go` `DefaultDistillSimilarity = 0.85`) for the
extraction path.

**Architecture:** New subpackage `internal/memory/conflict`. Pure detection:
`Detector.DetectBatch(ctx, candidates []Candidate) ([]Decision, error)`.
Fail-open (detector error → all `store`) so extraction availability never
depends on the conflict LLM call. Wiring lands in Tree 01's job handler
(dual dependency resolved in wave 3) and a standalone path for
`meept memory supersede` users.

## Interface contracts

- Decision type: frozen in SHARED-CONVENTIONS §2.3. `Action` values are
  exactly `"store" | "merge" | "supersede" | "skip"`.
- `Candidate{ID, Content, Type string, Embedding []float64}` — produced by
  the caller (L1 handler) from extraction output.
- Consumer rule: `merge` → caller writes merged content as a NEW claim and
  supersede-closes targets via existing `SupersedeClaim` semantics
  (`epistemic.go:570-589`); `supersede` → caller supersedes targets with the
  candidate; `skip` → drop candidate; `store` → plain store.
- Auto-cannot-supersede-confirmed rule is enforced by existing
  `SupersedeClaim` (keep relying on it; do not duplicate in conflict pkg).
- Config: `memory.conflict.enabled` default **false**; when false, wiring is
  bypassed entirely (no LLM call, no recall).

## Child Index (inline leaves)

| Leaf | Wave | Scope | Est. |
|------|------|-------|------|
| L1 conflict-detector | 2 | `internal/memory/conflict/` pkg: recall, prompt, parse, tests | 80K |
| L2 merge-wiring | 3 | wire into Tree 01 L1 handler + CLI trigger + docs | 60K |

## Dispatch Protocol

Standard (root master). L1 brief inlines the §2.3 type and the four-action
consumer rule verbatim. L2 depends on Tree 01-L3 being COMPLETE (same file:
distill_async.go) — dispatch only after.

## Review Checklist

Root checklist plus:
- [ ] Fail-open verified: injected detector LLM error → all decisions `store`
- [ ] Malformed LLM JSON (incl. `<think>`-wrapped) → all `store`, no partial
      application
- [ ] No merge writes inside the conflict package (it decides; caller writes)
- [ ] `memory.conflict.enabled` default false; zero LLM calls when disabled

## Coding Conventions

Per `../../SHARED-CONVENTIONS.md` §3: Go stdlib-first, sentinel errors with
`%w` wrap, `pkg/id.Generate()` IDs, JSON5 config opt-in defaults, no mutex
across I/O, lowercase UI text, no `os.Getwd()`, GPL/AGPL dep license stop.

## Integration Test Plan

L2 e2e addition (suite `memory-layered-distill` gains a conflict scenario or
new suite `memory-conflict-merge`): seed existing claim "user deploys on
Fridays"; extract candidate "user deploys on Friday afternoons" → assert
merged/superseded outcome per fake LLM verdict, calibration log written, and
`claim_verdicts.jsonl` reflects the merge.

## Completion Tracking Table

| Leaf | Status | Notes |
|------|--------|-------|
| L1 conflict-detector | PENDING | |
| L2 merge-wiring | PENDING | |

## Leaf definitions

### Leaf L1 — conflict-detector (Wave 2)

**Scope:** Create `internal/memory/conflict/`:
- `types.go` — Decision/Candidate/Action exactly per SHARED-CONVENTIONS §2.3
  (with `// SPEC (SHARED-CONVENTIONS.md §2.3):` comment).
- `recall.go` — `topSimilar(ctx, store RecallStore, c Candidate, k int)`:
  vector search via `Manager.VectorSearcher` seam (interface param, no
  internal/memory import cycle), fallback to token-overlap scoring when
  embeddings unavailable (mirror existing `similar` edge scoring in
  `internal/memory/graph.go` canonical-text rule).
- `prompt.go` — batch conflict prompt: system prompt + numbered candidates +
  recalled targets → strict JSON array output
  `[{candidate, action, target_ids, merged_content, reason}]`. Prompt
  instructs: merge only when both statements are true-aspects of one fact;
  supersede when the candidate replaces a stale target; skip exact dupes.
- `detect.go` — `Detector.DetectBatch`: group into batches ≤20, one LLM call
  each, strip `<think>` blocks and code fences before JSON parse, fill
  missing candidates with `store`, ANY error → all `store` + structured log.
- `conflict_test.go` — fake-LLM tests: happy merge, malformed JSON fail-open,
  `<think>`-wrapped JSON parse, missing-entry fill, batch splitting, empty
  recall (skip detection per TAM fallback semantics).

**Verify:** `go build ./internal/memory/... && go test ./internal/memory/conflict/ -count=1`.

**Do NOT commit.**

### Leaf L2 — merge-wiring (Wave 3, after Tree 01-L3)

**Scope:**
- In Tree 01's `handleL1Extract`: after parse, when
  `memory.conflict.enabled`, run `conflict.Detector.DetectBatch` and apply
  the consumer rule (merge → new claim + supersede targets; supersede →
  supersede; skip → drop; store → store). Log per-decision line with
  action + target IDs.
- Config fields `memory.conflict.enabled` (+ `top_k` default 5,
  `batch_size` default 20) in `internal/config` + template comments.
- CLI: `meept memory conflicts --since=48h` — lists claims created by merge
  decisions (metadata `conflict_action=merge`) for audit.
- Docs: `docs/workflows/memory.md` — "Conflict Detection & Merge" section
  documenting the four actions, fail-open rule, and config.
- e2e per orchestrator Integration Test Plan.

**Verify:** `make e2e-fast-area AREA=memory-conflict-merge && go vet ./internal/memory/...`.

**Do NOT commit.**

## Test Gate

<!-- Tests-first gate (hierarchical-planning skill v1.3.0, patched 2026-10-24).
     Coverage matrix derived mechanically from leaf task headers; the control
     agent (orchestrator, in-session) refines rows and fills test names at
     Phase 0 dispatch. No implementation leaf dispatches until every row is
     VALIDATED. Delta protocol: lesser of plan/tests updates to the greater.
     Recipe: skill hierarchical-planning -> references/tests-first-gate.md -->

N/A — no leaf documents found alongside this orchestrator. If this tree
gains leaves, build the coverage matrix before dispatching them.
