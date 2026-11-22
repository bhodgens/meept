# 06-planning/01 - Approval Visibility - REPORT

Worktree: .hermes/worktrees/usability-remediation (HEAD bcd27bf9 + this leaf's
uncommitted changes). Do NOT commit (orchestrator commits after review).

## 1. Files touched

- **internal/tui/approval_card.go** (NEW) - `App.handlePendingApprovalEvent`
  (session-scoped `task.pending_approval` payload -> `models.TaskGateApprovalMsg`),
  `stringField`/`intField` payload accessors.
- **internal/tui/app.go** - new `task.pending_approval` case in the
  EventStreamDataMsg switch: renders the card into the OWNING session's chat
  (same dual-binding session scoping as the plan family), parks payloads that
  arrive during the chat session-load window (`App.pendingGateCards`),
  flashes the plans tab, feeds `plans.SetPendingTaskGates`.
  New `App.drainParkedGateCards` (called from the
  `models.SessionMessagesLoadedMsg` case and from headless finish).
  `loadSessionImpl`: auto-resume refuses empty-ID answers and is
  startup-only (a live transcript is never re-bound by a duplicate load) -
  both were late-rebind transcript wipes under stress.
- **internal/tui/models/chat.go** - `TaskGateApprovalMsg` type;
  `ChatModel.Update` case rendering the card via
  `renderTaskGateCard` (task id, step count, per-step lines,
  `/plan approve|reject` affordances); card marked `localOnly` so
  transcript rebuilds re-append it instead of wiping it.
  `sessionLoadPending` flag + `IsSessionLoadSettled` accessor.
- **internal/tui/models/plans.go** - `SetPendingTaskGates` overlay
  (map keyed by task id, sessionMu-guarded); `pendingTaskGatesForFilter`
  session scoping; merge in `updatePlansTable` (synthetic row with review
  icon + "awaiting approval"); LIVE header count in `renderHeader`
  (cached count let a stale 0 reach the render); View guards + SetSize
  repopulation include gate rows (gate-only tables render);
  `ReapplyGatesForSession` rebind recovery; `TaskGateApprovalMsg` Update
  case; warning banner naming the approve/reject keys.
- **internal/tui/headless_test.go** - `finish()` rebind recovery
  (`expectedSession` tracked in `send()`, restored before quit) +
  post-quit parked-card drain (loop-dead barrier). No gate assertions
  weakened.
- **internal/tui/approval_card_test.go** - gate test COMMITTED at f326aef8,
  untouched assertions; only `gateApprovalWorld` seeding added
  (`session.messages.get` -> empty transcript,
  `session.get_most_recent` -> world session) to make the world's async
  barriers deterministic (the gate events ride the same message loop as
  the init-time fetches, so an unseeded recorder made the barrier racy).
- **docs/plans/.../06-planning/01-PROGRESS.md** - extended per task.

T2 (threshold config) NOT done here: landed elsewhere
(`orchestrator.task_approval_step_threshold` default 5 confirmed in
dispatch context); leaf re-scoped to TUI-only on this re-dispatch.

## 2. Verify output

Gate command (GREEN, stable across 30 full repeats):

```
go test ./internal/tui/ -run 'TestApprovalCard|TestPlanApproval|TestPendingApprovalCard|TestRejectFromCard|TestApproveFromCard|TestPlansViewTaskGate' -count=30 -v
=> 150/150 PASS (5 tests x 30 runs), 0 FAIL
```

Full-package subset check (`go test ./internal/tui/... -count=1`):
failure set = {TestEvolverDocRenderPlansDetail, TestEvolverDocRenderPlansTab,
TestEvolverDocRenderSidebar, TestModeBadgeDistinguishesModes} - strict
SUBSET of the HEAD baseline (/tmp/head-tui-failures.txt via scratch
worktree scratch-06p01-base, which additionally failed
TestPendingApprovalCardRendersInOwningSession,
TestPendingApprovalCardSessionScoped, TestPlansViewTaskGate - all GREEN
now). Zero new failures.

`go build ./...` clean. `gofmt -l internal/tui/` clean. ASCII-only on
added lines (0 em-dashes in the diff's + lines).

## 3. Live-gate evidence (T2 live-gate deferred to 06/03's scope)

- Card render: TestPendingApprovalCardRendersInOwningSession pins the card
  (task id, step count 6, approve/reject affordances) in the owning
  session's transcript - GREEN.
- Session scoping: TestPendingApprovalCardSessionScoped (foreign-session
  event never floods; owning session renders) - GREEN.
- C2 reject-never-executes: TestRejectFromCardNeverExecutes asserts
  task.reject{task_id, reason verbatim} with ZERO task.approve calls - GREEN
  (load-bearing pin, carried over from 05-navigation/01's wiring).
- C2 approve: TestApproveFromCardCallsApproveRPC (exactly one
  task.approve{task_id}) - GREEN.
- Plans view: TestPlansViewTaskGate (header count (1/1) + awaiting-
  approval affordance for a task-gate row absent from plan.list_by_session)
  - GREEN.
- Physical >=5-step plan rig test: out of scope for this leaf's rig
  (requires a live daemon); the gate tests pin the TUI side end-to-end via
  the real event pump + real RPC recorder.

## 4. Deviations / notes for the reviewer

1. **Session-load barrier + parking (new machinery, justified):** under
   `-count` stress the card raced the chat transcript's deferred
   `session.messages.get` rebuild - a card appended before the rebuild
   landed was silently wiped. The chat now tracks
   `sessionLoadPending`; the pump parks payloads that arrive during the
   window; `drainParkedGateCards` renders them after the authoritative
   rebuild. Cards are `localOnly` (never persisted, re-appended across
   rebuilds - same C6 contract as client-local notices).
2. **loadSessionImpl hardening (app.go, outside the leaf's original file
   list but required for gate stability):** auto-resume accepted an
   empty-ID session shell and fired on every duplicate dispatch; either
   could re-bind a live transcript away mid-turn. Resume is now
   startup-only and refuses empty-ID answers. This is a real product fix,
   not test-only: the same wipe path would orphan a live approval card in
   production.
3. **Live header count:** `renderHeader` derives the (visible/total) count
   from current state instead of the updatePlansTable cache - the cache
   could go stale when a gate arrives after the last store refresh.
4. **Headless harness changes:** `finish()` now performs rebind recovery +
   parked-card drain; `send()` records `expectedSession`. These are test
   infrastructure for the new async behavior; no assertions changed.
5. Review checklist: card is scoped to the owning session (pump + drain
   both enforce); reject never executes (load-bearing pin GREEN); reason
   forwarded verbatim; gate untouched; conventions kept (ASCII, %w,
   gofmt); no artifacts left (zz_probe files removed).
