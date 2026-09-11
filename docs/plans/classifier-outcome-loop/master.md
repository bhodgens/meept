# Classifier Outcome Loop - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.

## Meta

- **Role:** Root
- **Parent:** none (root)
- **Children:** 4 leaf documents
- **Scope:** Close the classifier accuracy loop: persist per-dispatch
  records with privacy hardening, capture outcome/correction signals,
  and expose a harvest pipeline that turns live traffic into corpus.

## Goal

Today the classifier logs WHAT it decided but never WHETHER the decision
was right. Accuracy improvement only happens offline (gold replay). This
tree implements `design.md` (committed 8dba8e0f, authoritative spec with
file:line citations): durable per-dispatch records (hashed, never raw),
outcome capture from two mechanical correction signals, and a harvest
pipeline feeding the existing adjudication -> corpus -> centroid-rebuild
workflow.

Note: docs/plans/classifier-observability/ is an EARLIER, COMPLETE tree
(provenance/fail-fast/digest-cwd — commits cb8031f3, 903644f0, 6f199716).
Its design.md is this tree's spec input. This tree lives in its own
directory to avoid touching that history.

## Architecture

`dispatch_log` (internal/metrics/store.go:314-328) already persists one
row per dispatch via RecordDispatch (dispatcher.go:3015-3020). Four
levers, in dependency order:

1. **L1** extends that table in place (input_hash/model/margin/turn_no/
   outcome/corrected_agent) and REMOVES raw input_summary persistence
   (privacy fix). Salted SHA-256 replaces raw text.
2. **L2** captures the Door-1 margin at dispatch time — including on
   abstentions, where it is currently discarded (Match returns nil,
   embedding_prefilter.go:316).
3. **L3** writes outcome/corrected_agent from two signals: re-route
   detection (Signal A, in recordDispatch) and failure/replan hooks
   (Signal B, Escalate + quickplan fallback).
4. **L4** harvest: nightly python job -> adjudication sheet -> corpus
   append (provenance + dedup) -> centroid rebuild.

Dependency graph: L1 -> {L2, L3} -> L4. L2 and L3 parallelizable.

## Interface Contracts

### Contract 1: dispatch_log schema + hash (L1 owns)

```sql
ALTER TABLE dispatch_log ADD COLUMN input_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE dispatch_log ADD COLUMN model TEXT NOT NULL DEFAULT '';
ALTER TABLE dispatch_log ADD COLUMN margin REAL;               -- NULL = not Door 1
ALTER TABLE dispatch_log ADD COLUMN turn_no INTEGER NOT NULL DEFAULT 0;
ALTER TABLE dispatch_log ADD COLUMN outcome TEXT NOT NULL DEFAULT 'pending';
ALTER TABLE dispatch_log ADD COLUMN corrected_agent TEXT NOT NULL DEFAULT '';
CREATE INDEX idx_dispatch_log_hash ON dispatch_log(input_hash);
CREATE INDEX idx_dispatch_log_outcome ON dispatch_log(outcome);
```

Tolerate-duplicate-column ALTER pattern proven at
internal/metrics/store.go:370-388; post-ALTER indexes created AFTER the
columns exist (ordering bug at store.go:390-397).

```go
// DispatchEntry (store.go:840-853) gains:
InputHash      string  `json:"input_hash" db:"input_hash"`
Model          string  `json:"model" db:"model"`
Margin         *float64 `json:"margin,omitempty" db:"margin"` // nil = not Door 1
TurnNo         int     `json:"turn_no" db:"turn_no"`
Outcome        string  `json:"outcome" db:"outcome"`   // pending|ok|corrected|failed_replan
CorrectedAgent string  `json:"corrected_agent,omitempty" db:"corrected_agent"`
// InputSummary stays in the struct for compile compatibility; ALWAYS
// written as "" going forward (privacy fix).
```

Hashing: input_hash = first 16 hex chars of SHA-256(salt_id || 0x00 ||
message). Salt: random 32 bytes, generated once per install, stored in
the meept config dir next to metrics.db (~/.meept/, store.go:25);
salt recorded in a config-dir sidecar file so rotation is possible
later; hash dedup only ever compared within a salt. Errors truncated
to 200 chars, key/token-shaped strings dropped (dispatcher.go:3054-3057
is the site). Owner: L1. Consumers: L3, L4.

### Contract 2: margin capture (L2 owns)

```go
// EmbeddingPrefilter (internal/agent/embedding_prefilter.go) gains:
type PrefilterVerdict struct {
    Routed         bool
    AssertedIntent string  // winning intent; "" when index empty
    Confidence     float64
    Margin         float64 // kNNVote.Margin (:220-225, computed :309)
    Suppressed     bool    // H6 gate or quickplan cue-guard suppression
}
func (p *EmbeddingPrefilter) SetVerdictObserver(fn func(PrefilterVerdict))
// Match (:306) invokes the observer on EVERY path — routed, suppressed,
// abstained (currently returns nil at :316 discarding the vote).
// Dispatcher prefilter block (:767-798) registers one observer;
// recordDispatch persists margin for all verdicts. Log line gains
// margin= fields on the prefilter branch.
```

Owner: L2. Consumers: L4 (near-miss harvest).

### Contract 3: outcome capture (L3 owns)

```go
// Signal A — in recordDispatch (dispatcher.go:3024-3085), after the
// INSERT of the current row: UPDATE the most recent prior row for the
// same session_id WHERE outcome='pending':
//   IF current.agent_id != prior.agent_id
//      AND (current.turn_no - prior.turn_no) <= 3
//      AND prior.classifier_method != ''   -- classified path only
//   -> prior: outcome='corrected', corrected_agent=current.agent_id
//   ELSE -> prior.outcome='ok'
// turn_no = COUNT(*) over session rows + 1 (session_id indexed).
// Signal B — at Escalate (internal/agent/tactical.go:1440) and
// quickplan fallback (internal/agent/strategic.go:419-427):
//   UPDATE dispatch_log SET outcome='failed_replan' WHERE task_id=?
```

Rows with no successor stay 'pending' (excluded from accuracy
denominators). Owner: L3. Consumers: L4.

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 01 | 01-persist-privacy.md | leaf | none | ~60K | A |
| 02 | 02-margin-capture.md | leaf | 01 | ~50K | B |
| 03 | 03-outcome-capture.md | leaf | 01 | ~60K | B |
| 04 | 04-harvest-dashboards.md | leaf | 01,02,03 | ~50K | C |

**Concurrency groups:** A alone (schema first); B = {02, 03} in
parallel (disjoint files); C alone.

## Dispatch Protocol

### Phase 1: Dispatch Concurrency Group A

1. **Read** 01-persist-privacy.md; dispatch via `delegate_task` with
   leaf text + Contract 1 + INLINED regions: store.go (:25-35 config,
   :314-397 schema+ALTER pattern, :551-560 purge, :840-864
   DispatchEntry+INSERT), dispatcher.go (:2665-2672 extractSummary,
   :3015-3085 recordDispatch), handler.go (:702 extractSummary call).
2. Constraints: no commit / no git add / search_files+cat only / ASCII
   / gofmt / verify `go build ./...` + `go test ./internal/metrics/
   ./internal/agent/ -short -count=1`.

### Phase 2: Review and Commit Child 01

1. Orchestrator reviews in-session: schema delta == Contract 1;
   input_summary always ""; salted hash 16 hex; error scrub; tests
   mirror llm_calls_test.go; RecordDispatch no-op when metricsStore
   nil (multi-user disabled path invariant).
2. Commit: `feat(observability): dispatch_log schema + privacy hardening (outcome-loop L1)`.

### Phase 3: Dispatch Concurrency Group B (parallel)

Dispatch 02 and 03 simultaneously (disjoint files), each with leaf
text + its contract + INLINED pinned regions from the leaf. Review on
return; commit separately:
`feat(observability): door1 margin capture (L2)` /
`feat(observability): outcome capture (L3)`.

### Phase 4: Dispatch Concurrency Group C

Dispatch 04 with leaf text + Contracts 1-3 + harvest_hermes.py and
iter7_harvest.py (dedup guard :67-69) INLINED as patterns. Review;
commit: `feat(observability): harvest pipeline + dashboards (L4)`.

### Phase 5: Integration Review

1. `go build ./...` clean
2. `go test ./internal/metrics/ ./internal/agent/ ./internal/daemon/
   ./internal/plan/ -short -count=1` all pass
3. `make mutexio && make predid` clean
4. Scratch smoke: 3 messages through the scratch daemon; confirm
   dispatch_log rows have input_hash (no raw text), margin on Door-1
   rows, outcome transitions pending->ok/corrected
5. Harvest dry-run: `harvest_outcomes.py --dry-run` prints the
   candidate sheet without writing

## Review Checklist

- [ ] All tasks per leaf implemented; table-driven tests passing
- [ ] Contracts 1-3 satisfied exactly
- [ ] PRIVACY: no raw message text persisted anywhere; error strings
      truncated; hash salted with per-install salt
- [ ] RecordDispatch stays a no-op when metricsStore nil
- [ ] Existing dispatcher.stats RPC unaffected
- [ ] No scope creep; no TODOs; gofmt/vet/mutexio/predid clean

## Coding Conventions

- Go 1.22+, stdlib + existing deps; no new third-party imports
- Exported PascalCase / unexported camelCase
- Errors wrapped %w; no panic; every error handled (pre-commit blocks
  ignored errors)
- Table-driven tests in _test.go alongside; mirror
  internal/metrics/llm_calls_test.go patterns
- gofmt before reporting; ASCII only (pre-commit ascii-check)
- SQL: tolerate-duplicate-column ALTERs; indexes after columns exist

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 01-persist-privacy.md | COMPLETE | 1 | 1a68a9b5 — schema delta + salted hash + raw text removal + error scrub; 9 metrics tests + privacy tests; one informational fmt.Errorf note (non-blocking) |
| 02-margin-capture.md | COMPLETE | 1 | 5b2b32e4 — PrefilterVerdict observer on all Match paths; abstain margin preserved (the point); 5 tests |
| 03-outcome-capture.md | COMPLETE | 1 | c5e018f3 — ResolvePendingOutcome + MarkTaskFailedReplan; Signal A in recordDispatch; Signal B at Escalate + quickplan fallback; 6+ store cases |
| 04-harvest-dashboards.md | COMPLETE | 1 | 440de6ca — harvest_outcomes.py read-only + gitignored text join + 4 idempotent views; verified graceful zero-state on pre-L1 daemon DB; docs section added |

Status values: PENDING | IN_PROGRESS | IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

## Integration Test Plan

1. `go build ./...` zero errors
2. `go test ./internal/metrics/ ./internal/agent/ ./internal/daemon/
   ./internal/plan/ -short -count=1` all pass
3. `make mutexio && make predid` clean
4. Scratch-daemon live check: dispatch_log rows carry input_hash (16
   hex), model provenance on LLM-door rows, margin on Door-1 rows,
   outcome transitions across 3+ sequential messages
5. `harvest_outcomes.py --dry-run` prints candidate sheet; the
   local-only text-join output stays gitignored

## Structural Completeness Check

Required orchestrator sections present: Dispatch Protocol, Interface
Contracts, Review Checklist, Coding Conventions, Completion Tracking
Table, Integration Test Plan. Verified before first dispatch.

## Notes

- design.md (8dba8e0f) is the authoritative spec; leaves cite its
  sections (S1-S5).
- The raw input_summary removal is the headline privacy fix — call it
  out in the L1 commit message.
- Preserve: RecordDispatch no-op when metricsStore nil; multi-user
  disabled path byte-identical.
- Out of scope (design.md S2): free-text "no I meant X" correction
  parsing.
- L4 python runs under /opt/homebrew/bin/python3.14 (sklearn
  available); harvest stays READ-ONLY on metrics.db; the local-only
  text-join output must be gitignored (verbatim text never enters git).


## Post-completion note (2026-09-10)

The tfidf-veto Door-1 upgrade (PROMOTED by the acceptance run, see
tools/classifier-eval/results/m4-gold-acceptance.md) was implemented on
top of this tree's L1-L3 infrastructure: commit 10fe6d6d ships
internal/agent/tfidf_veto.go + scripts/build_tfidf_veto.py. The effect
is UNVALIDATED at the replay's sample size (2 routes) — the outcome
loop this tree built is the instrument that will validate it on live
traffic.
