# Checkpointed Gate Pause/Resume - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit - the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files - explore with search_files or terminal cat. After writing a
> file, do NOT read it back - write once and stop. Report what you built,
> files touched, and any deviations.

## Meta

- **Parent:** ../master.md
- **Scope:** Plan and goal gate pauses persist a checkpoint (pending gate +
  turn re-entry data) through the existing TurnParker so approval can
  arrive out-of-band (Slack, CLI, after daemon restart) and the turn
  resumes from checkpoint.
- **Dependencies:** none (wires into leaf 01's resolve path at the daemon
  level, but compiles and tests independently)
- **Estimated Context:** 85K
- **Concurrency Group:** B
- **Audit references:** contract C5 in ../master.md

## Goal

Today a plan gate (internal/services/plan_service.go:182 Approve) or a goal
gate (internal/employee/goal.go:208 GateConfig, gate loop
goal_loop.go:1050-1189) blocks in-process; if the daemon restarts or the
approver is away, the turn dies. This leaf routes gate pauses through the
TurnParker (internal/agent/parked_turn.go) with a new ParkKindGate scope:
the pending gate + turn re-entry data persist via the parker's existing
SetParkPersistence store, and the existing approve/resolve paths trigger
the resume callback. Approval may arrive from any surface - including the
Slack channel from leaf 01 - with no new park machinery and no second
park path.

## Context

Meept is a Go agent daemon. The pieces you build on:
- internal/agent/parked_turn.go - TurnParker: Park/Resume lifecycle,
  parkKind scoping (chat vs episode) at :130-132,
  SetParkPersistence(store ParkPersistence, kind ParkKind) at :587-596
  (kind-scoped persistence rows; resumed rows deleted BEFORE the resume
  callback = at-most-once; Start re-arms surviving rows after restart,
  expired rows pruned at load). You ADD a ParkKind value and the gate
  data payload - you do NOT modify the parker's core semantics.
- internal/agent/retry_recovery.go:26 - Checkpoint struct is crash-recovery
  ONLY. Do NOT use it for approval state.
- internal/services/plan_service.go:182 - PlanService.Approve; the gate
  side lives around plan approval flow (see also cmd/meept/plans.go:341
  approve path).
- internal/employee/goal.go:208 - GateConfig; internal/employee/goal_loop.go
  :1050-1189 - the goal gate loop that currently waits in-process.
- internal/acp/relay.go:94-107 - PermissionNotifier; the resolve path is
  where out-of-band approvals surface (leaf 01 wires Slack resolves into
  the same relay; your resume trigger listens on the relay's resolved
  outcome or the plan/goal approval calls, whichever the code structure
  makes the single choke point - pin your choice and report it).
- Park persistence store precedent: the sqlite park store
  (park_store_sqlite.go, referenced at parked_turn.go:587) - reuse, do not
  create a new table for gate parks (the ParkKind scope keys the rows).

## Verified Anchors

Anchors grep-confirmed 2026-10-31. Re-grep before editing - siblings move lines.

- internal/agent/parked_turn.go:130-132 - parkKind field + scoping comment.
- internal/agent/parked_turn.go:587-596 - SetParkPersistence(store, kind).
- internal/agent/retry_recovery.go:26 - Checkpoint struct (crash-recovery
  only; not for approval).
- internal/services/plan_service.go:180-200 - PlanService.Approve flow.
- internal/employee/goal.go:200-230 - GateConfig definition.
- internal/employee/goal_loop.go:1050-1189 - gate wait loop.
- e2e/suites/quota-park/ - existing park-related e2e precedent (how a
  parked turn is observed hermetically).

## Interface Contracts (From Parent)

### What This Leaf Exposes

- C5: ParkKind constant ParkKindGate (TurnParker-scoped persistence rows
  via the existing SetParkPersistence), GateParkData struct exactly as
  pinned in ../master.md (GateKind "plan"|"goal", GateID, SessionID,
  TurnReentry map[string]any, RequestedAt), and a gate-pause integration:
  plan + goal gate pauses park with C5 data; resume re-enqueues the turn
  via the parker's existing callback mechanism.
  New file: internal/agent/gate_park.go (+ gate_park_test.go).

### What This Leaf Consumes

- [TurnParker] - Park / SetParkPersistence / Start / Resume semantics as
  they exist in internal/agent/parked_turn.go (read them; do not modify
  core parker behavior).
- [acp relay resolved outcome] - the resolved permission/decision event
  the resume trigger listens for (bus topics acp.permission.requested /
  .resolved, typed bus.ACPPermissionEvent - WS-visible precedent in
  internal/comm/AGENTS.md). If the plan/goal gates surface their pause
  through a different existing event, pin and report which.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | ParkKindGate + GateParkData + park helper (GatePause parks a turn with C5 payload through the EXISTING TurnParker.Park path) | Create: internal/agent/gate_park.go; Test: internal/agent/gate_park_test.go | `go test -p 2 ./internal/agent/ -run TestGatePark` | ParkKindGate absent | Park persists a GateParkData row scoped to ParkKindGate through a ParkPersistence store (in-memory fake + sqlite store both); payload JSON round-trips field-exact | Given a parked gate turn with persistence wired; When the parker reads back surviving rows; Then GateParkData decodes byte-identically and the row key is kind-scoped |
| T2 | Plan gate checkpointing: a paused plan gate parks the turn (C5, GateKind "plan"); Approve triggers resume | Modify: internal/services/plan_service.go (pause path) + the daemon-side seam that owns the TurnParker (grep SetParkPersistence / TurnParker construction sites); Test: internal/agent/gate_park_test.go (plan scenario) | `go test -p 2 ./internal/agent/ -run TestGateParkPlan` | no park on plan pause | pause parks; approve resumes the turn exactly once (at-most-once preserved); approve when not parked is unchanged | Given a turn blocked on a plan gate; When the plan is approved; Then the parked row is deleted BEFORE the resume callback runs and the turn re-enqueues with its TurnReentry payload |
| T3 | Goal gate checkpointing + restart re-arm: goal gate pause parks (GateKind "goal"); daemon restart re-arms the row; late approval still resumes | Modify: internal/employee/goal_loop.go (pause path parks instead of blocking in-process); daemon wiring in internal/daemon (Start re-arm already provided by the parker); Test: internal/agent/gate_park_test.go (goal scenario incl. simulated restart) | `go test -p 2 ./internal/agent/ ./internal/employee/ -run 'GatePark'` | no park on goal pause | park -> simulated restart (new parker over the same store, Start) -> resume fires once on approval | Given a parked goal gate and a daemon restart; When approval arrives after the restart; Then the re-armed row resumes the turn and a second approval is a no-op |

Worked example (canonical row expansion, T1):

1. Write failing test:

```go
func TestGateParkPersistsKindScoped(t *testing.T) {
    store := newFakeParkStore(t)
    p := agent.NewTurnParkerForTest(t, agent.WithParkStore(store, agent.ParkKindGate))
    data := agent.GateParkData{GateKind: "plan", GateID: "plan-1",
        SessionID: "session-1", TurnReentry: map[string]any{"turn_id": "t1"},
        RequestedAt: time.Now().UTC()}
    p.ParkGate(t.Context(), data)
    got := store.RowsForKind(agent.ParkKindGate)
    if len(got) != 1 { t.Fatalf("want 1 row, got %d", len(got)) }
}
```

2. Verify failure: `go test -p 2 ./internal/agent/ -run TestGatePark`
   Expected: FAIL - ParkKindGate / ParkGate not defined.
3. Minimal implementation: ParkKindGate constant + GateParkData struct +
   ParkGate helper that calls the existing park path.
4. Verify pass: same command, PASS.

## Self-Verification Checklist

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts satisfied exactly (C5 struct byte-identical)
- [ ] All files at exact specified paths
- [ ] No deviations (or deviations documented in the report)
- [ ] No scope creep - only what the tasks specify
- [ ] Single park path: grep proves all gate parks flow through
      TurnParker.Park; no second persistence mechanism; Checkpoint struct
      untouched
- [ ] At-most-once resume preserved (delete-before-callback); no double
      resume on duplicate approvals
- [ ] gofmt clean; go vet clean on touched packages

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [verify commands; last 20 lines of output]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; tests passing
- [ ] C5 struct + ParkKindGate exact vs master contract
- [ ] Core TurnParker semantics unmodified (diff shows no changes to
      parked_turn.go park/resume/persistence internals)
- [ ] Resume trigger is a single pinned choke point (documented in report)
- [ ] Restart re-arm covered by a test through the REAL parker, not a
      hand-built double
- [ ] Conventions followed; no bugs, no security issues
- [ ] No scope creep; no debug artifacts or line-number corruption

Output: APPROVED or specific gaps with file + line references.

## Notes

- The parker's expiry semantics apply unchanged: if the gate config carries
  a timeout, it maps onto the parker's existing expiry - do not add a new
  timer type. If gates have no timeout today, gate parks simply survive
  until approved (park rows are not auto-expired) - state your reading in
  the report.
- goal_loop.go:1050-1189 is a hot, carefully-tuned region: keep the
  integration minimal (park instead of block) and never re-order its
  existing checks.
- If the plan/goal approval paths are unreachable from internal/agent
  without an import cycle, put the trigger wiring in internal/daemon
  (pattern: other *_wiring.go files there) and report the seam.
