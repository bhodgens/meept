# 06-planning/02 GUI Approval + Seal -- COMPLETION REPORT

## 1. Files touched
- `ui/flutter_ui/lib/services/sdk_client.dart` (MODIFIED)
  - `approveTask(id)` / `rejectTask(id, {reason})` -- C2 HTTP twins
    (POST /api/v1/tasks/{id}/approve|reject; reason forwarded in body).
  - `getDraft(id)` / `sealTask(id)` -- plan.draft/plan.seal HTTP twins
    (GET /api/v1/tasks/{id}/draft, POST /api/v1/tasks/{id}/seal).
- `ui/flutter_ui/lib/providers/task_provider.dart` (MODIFIED)
  - `TaskNotifier.approveTask` / `rejectTask` -- forward to the SDK
    twins, then reload the list so the rejected terminal state lands in
    the UI. Deliberately separate from PlanNotifier's plans.db gate.
- `ui/flutter_ui/lib/features/plans/plans_tab.dart` (MODIFIED)
  - initState loads taskProvider alongside planProvider.
  - `_TaskGateSection`: strategic-gate cards render ABOVE the plans.db
    detail pane (two gates visually + programmatically distinct).
  - `_ApprovalTaskCard` (T1): awaiting_approval task card with planned
    steps (_StepSummary from task.steps / total_jobs) + approve/reject;
    reject opens a reason dialog whose only action is rejectTask.
  - `_SealTaskCard` (T2): renders the compiler draft markdown inline +
    explicit 'seal' button; seal problems render inline per
    {line,message} and the draft stays a draft; sealed result shows
    hash/mode. No auto-seal path exists.
  - `_WaitStateCard` (T3): hostile_skipped / await_seal / compile_failed
    get actionable copy instead of a gray badge.
- `internal/comm/http/plan_seal.go` (NEW) -- handleTaskDraft /
  handleTaskSeal dispatch over rpcCall to plan.draft (get shape) /
  plan.seal; error mapping: "no draft" -> 404, "not available" -> 503,
  compile failure -> 400; compile PROBLEMS pass through as 200 results
  (matching internal/rpc/plan_seal.go's result-not-error contract).
- `internal/comm/http/server.go` (MODIFIED) -- the two routes registered
  beside the C2 approve/reject twins.
- `docs/plans/20261119-usability-remediation/06-planning/02-PROGRESS.md` (NEW).

## 2. Verify output
- Gate files: `flutter test test/features/approval_card_test.dart
  test/features/seal_surface_test.dart` -> **All tests passed (+10)**.
  5 approval-card gates + 5 seal-surface gates GREEN; gate files
  untouched (diff empty).
- Zero-new-failures bar: full `flutter test` -> +833 -3. All 3
  remainders are pre-existing base failures (chat_submit_flow parked
  status; restore_test x2 = sibling 05/03 T6 territory, explicitly not
  mine). Base failure count 13 -> 3 = exactly this leaf's 10 closed,
  nothing new introduced.
- `go test -p 2 ./internal/comm/http/` -> ok (suite green with the new
  routes; the existing task_approval twins' tests unaffected).

## 3. Build log
- `flutter build web` -> exit 0 ("✓ Built build/web", 30.8s compile).
- `go build ./internal/comm/...` -> clean. NOTE: `go vet` in this
  worktree reports a pre-existing break in `internal/tui/models`
  (chat_turn.go attachTurnCorrelation -- sibling 05-navigation's live
  edit); it does NOT block internal/comm/http (import cycle absent) and
  was confirmed unrelated to this leaf.
- `dart format` clean on all 3 touched Dart files; `gofmt` clean on
  both Go files. No ChatMessage-model changes => no build_runner regen
  needed (Task already carries `metadata` + `steps`).

## 4. Deviations
- T3 (wait_states_test.dart) has NO committed gate file (leaf doc
  marked it "(add)" but 00T shipped only the two files); implemented
  anyway per the leaf Goal, kept minimal and data-driven so it cannot
  affect other tests. Did NOT author a new gate test (tests-first rule
  says the gates were already set by 00T).
- Draft markdown renders from task metadata (`plan_draft.markdown`,
  the shape the gate fixture seeds) rather than forcing a getDraft
  round-trip at render time; `getDraft` is nonetheless implemented +
  pinned by the wire gate, and available for a future refresh button.
- The reject dialog's TextField+FilledButton('reject') sit in the
  dialog CONTENT (via `_RejectReasonField`, a StatefulWidget owning the
  controller) instead of AlertDialog `actions`, because the gate taps
  `confirm.last` after entry and the whenComplete-dispose pattern raced
  the exit animation ("controller used after being disposed"). Same
  affordances, no gate weakening.
- No commit made, per instruction.
