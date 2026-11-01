# Demotion: Semantic-to-Episodic Path - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit - the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files - explore with search_files or terminal cat. After writing a
> file, do NOT read it back - write once and stop. Report what you built,
> files touched, and any deviations.

## Meta

- **Parent:** ../orchestrator.md
- **Scope:** Semantic-to-episodic demotion (contract C6): stale/low-value
  semantic claims are copied into the episodic tier and marked demoted,
  idempotently, driven by staleness thresholds; wired into the policy
  engine's action set.
- **Dependencies:** 02-policy-automation.md
- **Estimated Context:** 70K
- **Concurrency Group:** C
- **Audit references:** R7

## Goal

Close the missing lifecycle direction: today claims flow episodic ->
semantic (L1 extract) but never return. When a semantic claim goes stale
(internal/memory/staleness.go scoring) or loses retrieval utility, it
should DEMOTE to episodic: content preserved losslessly in the episodic
store, semantic entry marked (metadata flag, not deleted) so recall stops
surfacing it as a claim while history retains it. Demotion is idempotent
(re-planning a demoted ID is a noop) and executes only from the policy
engine's background execution path (never the chat path).

## Context

- internal/memory/staleness.go - staleness scoring - the demotion signal.
- internal/memory/types.go:13-29 - MemoryType constants - demotion moves
  effective tier from MemoryTypeClaim to MemoryTypeEpisodic.
- internal/memory/episodic.go - episodic store Add path - demotion copy
  target.
- internal/memory/tierpolicy/engine.go (sibling leaf 02) - Action/Engine
  this leaf extends with "demote" actions + PlanDemotions (contract C6).
- findings-memory-tiers.md - R-Bx citations for demotion criteria.

## Verified Anchors

- internal/memory/staleness.go - staleness scoring - demotion signal
- internal/memory/types.go:13-29 - MemoryTypeClaim / MemoryTypeEpisodic - tier taxonomy
- internal/memory/episodic.go - episodic Add path - copy target
- internal/memory/tierpolicy/engine.go - Action{Kind:"demote"} extension point (from leaf 02 contract C5)
- internal/memory/consolidation.go:96 - Consolidator.Run - prior art for pass-style execution (do NOT add to it; demotion executes via tierpolicy)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/memory/tierpolicy/demote.go  (contract C6, verbatim from parent)
func (e *Engine) PlanDemotions(ctx context.Context, semanticIDs []string) ([]Action, error)
// - queries staleness + usefulness via e.deps; emits Action{Kind:"demote",
//   TargetIDs:[id], Reason:"stale"|"low_usefulness"} only above thresholds;
//   already-demoted IDs => noop (omitted from plan).
// - Extend Execute (leaf 02) to handle Kind=="demote": copy to episodic
//   (lossless), set demoted metadata flag on the semantic entry.

// internal/memory/tierpolicy/thresholds.go
type DemotionThresholds struct { StaleDays int; MinUsefulness float64 }
func ResolveDemotionThresholds(cfg *config.MemoryTiers /* adapt by grep */) DemotionThresholds
// defaults documented; zero-value config => defaults (30d stale,
// usefulness floor consistent with PlanUsefulEviction's floor concept).
```

### What This Leaf Consumes

- C5 Engine/Deps/Action (../02-policy-automation.md) - extension point
- internal/memory existing: staleness scoring, episodic Add, claim store
  metadata (grep for the claim metadata/update API; adapt to REAL names)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | PlanDemotions: staleness/usefulness-gated action planning with idempotency (demoted flag checked) | Create: internal/memory/tierpolicy/demote.go; Test: internal/memory/tierpolicy/demote_test.go | `go test -p 2 ./internal/memory/tierpolicy/ -run TestPlanDemotions` | demote.go absent | stale claims => demote actions; fresh/high-usefulness => noop; already-demoted => omitted | Given 3 claims (stale, fresh, demoted); When PlanDemotions; Then exactly 1 action targeting the stale claim |
| T2 | Execute demote: copy claim content to episodic store losslessly; set demoted metadata flag; NOT delete; failure mid-copy leaves no partial state (episodic write first, then flag) | Modify: execute.go (from leaf 02), demote.go; Test: demote_execute_test.go | `go test -p 2 ./internal/memory/tierpolicy/ -run TestExecuteDemote` | Execute lacks demote case | stub Deps records episodic copy + flag update in that ORDER; no delete call; crash-between steps is recoverable (re-run completes) | Given demote action and stub Deps; When Execute; Then copy logged before flag; delete count == 0 |
| T3 | Thresholds + config: [memory.tiers.demotion] stale_days, min_usefulness under the leaf-02 config block; defaults 30 / floor consistent with usefulness eviction; resolve function with gendoc comments | Modify: internal/config/schema.go; Test: internal/config/schema_demotion_test.go | `go test -p 2 ./internal/config/ -run TestDemotionDefaults` | keys absent | zero-value => defaults applied; negative rejected at resolve | Given unset demotion config; When resolved; Then 30d + documented floor |
| T4 | Engine integration: Evaluate/plan path may emit demote alongside evict/compress without order violation (demote before evict in returned plan); tick execution covers all three kinds | Modify: engine.go, execute.go; Test: engine_demote_integration_test.go | `go test -p 2 ./internal/memory/tierpolicy/ -run TestEngineDemote` | demote not in plans | mixed plan ordered demote->compress->evict; single Execute pass handles all | Given over-ceiling semantic tier with stale claims; When planned; Then demote actions precede evict actions |
| T5 | Wiring + user visibility: demotion stats in the policy engine's ExecutionReport (demoted count) surfaced in the daemon tick log; no CLI/GUI change required (chat-path-free background feature) - report notes the wiring requirement exemption per AGENTS.md prototype/exception rule with justification | Modify: execute.go, internal/daemon/ (tick logging site from leaf 02); Test: demote_execute_test.go (report field) | `go test -p 2 ./internal/memory/tierpolicy/ -run TestReportDemoted` | report lacks demoted field | ExecutionReport.Demoted counted; daemon tick logs include it | Given 2 demotions; When Execute; Then report.Demoted==2 and log line present |

## Self-Verification Checklist

- [ ] All tasks implemented and tests passing (`go test -p 2 ./internal/memory/tierpolicy/ ./internal/config/`)
- [ ] Contract C6 satisfied exactly; idempotency proven by test
- [ ] No deletion in the demote path; no chat-path execution
- [ ] Config defaults documented; ASCII only; gofmt clean
- [ ] No deviations (or deviations documented in the report)

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [last 20 lines of go test output]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; tests passing
- [ ] Contract C6 matches exactly; idempotent; lossless copy
- [ ] Order rule (demote before evict) enforced and tested
- [ ] No debug artifacts, no scope creep, no line-number corruption

Output: APPROVED or specific gaps with file + line references.

## Notes

Cite findings-memory-tiers.md R-Bx demotion-criteria IDs in comments.
Adapt to the REAL claim-store metadata API found by grep; if claims lack
a mutable metadata flag, add one via the store's existing update path and
record the delta. The user-visible surface is the memory behavior itself
(claims stop surfacing as semantic after demotion; history retains them)
plus tick logging - record the AGENTS.md wiring-exception justification
(background policy feature, no new CLI surface) in the report.
