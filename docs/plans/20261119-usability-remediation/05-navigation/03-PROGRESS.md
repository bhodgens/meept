# 05-navigation / 03-restore-queue-visibility — PROGRESS

Worktree: .hermes/worktrees/usability-remediation (HEAD b161d027). Started fresh.

## Gate status (initial, re-checked fresh)

- `TestQueueStatusParked` — FAILS on missing `task_states` key (B5 zeros-only payload).
  `TestQueueStatusParkedWithActiveQueue` — depths pass, `task_states` missing.
  NOT blocked by sibling churn: gates run and fail for the documented reason.
- `TestRestorePolicyAligned` — all three subtests ERROR before assertions:
  `gateCallStub` unix socket path exceeds macOS 104-byte sun_path limit
  (subtest names push TMPDIR path to ~131 chars). Pre-existing harness defect in
  the shared `newGateCallStub` (also breaks TestPlanSlashForwards* committed by
  01-safety/01). Must fix the harness (short socket dir), NOT the assertions.

## Findings

- queue.status: internal/rpc/queue.go handleStatus returns only depths. Task
  store lookup must be session-scoped via `store.GetTasksForSession` (bounded,
  no full scan). Conversation ids in the task store's session_tasks join are
  SESSION ids; the RPC param is a conversation id — thread-scoped ids must be
  unwrapped (agent.unwrapThreadConversationID is private; rpc side needs the
  base via strings or an exported helper). Registry holds TaskStore privately
  (RegistryConfig.TaskStore -> r.taskStore); needs an exported accessor.
- TUI restore: loadSessionImpl already honors auto_resume (arms 1-2 pass
  semantics). Missing arm 3: interrupted-awareness for stale non-terminal
  tasks after TasksUpdateMsg. Needs TasksModel-side rebuild + system message.

## Task log

- [x] Read leaf doc + both Go gates
- [x] Re-checked gates fresh (RED reasons recorded above)
- [ ] TUI stub harness fix (short socket dir)
- [ ] T2 queue.task_states implementation
- [ ] T1 interrupted-awareness implementation
- [ ] T7 restore e2e extension + manifest rows
- [ ] Verify: both gates GREEN, manifest completeness GREEN, go build
- [ ] 03-REPORT.md

## Restart 2 (2026-11-20, this child)
- Disk freed (32G avail); gates re-run fresh: same RED reasons as restart 1.
- TestQueueStatusParked: handleStatus has NO task_states key (both arms).
- TestRestorePolicyAligned: all 3 subtests die in newGateCallStub bind
  (sun_path > 104) — harness fixed (short /tmp/mts<pid><seq> dir in
  command_handler_plan_test.go); arms 1-2 pass pre-existing; arm 3 needs
  interrupted-awareness.
- Plan now: T2 task_states -> T1 interrupted -> queue-view badge parity ->
  T7 e2e restore flows + manifest rows.
- [x] TUI stub harness fix (short socket dir; sun_path under 104)
- [x] T2 queue.task_states: Registry.TaskStore() accessor (registry.go),
  agent.UnwrapThreadConversationID export (queue_resolve.go),
  rpc/queue.go handleStatus + sessionParkedTaskStates (bounded
  GetTasksForSession on thread-unwrapped base; terminal states skipped;
  never errors the poll). TestQueueStatusParked + WithActiveQueue GREEN.
- [x] T1 interrupted-awareness: models/tasks.go ArmRestoreAwareness/
  RestoreAwareness sink/consumeRestoreAwareness (one-shot, cleared on
  session switch); app.go armRestoreAwareness wired into both resume
  paths of loadSessionImpl + SessionLoadedMsg batch fires
  RestoreFetchCmd + top-level TasksUpdateMsg case (consumed in any view).
  TestRestorePolicyAligned GREEN (all 3 arms). Both Go gates GREEN.
- [x] T2 client-side parity: types.QueueStatusResponse + TaskStates
  []TaskStateRow; chat.go parked badge + hasQueueItems.
- !! INCIDENT 17:10: a sibling's `git stash` swept my uncommitted edits
  (plus 3 other siblings' WIP) into stash@{0} (f2a50e91). Recovered by
  re-applying ONLY leaf-03 hunks from the stash commit (git show
  f2a50e91:<file> for my-exclusive files; filtered patch for app.go);
  stash left INTACT for the siblings. Did not pop.
- [x] T1 app-side: armRestoreAwareness + restoreCmd batch +
  top-level models.TasksUpdateMsg route + ChatTranscriptExcerpt
  accessor (ASCII-clean diff).
- [ ] T7: restore_flow_test.go written (tui-restore-01/02/03); run + manifest rows.
- [x] T7 restore e2e: e2e/suites/tui-flows/restore_flow_test.go
  (tui-restore-01 parked task_states on live daemon; -02 auto_resume
  most-recent + explicit target override; -03 interrupted marker in
  transcript on resume). 3/3 GREEN. manifest.json: 3 additive scenario
  rows + generated=2026-11-20; completeness suite PASS.
- [x] Verify: TestRestorePolicyAligned ok; TestQueueStatusParked(+Active)
  ok; manifest -tags e2e ok; go build ./... ok; tui failure-set diff vs
  HEAD: strict subset (zero new failures; TestTaskApprove_RPC failure
  pre-exists at base b161d027).
- Gates restore_policy_test.go / queue_status_parked_test.go untouched
  (git status clean on both). No commit. gofmt + ASCII clean on diff.
