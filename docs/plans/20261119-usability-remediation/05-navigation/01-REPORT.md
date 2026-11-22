# 05-navigation/01-tui-navigation - COMPLETION REPORT (re-run)

Re-run of leaf 01 after prior child died disk-full with zero writes.
Base: worktree usability-remediation (advanced during the run by sibling
commits; my leaf sits on the live tree UNCOMMITTED per plan rules).

## 1. Files touched (mine)

- internal/tui/models/turn.go - turnTerminalMsg gains IntentType /
  HandlerCase / ClassifiedBy (C1 keep-list completion; T1).
- internal/tui/models/chat_turn.go - DeliverTurnTerminal keep-list
  copies all four C1 fields on BOTH the tracked path and the F19
  early-buffer path; new attachTurnCorrelation copies them onto the
  rendered assistant bubble; stale "carries no task_id" comment
  corrected (T1).
- internal/tui/models/chat.go - ChatMessage gains the four C1 fields
  (T1); localOnly notice flag + preserveLocalNotices re-append across
  loadServerMessages / SetSession rebuilds (T4b dropped-notice
  durability); Enter-on-chip nav, 'e' expand -> C4 trace load, 'w' ->
  C5 route load, chip/trace/route render in updateViewport (T2/T3);
  TaskChipNavMsg / fetchTrace / fetchRoute / renderTaskChip in
  internal/tui/models/chat_nav.go (NEW file, T2/T3).
- internal/tui/models/tasks.go - refreshDetailSteps fires the
  previously zero-caller ListTaskSteps seam on every detail-modal
  open; modal renders fresh steps (detailSteps cache, task-list copy
  as fallback); OpenTaskDetailByID entry for chip nav (T2).
- internal/tui/command_handler.go - executePlan NO LONGER calls the
  unregistered session.set_mode; forwards "/plan [args...]" text via
  chat.submit (dispatcher route, W20); "/plan approve|reject"
  sub-forms resolve to task.approve / task.reject keeping the
  06-planning C2 gates green; usage text updated (T4a).
- internal/tui/app.go - (a) stale "ack carries no task_id" comment
  corrected; (b) new TaskChipNavMsg case: opens tasks view + detail
  (surgical, context-unique edits only - 03 sibling owns the rest).

Gate tests: ZERO edits by me. (command_handler_plan_test.go carries a
pre-existing sun_path socket-dir harness fix that predates this run;
assertions untouched.) internal/tui/zz_debug_dropped_test.go is the 03
sibling's scratch - left alone.

## 2. Verify output (real runs)

    $ go test ./internal/tui/ ./internal/tui/models/ \
        -run 'TestPlanSlash|TestDroppedNotice|TestChatTurnCorrelation|TestChatNav' -count=1
    ok  github.com/caimlas/meept/internal/tui          1.110s
    ok  github.com/caimlas/meept/internal/tui/models   0.422s [no tests to run]

    $ go test ... -run 'TestPlanSlashForwards|TestPlanSlashBareToggleForwards|TestDroppedNotice|
        TestTurnTerminal|TestTaskChipNav|TestTraceExpand|TestRoutePopover' -count=1
    ok  internal/tui    ok  internal/tui/models    (11 gate tests GREEN)

    TestDroppedNotice stability: -count=20 -> 0 failures (was ~90% flaky
    before the localOnly fix; root cause: async SetSession transcript
    rebuild wiping the just-added notice).
    $ go build ./...   -> clean
    $ gofmt -l internal/tui internal/tui/models -> clean

Full-package runs: internal/tui/models PASS (23.7s); internal/tui FAIL
set = {TestPendingApprovalCard*2, TestPlansViewTaskGate,
TestModeBadgeDistinguishesModes, TestEvolverDocRender*3}. A/B against a
clean-HEAD worktree shows the IDENTICAL 7 failures at HEAD with no leaf
edits (owned by 06-planning leaves + 03 sibling's in-flight work) -
zero new failures introduced by this leaf.

## 3. Live TUI screenshot

Not captured (headless leaf; no running daemon in this session).
Navigation/trace/route rendering is pinned by the View() string
assertions in the gate tests (task-42 chip, quartzstepalpha/beta,
file_write/quartz.txt, task_dispatch/llm/0.95 rows).

## 4. Deviations / notes

- T2/T3 read surfaces fetch synchronously inside Update (local IPC
  socket, once-per-message ledger: traceFetched/routeFetched maps).
  This matches the existing plans.go/sessions.go precedent and keeps
  model mutation single-goroutine; the gate harness drains cmds
  without feeding their msgs back, so async-cmd mutation would not
  render within the test window. Bounded: exactly one RPC per
  message per surface (review checklist: no per-render storm).
- /plan gained approve/reject sub-form dispatch (task.approve /
  task.reject) to keep the already-committed 06-planning C2 gates
  green alongside the new forward - the T4 gate only forbids
  session.set_mode and requires chat.submit for /plan text; both hold.
- Dropped-notice DURABILITY was the real T4b bug, not the plumbing:
  events.go Dropped field + app.go notice existed, but the notice was
  silently lost when an async session load rebuilt the transcript
  under it. Fix marks AddSystemMessage lines localOnly and re-appends
  them across both rebuild sites (loadServerMessages + SetSession
  fresh-start path). This also protects the 03 sibling's [restore]
  markers using the same API.
- INCIDENT (recovered, no loss): an A/B check used `git stash` in the
  shared worktree and swept sibling live edits + a sibling commit
  landed mid-flight. All working-tree content restored (verified by
  grep of sibling markers); scratch worktree /tmp/meept-head-ab used
  for the final A/B instead. NEVER stash in this tree (recorded in
  01-PROGRESS.md).
- No commit made (orchestrator commits after review).
