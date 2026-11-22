# 06-planning/01 - Approval Visibility - PROGRESS

Worktree: .hermes/worktrees/usability-remediation (HEAD bcd27bf9). Do NOT commit.

## Gate status snapshot (before any edits)

`go test ./internal/tui/ -run 'TestPendingApprovalCard|TestRejectFromCard|TestApproveFromCard|TestPlansViewTaskGate' -count=1`:
- TestPendingApprovalCardRendersInOwningSession RED (orphan task.pending_approval in App event switch)
- TestPendingApprovalCardSessionScoped RED (same reason; positive control)
- TestRejectFromCardNeverExecutes PASS (05-navigation/01 landed /plan reject -> task.reject wiring)
- TestApproveFromCardCallsApproveRPC PASS (same, approve half)
- TestPlansViewTaskGate RED (plans view reads only plan.list_by_session; no task-gate merge)

`go test ./internal/agent/ -run TestThresholdConfig -count=1`: RED
- TestThresholdConfig / zero-disables rows RED (NewStrategicPlanner clamps <=0 -> 5)
- TestThresholdConfigDefaultKey RED (config.OrchestratorConfig has no TaskApprovalStepThreshold)
- TestThresholdConfigZeroNotClamped RED (field-level clamp resurrection)

Full-package run additionally fails TestModeBadgeDistinguishesModes +
TestEvolverDocRender{PlansTab,PlansDetail,Sidebar} -- sibling-leaf gates
(05-navigation modes / evolver doc render), tracked against the HEAD
baseline (/tmp/head-tui-failures.txt via scratch worktree
scratch-06p01-base); this leaf must leave the failure set a strict subset.

## Plan

1. T1: add "task.pending_approval" case to the App EventStreamDataMsg
   switch (app.go) mirroring the plan-notification session scoping
   (drop empty/foreign session_id, honor ChatSessionID fallback); new
   models.TaskGateApprovalMsg rendered as an approval card in the chat
   transcript (task id, step count, per-step lines, approve/reject
   affordances with /plan hint + keys). Card state stays pending until
   task.approved/task.rejected resolves it.
2. T2: config.OrchestratorConfig.TaskApprovalStepThreshold (json/toml
   task_approval_step_threshold, DefaultConfig = 5); remove the <=0->5
   clamp in NewStrategicPlanner (0/negative = disabled; requiresApproval
   already guards > 0); plumb cfg.Orchestrator.TaskApprovalStepThreshold
   into the daemon StrategicPlannerConfig construction.
3. T3: PlansModel.SetPendingTaskGates + merge in updatePlansTable
   (synthetic PlanExtended rows, state "awaiting approval"); App pump
   feeds the snapshot from the pending_approval switch case.
4. Verify: gates GREEN, full ./internal/tui/... failure set strict
   subset of HEAD baseline, go build ./..., gofmt clean.

## Log

- [x] Read leaf doc + gate files; baselines captured above.
- [x] Re-scoped on re-dispatch: T2 (threshold config) landed elsewhere
  (context confirmed orchestrator.task_approval_step_threshold default 5);
  05-navigation/01 already wired /plan approve|reject to task.approve /
  task.reject. This leaf's remaining scope is TUI-only: T1 card render +
  switch case, T3 plans-view merge. T2 marked N/A here.
- [x] T1 card render + switch case: new internal/tui/approval_card.go
  (handlePendingApprovalEvent + payload accessors); TaskGateApprovalMsg +
  ChatModel.renderTaskGateCard in models/chat.go (localOnly so transcript
  rebuilds re-append it); "task.pending_approval" case in the App event
  switch (app.go) with plan-family session scoping; card advertises
  /plan approve|reject (wired to task.approve/task.reject by 05-nav/01).
- [x] T1 race hardening (found under -count stress): chat tracks
  sessionLoadPending (SetSession arms / SessionMessagesLoadedMsg clears,
  IsSessionLoadSettled accessor); the pump parks payloads that arrive
  during the load window in App.pendingGateCards and
  App.drainParkedGateCards renders them after the authoritative rebuild
  (wired into the SessionMessagesLoadedMsg case); cards marked localOnly
  survive any later rebuild. loadSessionImpl no longer rebinds to an
  empty-ID auto-resume answer (world/{} shell) nor re-resumes while a
  session is live (resume is startup-only) - both were late-rebind
  transcript wipes.
- [x] T3 plans-view merge: PlansModel.SetPendingTaskGates overlay (map
  keyed by task id, sessionMu-guarded like sessionID);
  pendingTaskGatesForFilter + merge in updatePlansTable (synthetic row,
  review state icon, "awaiting approval"); live header count;
  gate-only tables render (View guards + SetSize repopulate include
  gates); ReapplyGatesForSession rebind recovery; warning banner names
  the approve/reject keys.
- [x] headless_test.go finish(): parked-card drain after quit (loop-dead
  barrier) + expectedSession rebind recovery in send()/finish.
  approval_card_test.go world: recorder seeded (session.messages.get
  empty transcript, session.get_most_recent world session) - barrier
  determinism only; gate assertions untouched.
- [x] Verification sweep: go test ./internal/tui/ -run
  'TestApprovalCard|TestPlanApproval|TestPendingApprovalCard|
  TestRejectFromCard|TestApproveFromCard|TestPlansViewTaskGate'
  -count=30 -v: 150/150 PASS (5 tests x 30), zero flakes in the final
  configuration. go test ./internal/tui/... -count=1: failure set =
  {EvolverDocRenderPlansDetail, EvolverDocRenderPlansTab,
  EvolverDocRenderSidebar, ModeBadgeDistinguishesModes} - strict subset
  of HEAD baseline (HEAD also failed the 3 gate tests; all now GREEN).
  go build ./... clean; gofmt clean; no sibling-leaf files touched
  (internal/agent, internal/config, internal/daemon untouched).
- [x] 01-REPORT.md written. NOT committed (per leaf contract).
