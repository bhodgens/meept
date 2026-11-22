# 02-ROUTING TEST-GATE - Completion Report (00T)

Branch: 02-routing. Worktree: .hermes/worktrees/usability-remediation (HEAD 76b1c0f7).
Tests ONLY. No implementation code. No commit, no git add. Every gate test
fails RED for a missing-implementation reason; positive-control / over-veto
guards pass today by design. Matrix F6-F8 covered.

## 1. Files touched

Created (11 test files + 1 fixture):

| File | Task | Pins |
|------|------|------|
| internal/agent/testdata/usability_prompts.json | T1/T7 | FROZEN p1 (4 turns) + p6 (3 turns) VERBATIM from /tmp/usability-rig/p1_naive.py + p6_control.py; byte-match verified programmatically |
| internal/agent/dispatcher_veto_test.go | T1 | TestGitVetoShapeIndependent_* (5 tests): frozen edit prompt mutation-discipline probe, edit-shape sweep (question/declarative/polite), genuine-git over-veto guard, F41 recall priority, disabled escape hatch |
| internal/agent/dispatcher_notice_test.go | T2 | TestNoticePopulated_{EmptyResponse,Unavailable,Cooldown}Lane (ClassificationNotice on DispatchResult from llm.ClassificationUserGuidance); TestHintCapabilityFit_ResearchStepCodeHint + _CodeStepUnchanged (roster ground truth: researcher HAS web_fetch, coder does NOT) |
| internal/metrics/dispatch_log_test.go | T3 | TestTraceTurnID_{SchemaHasTurnID,RoundtripWithTurnID,DispatcherRecordCarriesTurnID}: turn_id column + populated input_summary, idempotent reopen, legacy COALESCE |
| internal/rpc/dispatch_trace_test.go | T3 | TestDualKeyTrace_{PrimarySessionID,ConversationIDKeepsWorking,ThreadIDResolves,UnknownIDEmptyNotError}; TestTraceTurnID_RowsCarryTurnIDAndInput (real Server.CallMethod + real metrics store) |
| internal/agent/dispatcher_continuation_test.go | T4 | TestDraftRefinementRoute_{FollowupRefinesSameTask,NewWorkStillCreatesTask}; TestThreadResolveOnce_FullInputTopic (topic keyword past char 100; counting thread store; ThreadConversationID via reflection so the gate compiles pre-field) |
| internal/agent/queue_recovery_test.go | T5 | TestBootFollowupReenqueue_{ContentSurvivesBoot,NoInboxKeepsRows}: full content re-enqueued to session inbox, delete-only-after-injection, crash-window idempotence |
| internal/agent/tactical_retry_test.go | T6 | TestBlockedStepRetry_{TickerSchedulesBlockedSteps,ProgressSurfacesBlockedState}; TestNoSingletonDirMutation_{FailedSessionLoopNeverMutatesShared,ConcurrentSessionsIsolated} |
| internal/daemon/stepjob_dir_test.go | T6 | TestStepJobDirBinding_{FallbackBindsSessionDir,ThreadScopedPayloadHintResolves,NoDirAnywhereRefusesActionably} (GetForTask-error fallback via unknown agent id; turnContextObserver seam) |
| e2e/suites/dispatcher/frozen_prompts_test.go | T7 | dispatcher-08 (frozen p1 edit + scripted git costume), dispatcher-09 (p6 positive control) |
| e2e/suites/dispatch-trace/dispatch_trace_test.go | T7 | dispatch-trace-01..03 (dual-key, turn join + input, HTTP twin) - NEW suite |
| e2e/suites/plan-lifecycle/continuation_gate_test.go | T7 | plan-lifecycle-04 (parked plan-compiler draft refinement continues SAME task) |

Modified: e2e/manifest.json - dispatch-trace suite row (implemented), 6 new
scenarios (dispatcher-08/09, dispatch-trace-01..03, plan-lifecycle-04),
path_map keys (e2e/suites/dispatch-trace, internal/rpc/, internal/metrics/,
internal/agent/, internal/comm/http/, exact-file keys for dispatch_trace.go /
store.go / plan_draft.go). Manifest completeness suite PASSES
(`go test -tags e2e ./e2e/suites/manifest/ -count=1` -> ok), including the
narrow-catchall scenario-derivation pin. (Sibling gate waves had already
modified the manifest before this leaf; those edits were preserved.)

## 2. RED output per task (verify commands, last lines)

- T1 `go test ./internal/agent/ -run TestGitVetoShapeIndependent -count=1`
  -> `FAIL github.com/caimlas/meept/internal/agent 0.900s`.
  RED reason: canned git @0.91 verdict SURVIVES on every frozen/synthetic
  edit shape (method "llm", agent committer) - the veto conjunct
  hasLeadingImperativeVerb + verb vocabulary never fires. Mutation
  discipline satisfied: TestGitVetoShapeIndependent_FrozenP1EditPrompt logs
  `current-behavior probe: git verdict survived frozen p1 edit prompt = true`.
  Guards PASS today: genuine-git kept (4 shapes), recall priority
  (platform_recall_arbitration), disabled hatch.
- T2 `go test ./internal/agent/ -run 'TestNoticePopulated|TestHintCapabilityFit' -count=1`
  -> `FAIL github.com/caimlas/meept/internal/agent 1.264s`.
  RED reason: `ClassificationNotice is empty` on llm_empty_fallback_chat,
  heuristic_fallback (classifier 400), and cooldown lanes (zero writers);
  `research-shaped step kept tool_hint="code" with no routing.warning`.
  Guard PASS: code step unchanged, no warning.
- T3 metrics `go test ./internal/metrics/ -run TestTraceTurnID -count=1`
  -> `FAIL github.com/caimlas/meept/internal/metrics 0.382s`
  (`dispatch_log has no turn_id column`; `input_summary came back empty`;
  `no such column: turn_id`).
  T3 rpc `go test ./internal/rpc/ -run 'TestDualKeyTrace|TestTraceTurnID' -count=1`
  -> FAIL: primary-id query `count:0 entries:<nil>` while conv-id returns 1;
  thread id 0 rows; wire rows carry no turn_id, empty input_summary.
  Guards PASS: conv-id lookup, unknown-id empty-not-error.
  NOTE: the rpc package test build is blocked by the SIBLING 01-safety gate
  file task_approval_reject_test.go (compile-RED on RejectFunc, its own
  documented RED). It was moved aside to /tmp/02-routing-gate-stash/ for the
  RED run and RESTORED byte-identical afterwards; this leaf did not touch it.
- T4 `go test ./internal/agent/ -run 'TestDraftRefinementRoute|TestThreadResolveOnce' -count=1`
  -> `FAIL github.com/caimlas/meept/internal/agent 1.294s`.
  RED reason: follow-up minted task-...-0002 ("make it five languages
  instead"), parked draft version stayed 1; router consulted 4 extra times
  in RouteToAgent; DispatchResult carries no ThreadConversationID.
  Guard PASS: new-work command still creates a task, draft untouched.
- T5 `go test ./internal/agent/ -run TestBootFollowupReenqueue -count=1`
  -> `FAIL github.com/caimlas/meept/internal/agent 0.769s`.
  RED reason: `session inbox holds 0 follow-ups after boot, want 2`;
  `rows after boot with no inbox = 0, want 1` (loadAndClear deletes content,
  count event only).
- T6 agent `go test ./internal/agent/ -run 'TestBlockedStepRetry|TestNoSingletonDirMutation' -count=1 -timeout 120s`
  -> FAIL: blocked step state "ready" 20s after slots freed (no ticker);
  task.progress never surfaces blocked_by_semaphore; singleton workingDir
  mutated to "/session-a/project" (race-risk branch).
  T6 daemon `go test ./internal/daemon/ -run TestStepJobDirBinding -count=1`
  -> `FAIL github.com/caimlas/meept/internal/daemon 0.962s`: fallback turn
  dir "" (want session project); thread-scoped payload hint unresolved;
  no-dir job reaches the loop instead of ErrNoWorkingDir refusal.
  Guard PASS: ConcurrentSessionsIsolated (manager-wired path is correct today).
- T7 `go test -tags e2e ./e2e/suites/dispatch-trace/... -count=1`
  -> `FAIL github.com/caimlas/meept/e2e/suites/dispatch-trace 19.914s`:
  01 primary id 0 rows vs conv 1; 02 no row carries turn_id (rows show
  TurnID:"" InputSummary:""); 03 HTTP 404 (route absent).
  dispatcher ext: dispatcher-08 RED (`hello.txt still says "hello world"
  ... the edit never executed (git lane swallowed it)`); dispatcher-09
  PASS (positive control - the probe's wording-sensitivity hypothesis).
  plan-lifecycle ext: plan-lifecycle-04 RED (refinement minted
  task-...-0002; draft version stayed 1; trace by primary id 0 rows).
  Fixture drives the REAL compiler pipeline (daemon log: "Plan draft saved
  ... version=1", "brainstorm draft seeded").
  Manifest completeness: `go test -tags e2e ./e2e/suites/manifest/ -count=1` -> ok.

## 3. Deviations

- ClassificationNotice lives on DispatchResult (dispatcher.go:231), not
  Intent; T2 notice pins drive ClassifyAndRoute and assert the result field
  (the handler already reads it at handler.go:1049/1258 - write side is the
  gap, exactly as the leaf states).
- T3 turn_id read assertions use raw SQL row maps + optional-setter
  interface assertions so the gate COMPILES today and flips GREEN with zero
  gate edits when the column/field/setters land (per spec-first Go rule:
  no direct reference to not-yet-existing symbols).
- T4 ThreadConversationID read via reflection (same rule).
- T5/T6 future seams (boot-recovery inbox resolver, SetStepRetryInterval)
  wired via interface assertions; missing seam is part of the RED state.
  T6 retry test waits 20s (> default 15s interval) so it holds without the
  fast seam.
- T7 plan-lifecycle fixture: plans.mode=always routes to the plan-FILE path
  (no strategic draft), so the parked draft is produced via scripted code
  verdict (IntentCode.SuggestedMode()=="plan") + plans.plan_compiler_enabled
  overlay; both turns ride fire-and-forget SubmitChatHTTP because a parked
  draft turn never finalizes (CLI await would hit its ceiling).
- T7 dispatch-trace RPC probes send both identity spellings under the
  session_id param (the handler's only param today); C5's conversation_id
  param spelling is additive for 02-routing/02.
- rpc RED run required temporarily moving the sibling 01-safety compile-RED
  gate file aside (restored; see T3 note). No existing test was weakened.

## 4. Conventions verified

gofmt clean (all 11 files); ASCII-only (byte scan); no bare `_ = ` added
lines; frozen prompts VERBATIM (programmatic byte-match vs probes);
`go build ./...` OK; `go vet` clean on touched packages (except the
sibling's pre-existing compile-RED file); no commit / no git add.