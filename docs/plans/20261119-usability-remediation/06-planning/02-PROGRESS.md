# 06-planning/02 GUI Approval + Seal -- PROGRESS

Worktree: .hermes/worktrees/usability-remediation (base HEAD 00499a74).
Gate tests (committed f326aef8): test/features/approval_card_test.dart (5),
test/features/seal_surface_test.dart (5). NOT weakened, NOT touched.

## Task log (all complete)

### [x] T1 wire -- SDK C2 task twins
- sdk_client.dart: `approveTask(id)` -> POST /api/v1/tasks/{id}/approve;
  `rejectTask(id, {reason})` -> POST /api/v1/tasks/{id}/reject with
  {"reason": ...} body. Mirrors internal/comm/http/task_approval.go.
  Distinct from approvePlan/rejectPlan (plans.db gate) -- no conflation.

### [x] T2 wire -- SDK draft/seal twins + daemon HTTP routes
- sdk_client.dart: `getDraft(id)` -> GET /api/v1/tasks/{id}/draft;
  `sealTask(id)` -> POST /api/v1/tasks/{id}/seal.
- internal/comm/http/plan_seal.go (NEW): handleTaskDraft/handleTaskSeal
  dispatch over the rpcCall seam to plan.draft (get shape) / plan.seal,
  mirroring internal/rpc/plan_seal.go semantics: problems = 200 RESULT,
  "no draft" = 404, service unavailable = 503, compile failure = 400.
  Seal is ONLY ever an explicit POST.
- internal/comm/http/server.go: routes registered beside the C2 twins.

### [x] T1 widget -- approval card
- PlansTab initState now also loads taskProvider.loadTasks().
- New `_TaskGateSection` above the plans.db detail pane: renders one
  `_ApprovalTaskCard` per awaiting_approval TASK (planned steps via
  task.steps / total_jobs), with approve/reject wired to
  TaskNotifier.approveTask/rejectTask -> C2 HTTP twins.
- Reject opens a reason dialog (_RejectReasonField owns the controller;
  a whenComplete-dispose raced the dialog exit animation -- fixed);
  confirm calls rejectTask ONLY (load-bearing: never approve).
- Rejected terminal state surfaces via the post-call loadTasks refresh.

### [x] T2 widget -- seal surface
- `_SealTaskCard` renders the compiler draft markdown (metadata
  plan_draft.markdown) in a scrollable SelectableText + a 'seal' button.
- Tap seal -> sealTask; {status:"problems"} renders each {line,message}
  inline ("line N: message", the draft stays a draft); {status:"sealed"}
  shows hash/mode. NEVER auto-seals -- no seal path runs at render.

### [x] T3 widget -- wait states
- `_WaitStateCard` for hostile_skipped / await_seal / compile_failed
  task statuses: actionable copy ("hostile review skipped - configure a
  critique model...", "awaiting your seal..."), never silent gray.
  (No gate file pins T3; implemented per leaf doc, low risk.)

## Verification (final)
- flutter test test/features/approval_card_test.dart test/features/seal_surface_test.dart
  -> +10: All tests passed (10/10 gates GREEN).
- Full `flutter test`: +833 -3. The 3 remainders are pre-existing base
  failures owned elsewhere: chat_submit_flow parked-status (base) +
  restore_test x2 (sibling leaf 05/03 T6 territory). Base had 13 known
  failures; this leaf's 10 are closed with zero new elsewhere.
- flutter build web: exit 0.
- go build ./internal/comm/... + go test -p 2 ./internal/comm/http/: ok
  (88s, suite green with the two new routes registered).
- gofmt clean on both touched Go files; dart format clean on all three
  touched Dart files.
- No freezed regen needed (Task model already carries metadata/steps).

## NOT COMMITTED (per leaf doc; orchestrator commits after review).
