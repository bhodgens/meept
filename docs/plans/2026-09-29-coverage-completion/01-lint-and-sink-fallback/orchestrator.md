# Area 01 — Lint Zero + PlanService Sink Fallback (orchestrator)

## Goal

Zero golangci/gosec findings repo-wide, and PlanService gains the evolver
sink fallback the RPC layer already has — including landing the stray
test that documents the intended API.

## Children

| Doc | Scope |
|---|---|
| `01-gosec-zero.md` | Fix 7 gosec G115/G123 findings in 4 files |
| `02-planservice-sink.md` | PlanService sink fallback + land the stray TDD test |
| `03-stray-test-and-lint-zero.md` | Resolve the untracked broken test, final full-repo lint pass |

## Dispatch order

01 → 02 → 03 (03 runs the final full-repo verification).

## Contracts

- `02` adds `(*PlanService).SetEvolverSink(m *plan.PlanManager, store plan.PlanStore)`
  and fallback fields `fallbackManager *plan.PlanManager`,
  `fallbackStore plan.PlanStore` — mirroring `internal/rpc/plan.go`
  PlanHandler exactly (usedFallback pattern from commit d9688804).
- `03` must NOT modify plan_service.go (02 owns it).

## Completion tracking

| Child | Status | Commit |
|---|---|---|
| 01-gosec-zero | pending | — |
| 02-planservice-sink | pending | — |
| 03-stray-test-and-lint-zero | pending | — |

## Final verification (orchestrator runs after 03)

- `golangci-lint run ./...` — zero typecheck errors, zero findings
- `gosec -quiet -include=G115,G123 ./internal/...` — zero findings
- `go build ./...` clean; `make e2e-fast` full tier green
- `make graphs` if any internal/ file line numbers shifted; commit graphs

## Test Gate

<!-- Tests-first gate (hierarchical-planning skill v1.3.0, patched 2026-10-24).
     Coverage matrix derived mechanically from leaf task headers; the control
     agent (orchestrator, in-session) refines rows and fills test names at
     Phase 0 dispatch. No implementation leaf dispatches until every row is
     VALIDATED. Delta protocol: lesser of plan/tests updates to the greater.
     Recipe: skill hierarchical-planning -> references/tests-first-gate.md -->

| # | Feature (from plan) | Source | Unit tests | E2E tests | Status |
|---|---------------------|--------|-----------|-----------|--------|
| F1 | Leaf 01 — gosec G115/G123 zero | 01-gosec-zero.md | (pending) | (pending) | PENDING |
| F2 | Leaf 02 — PlanService evolver sink fallback | 02-planservice-sink.md | (pending) | (pending) | PENDING |
| F3 | Leaf 03 — stray test + final lint zero | 03-stray-test-and-lint-zero.md | (pending) | (pending) | PENDING |

Statuses: PENDING | DELTA | VALIDATED

Delta log: (none yet)
