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
