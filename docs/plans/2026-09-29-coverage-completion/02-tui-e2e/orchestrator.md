# Area 02 — TUI e2e Phases 1-3 (orchestrator)

## Goal

Implement the three phases of `docs/workflows/tui-e2e-plan.md`
(3305f6c9): headless interaction foundations, golden render suite, and
the `tui-flows` hermetic e2e suite. End state: `internal/tui` maps to a
behavioral suite, not thin smoke.

## Children

| Doc | Scope |
|---|---|
| `01-phase1-foundations.md` | newHeadlessApp helper + golden harness + first goldens + parity inventory test |
| `02-phase2-tui-flows.md` | e2e/suites/tui-flows/ against harness.Stack (5 flows) + manifest flip |
| `03-phase3-breadth.md` | remaining views goldens, modals, keybinding overrides |

## Dispatch order

Phase 1 → Phase 2 → Phase 3 (strictly sequential; each builds on the
previous layer).

## Contracts

- Phase 1 defines `newHeadlessApp(t *testing.T, rpc RPCClient, w, h int) (*App, *tea.Program, teardown)` in
  `internal/tui/app_test.go` (or a new `headless_test.go`) — Phases 2-3
  import it.
- Phase 1 defines the golden layout: `internal/tui/testdata/golden/`
  with `-update-golden` flag via a `golden.go` helper.
- Phase 2 owns `e2e/suites/tui-flows/` and the manifest path_map flip
  (`internal/tui/` → `tui-flows`). Phases 1/3 do not touch the manifest.
- All phases: every View() assertion runs against a FIXED
  WindowSizeMsg; wall-clock statusMessageTime must be frozen or avoided
  (see tui-e2e-plan.md hazards section).

## Completion tracking

| Child | Status | Commit |
|---|---|---|
| 01-phase1-foundations | done | 5fdeec90 |
| 02-phase2-tui-flows | done | 5c7247bf |
| 03-phase3-breadth | done | b1e592da |

## Final verification

- `go test ./internal/tui/...` green including goldens
- `make e2e-fast-area AREA=tui-flows` green
- `make e2e-fast` full tier green (manifest flip must not break other suites)
- `make graphs-check` green

## Test Gate

<!-- Tests-first gate (hierarchical-planning skill v1.3.0, patched 2026-10-24).
     Coverage matrix derived mechanically from leaf task headers; the control
     agent (orchestrator, in-session) refines rows and fills test names at
     Phase 0 dispatch. No implementation leaf dispatches until every row is
     VALIDATED. Delta protocol: lesser of plan/tests updates to the greater.
     Recipe: skill hierarchical-planning -> references/tests-first-gate.md -->

| # | Feature (from plan) | Source | Unit tests | E2E tests | Status |
|---|---------------------|--------|-----------|-----------|--------|
| F1 | Leaf 01 — TUI Phase 1: headless foundations + goldens + parity inventory | 01-phase1-foundations.md | (pending) | (pending) | PENDING |
| F2 | Leaf 02 — TUI Phase 2: tui-flows hermetic e2e suite | 02-phase2-tui-flows.md | (pending) | (pending) | PENDING |
| F3 | Leaf 03 — TUI Phase 3: breadth | 03-phase3-breadth.md | (pending) | (pending) | PENDING |

Statuses: PENDING | DELTA | VALIDATED

Delta log: (none yet)
