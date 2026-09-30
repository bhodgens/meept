# Coverage Completion — Lint Zero, PlanService Sink Fallback, TUI e2e Phases

> **For Hermes:** Execute via hierarchical-plan-execution. Dispatch leaf
> documents to fresh subagents; orchestrators (main session) review,
> reconcile, and commit. Leaves 01/02 and 02-x are parallelizable; 02-tui
> phases are sequential.

**Goal:** Close the remaining coverage gaps to a true 100%: zero
golangci/gosec findings, PlanService sink fallback parity with the RPC
layer, and the three TUI e2e phases from `docs/workflows/tui-e2e-plan.md`.

**Architecture:** Three independent work areas. Area 01 (lint zero +
PlanService sink fallback) is small and mechanical. Area 02 is the TUI
plan's three phases, each a separate leaf because Phase 2 depends on
Phase 1's golden harness and Phase 3 on Phase 2's suite.

**Tech Stack:** Go 1.26, golangci-lint, gosec, bubbletea v2,
e2e/harness (scratch daemon + fake LLM), testify.

---

## Current state (measured 2026-09-29, evidence-based)

- golangci-lint full repo: **2 reported errors**, both from ONE broken
  untracked test file (`internal/services/plan_service_sink_test.go`,
  never committed — references `svc.SetEvolverSink` which does not exist
  on PlanService). The ~1100 figure in older docs is STALE: the lint-fix
  waves + 91e20d89 already drove real findings to zero.
- gosec scoped (G115, G123): **7 findings** —
  `internal/memory/vector/store.go` (3),
  `internal/tui/vim/mode.go` (1), `internal/tui/thread_indicator.go` (1),
  `internal/code/ast/parser.go` (1), plus 1 more in vector.
- **PlanService sink fallback is MISSING** (real product gap, same class
  as the rpc/plan.go bug fixed in d9688804): `internal/services/plan_service.go`
  Approve/Reject/Confirm/Revise/Get read back from the shared store only;
  no fallbackManager/fallbackStore wiring, no SetEvolverSink. The stray
  test file was written to drive this fix and never completed.
- TUI: plan exists (`docs/workflows/tui-e2e-plan.md`, 3305f6c9), zero of
  three phases implemented.

---

## Interface contracts between areas

- Area 01 changes `internal/services/plan_service.go` (adds
  `SetEvolverSink` + fallback fields/methods). Area 02 does NOT touch
  plan_service.go — no contract overlap.
- Area 01's stray-test resolution: the untracked
  `plan_service_sink_test.go` becomes the leaf's TDD target — its intent
  (sink fallback through PlanService) is correct; its API assumption
  (`svc.SetEvolverSink`) is what the leaf implements.
- Area 02 Phase 2 adds `e2e/suites/tui-flows/` and edits
  `e2e/manifest.json` path_map (`internal/tui/` → `tui-flows`).
  No other area touches the manifest.
- All areas: final verification is `go build ./...` +
  `make e2e-fast` (full tier) + `make graphs-check`.

---

## Child document index

| Doc | Type | Scope | Depends on |
|---|---|---|---|
| `01-lint-and-sink-fallback/orchestrator.md` | branch | lint zero + gosec fixes + PlanService sink fallback (using the stray test as TDD seed) | none |
| `02-tui-e2e/orchestrator.md` | branch | TUI phases 1-3 per tui-e2e-plan.md | none |

Dispatch order: both branches in parallel (disjoint file sets).

## Completion tracking

| Child | Status | Commit |
|---|---|---|
| 01-lint-and-sink-fallback | pending | — |
| 02-tui-e2e Phase 1 | done | 5fdeec90 |
| 02-tui-e2e Phase 2 | done | 5c7247bf |
| 02-tui-e2e Phase 3 | done | b1e592da |

## Integration review plan

After both branches: main session runs `go build ./...`,
`make e2e-fast` (full 50-suite tier), `make graphs-check`, verifies
golangci/gosec return zero findings, then pushes upstream/main.
