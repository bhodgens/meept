# 05-navigation/01-tui-navigation - PROGRESS (re-run; prior child died disk-full, zero writes)

Worktree HEAD: 5c9c6c4d (leaf tree b161d027+). Gates committed f326aef8.

## Initial state (measured)
- models pkg (chat_turn_correlation_test.go + chat_nav_test.go): ALL 8 tests RED
  - T1: turnTerminalMsg missing IntentType/HandlerCase/ClassifiedBy;
    ChatMessage missing TaskID/IntentType/HandlerCase/ClassifiedBy
  - T2: no task chip in View; Enter emits no nav msg; detail modal never
    calls ListTaskSteps; fetched steps not rendered
  - T3: no include_tool_calls round-trip; no dispatch_trace popover
- tui pkg: TestPlanSlashForwards RED (session.set_mode), TestPlanSlashBareToggleForwards RED,
  TestDroppedNotice FLAKY (passes alone sometimes; fails with siblings):
    Dropped plumbing EXISTS (events.go:259 + app.go:2051) but the notice is
    wiped by the async SetSession/SessionMessagesLoadedMsg transcript
    replacement (chat.go SetSession clears m.messages; loadServerMessages
    replaces). Root cause pinned: local-only system notices silently lost.
    Scratch debug confirmed: transcript=[] with lastDropped=3.

## Plan / task order
1. T1 correlation fields (turn.go, chat_turn.go keep-list, chat.go ChatMessage + attach)
2. T4a /plan forward (command_handler.go executePlan)
3. T4b dropped-notice durability (preserve localOnly system notices across
   SetSession/loadServerMessages replacement)
4. T2 chip render + Enter nav msg + tasks detail fresh steps
5. T3 trace expand (include_tool_calls) + route popover (dispatch_trace)
6. Remove stale "carries no task_id" comments (chat_turn.go:462, app.go:1769)
7. Verify: targeted gate run + go build ./... ; gofmt clean; NO COMMIT

## Status log
- [x] leaf doc read; gates run once (RED mapped above)
- [x] T1 GREEN (turn.go + chat_turn.go keep-list + ChatMessage fields +
      attachTurnCorrelation; TestTurnTerminal* 4/4 ok)
- [x] T4a GREEN (executePlan forwards chat.submit, approve/reject
      subforms -> task.approve/task.reject for 06-planning C2 gates;
      TestPlanSlash* 2/2 ok, approval-card approve/reject gates ok)
- [x] T4b GREEN (localOnly notice durability: AddSystemMessage marks,
      preserveLocalNotices re-appends across loadServerMessages +
      SetSession fresh-start; TestDroppedNotice 20/20 count=20)
- [x] stale "carries no task_id" comments removed (chat_turn.go, app.go)
- [x] T2 GREEN (chip render + Enter -> TaskChipNavMsg + App case ->
      OpenTaskDetailByID + refreshDetailSteps via ListTaskSteps;
      TestTaskChipNav* 2/2)
- [x] T3 GREEN (fetchTrace include_tool_calls + fetchRoute
      dispatch_trace, sync-on-Update once-per-msg;
      TestTraceExpand + TestRoutePopover)
- [x] final verify: 11 gate tests ok -count=1; TestDroppedNotice
      -count=20 stable; go build ./... clean; gofmt clean;
      full-suite A/B vs clean HEAD worktree: failure set byte-identical
      (7 pre-existing, 06-planning/03-owned), zero new.
- [x] 01-REPORT.md written. NO COMMIT.

## INCIDENT: sibling collision during stash
While A/B-checking the approval-card gates I ran `git stash`, which
swept SIBLING (05-navigation/03) live edits AND caught a sibling commit
(00499a74 GUI leaf). Recovery: git checkout of my files from the stash,
re-applied the app.go comment fix surgically, `git reset` to unstage the
sibling's staged files (working tree content preserved - NOTHING lost;
verified via grep of sibling markers armRestoreAwareness/queue etc).
LESSON: never git stash in this shared worktree; A/B via
`git show HEAD:path > /tmp` scratch builds instead.

## Decisions
- Detail fetch on Enter is SYNCHRONOUS through TasksRPCClient.ListTaskSteps
  (zero-caller seam; mock counts calls in Update return window; once per
  user Enter = bounded, no per-render RPC storm).
- TaskChipNavMsg: chat model emits it on Enter over a TaskID-bearing reply;
  App opens tasks view + detail (surgical app.go case).
- Trace/popover cmds mutate the shared *ChatModel then emit a refresh msg
  (harness drains cmds without feeding msgs back; production loop re-renders).
- Sibling 03 owns events.go/app.go restore surface: app.go edits limited to
  (a) stale comment reword (gate-pinned), (b) additive TaskChipNavMsg case +
  dropped-notice durability if not fixable inside models.
