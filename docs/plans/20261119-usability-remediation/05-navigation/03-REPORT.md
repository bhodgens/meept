# 05-navigation / 03-restore-queue-visibility — COMPLETION REPORT

Restart run (prior child died disk-full mid-implementation; this child
recovered the plan and finished). Worktree:
.hermes/worktrees/usability-remediation, base b161d027 + siblings'
commits (HEAD 00499a74 during the run). NOT committed, per leaf rules.

## 1. Files touched (leaf-03 changes)

Product:
- internal/agent/queue_resolve.go — exported UnwrapThreadConversationID
  (thread-scoped conv id -> session-level base for store joins).
- internal/agent/registry.go — exported (*AgentRegistry).TaskStore()
  read accessor (nil-safe when unwired).
- internal/rpc/queue.go — T2: handleStatus now answers task_states
  []{task_id,state,name} on BOTH arms (no-active-queue and
  active-queue), via sessionParkedTaskStates: bounded
  GetTasksForSession on the thread-unwrapped id, terminal states
  skipped, store/lookup failure degrades to an empty list (never
  errors the poll, never scans the full task table, never crosses
  sessions).
- internal/tui/command_handler_plan_test.go (harness only, not a gate) —
  newGateCallStub socket dir moved out of t.TempDir() (macOS 104-byte
  sun_path limit broke all TestRestorePolicyAligned arms, and the
  sibling's committed TestPlanSlashForwards* gate too). Short
  /tmp/mts<pid><seq> dir; also unblocks the other leaf's gates.
- internal/tui/models/tasks.go — T1: RestoreAwareness type +
  RestoreAwarenessSink; ArmRestoreAwareness (one-shot),
  RestoreFetchCmd, consumeRestoreAwareness (non-terminal states,
  session-scoped via LinkedSessions when attributed); SetCurrentSession
  disarms only on a real session switch.
- internal/tui/app.go — T1: loadSessionImpl arms awareness on both
  resume paths (target-session + auto_resume); SessionLoadedMsg batch
  fires the restore tasks fetch for resumed (not new) sessions;
  top-level models.TasksUpdateMsg route consumes the fetch from any
  view; armRestoreAwareness sink renders one
  "[restore] interrupted work on this session: task ... was still
  <state> ..." system message per stale task; ChatTranscriptExcerpt
  read accessor for headless assertions.
- internal/tui/types/types.go — QueueStatusResponse.TaskStates
  []TaskStateRow (json task_states; older daemons omit the key).
- internal/tui/models/chat.go — renderQueueIndicator "parked: N" badge
  + hasQueueItems counts TaskStates (TUI render of task_states).

E2e + manifest:
- e2e/suites/tui-flows/restore_flow_test.go (NEW) — tui-restore-01
  (queue_status task_states on a live daemon, session-scoped),
  -02 (auto_resume=true boots onto session.get_most_recent's answer;
  explicit --session target overrides; client.json5 policy file
  honored — JSON5 quoted keys), -03 (resumed session with a stale
  executing task renders the interrupted marker into the transcript).
  The -02 same-second timestamp tie needed a >1s gap between creates
  (RFC3339 second granularity).
- e2e/manifest.json — additive: 3 scenario rows (tui-restore-01/02/03,
  suite tui-flows, diff M, real paths) + generated=2026-11-20.

Docs: 03-PROGRESS.md (appended per task), this report.

## 2. Verify output (all run against the live tree)

- go test ./internal/tui/ -run TestRestorePolicyAligned -count=1
  -> ok (3/3 subtests; harness fix + T1)
- go test ./internal/rpc/ -run TestQueueStatusParked -count=1
  -> ok (both parked tests)
- go test -tags e2e ./e2e/suites/tui-flows/ -run
  'TestQueueStatusSurfacesParkedTask|TestRestoreRelaunchesMostRecentSession|TestRestoreMarksInterruptedWorkOnResume'
  -> ok (3/3)
- go test -tags e2e ./e2e/suites/manifest/ -count=1 -> ok
  (completeness PASS incl. new scenario rows reachable via path_map)
- go build ./... -> ok
- Full ./internal/tui failure set diffed vs HEAD worktree
  (00499a74 copy): worktree set is a STRICT SUBSET of HEAD's — zero
  new failures; the 5 remaining failures (evolver/mode-badge/
  approval-card/plans-gate/restore-policy-adjacent) are other leaves'
  pending RED gates pre-existing at base.
- internal/rpc full-package run has TestTaskApprove_RPC failing —
  reproduced IDENTICALLY at base b161d027 in a scratch worktree
  ("rejection service not available"); pre-existing, not leaf-03.
- Gates untouched: git status clean on restore_policy_test.go +
  queue_status_parked_test.go; gofmt clean; diff is ASCII.

## 3. Deviations / notes

1. GUI half of T1 (home_screen/sidebar autoResume consumption) is NOT
   in this run: ui/flutter_ui is owned by the GUI sibling (02 landed
   00499a74; Dart-side restore_test.dart gate is that sibling's
   surface). TUI-side alignment + queue.status + e2e/manifest were
   this child's remit.
2. INCIDENT: at 17:10 a sibling's `git stash -u` swept this leaf's
   uncommitted work (plus 3 other siblings' WIP) into stash@{0}
   (merge commit f2a50e91). Recovered surgically WITHOUT popping:
   my exclusive files restored from `git show f2a50e91:<path>`, my
   app.go hunks via filtered patch (the sibling's chat_turn comment
   hunk re-applied itself from their live write; the sibling's
   chat.go correlation-field hunks were preserved — their file was
   also in the stash and restored whole). stash@{0} left INTACT for
   the orchestrator/other children to recover their remaining files
   (internal/comm/http/server.go, internal/tui/command_handler.go,
   models/turn.go, Dart files — some re-landed on their own).
   The orchestrator should know stash@{0} holds live sibling WIP.
3. task_states empty-list semantics: present-but-empty (older daemons
   omit the key entirely; TUI decoder tolerates both). Rows are
   {task_id,state,name} verbatim per the contract.
4. chat.queue_status WS-event payload path (bus EventQueueStatus
   decoder) unchanged — depths only; task_states rides the RPC poll
   the queue views use (the leaf contract pins the RPC payload).
