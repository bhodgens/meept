# 02-recall-fidelity -- Completion Report

Leaf: docs/plans/20261119-usability-remediation/03-context/02-recall-fidelity.md
Worktree: .hermes/worktrees/usability-remediation (HEAD f326aef8). NOT committed, NOT git-added.
Gate tests (committed 89e996e2) untouched: recall_state_gate_test.go (T4), recall_fact_test.go (T5).
Pre-existing recall_answer_test.go untouched.

## 1. Files touched

Modified:
- internal/agent/recall_answer.go
  - T1 (F-ORCH-16a): state switch gains explicit failed/cancelled/rejected
    arms -- "The task failed / was cancelled / was rejected; the work did
    not complete." The stored result line above already carries the
    reason. completed/approved unchanged; default keeps the in-progress
    sentence for genuinely non-terminal tasks (both gate controls pass).
  - T2 (F-ORCH-16b/F-NEW-7): every handled recall answer appends
    renderRecallTaskSet -- the session's OTHER tasks (header task and
    current-turn task excluded), newest-first, bounded: max 10 tasks,
    name cap 100 chars, per-task best-step-result summary cap 200 chars
    (same informativeResultLine/bestStepResult selection as the digest),
    whole answer hard-capped at 3800 chars (gate bound is 4000). Nil-
    guarded store access; errors degrade to the header-only answer.
- internal/agent/dispatcher.go
  - T3 (F-ORCH-24): new arbitration branch in classifyIntent, placed
    after the chat-imperative arbitration and BEFORE quickplan_cue_upgrade:
    ShouldUseLLMResult && verdict.ShouldCreateTask() && isFactRecallQuestion(input)
    -> replace verdict with IntentRecall @0.85, method "fact_memory_recall",
    agent chat; no task creation. Same posture/telemetry as
    platform_recall_arbitration (recordClassificationMethod/Agent/IntentType).
- internal/agent/strategic.go
  - T3 (F-NEW-9): shared Plan tail calls sp.ensureStepPromptFidelity(req,
    steps[0]) right after the session-context injection, before persistence
    -- covers quick_plan (and every mode through the shared tail).

New:
- internal/agent/recall_fact_routing.go
  - isFactRecallQuestion: interrogative + personal past-conversation cue
    ("told you", "i said", "do you remember", "earlier in this session",
    ...), narrowed by: no leading imperative verb, not work-status-recall
    shaped (referencesPriorWork -- those already route to RecallAnswer).
    Conservative by design: only task-creating verdicts on this shape are
    overridden; recall/chat verdicts and genuine work are untouched.
- internal/agent/step_prompt_fidelity.go
  - explicitPathTokens (absolute Unix/Windows path tokens from the input)
    + ensureStepPromptFidelity: when the planner's decomposition dropped
    an explicit path the full user prompt names, append the verbatim
    req.Input to the FIRST step under an idempotent "## Original user
    request" marker. Steps already carrying every path (incl. direct-mode
    req.Input-verbatim steps) are untouched.

Docs:
- docs/plans/20261119-usability-remediation/03-context/02-PROGRESS.md (checkpoints)
- docs/plans/20261119-usability-remediation/03-context/02-REPORT.md (this file)

## 2. Verify output

Sibling compile-RED handling: go -overlay with /tmp/recall-overlay/overlay.json
emptying ONLY internal/agent/ralph_storm_test.go (package agent stub).
queue_lifecycle/registry_remap/failure_terminal test files compile as-is.

Gate run (all GREEN):
  go test -overlay /tmp/recall-overlay/overlay.json ./internal/agent/ \
    -run 'TestRecallFailedState|TestRecallTaskSet|TestRecallFactToMemory|TestStepPromptFidelity' -count=1 -v
  --- PASS: TestRecallFactToMemory (0.02s)      [T5: no task created, intent not quickplan, I5 control]
  --- PASS: TestStepPromptFidelity (0.08s)      [T5: real quick_plan path, persisted step carries /tmp/... path]
  --- PASS: TestRecallFailedState (0.02s)       [T4: failed/cancelled/rejected + completed_unchanged + non-terminal_keeps_in-progress]
  --- PASS: TestRecallTaskSet (0.00s)           [T4: hello.txt surfaced despite newer unrelated task; bounded]
  ok  github.com/caimlas/meept/internal/agent  0.940s

Adjacent regression suites (GREEN):
  -run 'TestRecall|TestSessionContextDigest|TestReferencesPriorWork|TestDispatcher_ClassifyAndRoute|TestPlanRepair|TestStrategic|TestClassifyAndRoute|TestScope|TestSessionDigest|...Arbitration...'
  ok 1.899s -- includes the pre-existing recall_answer_test.go option-3 pins,
  digest preference/cap pins, arbitration order pins, plan-repair pins.

Build: go build ./... => BUILD_OK.
gofmt -l on all touched files: clean. ASCII only; wrapped errors unchanged;
no '_ = ' introduced.

Full-package sweep note (not this leaf's scope): the full ./internal/agent/
run shows failures in OTHER leaves' gate/feature tests present in the shared
worktree (failure_terminal_test.go TestPlanRequestErrorTerminal /
TestPairFailureTerminal / TestPairExhaustionTerminal panic, plus
TestTrivialFastLane, TestGitVetoShapeIndependent_EditShapes,
TestDraftChatSurface*, TestNoticePopulated_*, TestThreadResolveOnce_FullInputTopic,
TestHintCapabilityFit_ResearchStepCodeHint). None are recall/digest/planner-
fidelity tests; every suite covering this leaf's touched code paths is GREEN
above. These belong to the parallel 01-safety / sibling leaves.

## 3. Live-gate + I5 evidence

- I5 verdict: NO scope leak on the dispatcher recall path. The T5 I5 control
  drives d.sessionScopeOptions(chat, conv-fact-session-b) against a real
  SQLite memory manager holding session A's project-scoped ZEBRA-4417 fact;
  session B (unbound/foreign project) does NOT retrieve it -- control GREEN
  before and after this leaf. sessionScopeOptions/EffectiveMemoryScope wiring
  was NOT modified (scope_inputs.go / scope_wiring.go / dispatcher_scope.go
  untouched); the fact lane answers through the same scoped
  buildMemoryContext/Manager.Search path, so the filter is applied, not
  bypassed. No follow-up leaf needed for scope wiring.
- Live rig probes (p1 T3/T4, p2 A3/B) not run from this leaf session (no rig
  daemon here); gate-level equivalents all GREEN as above. Orchestrator
  verified the gates independently.

## 4. Deviations

- Gate file naming: T4 lives in recall_state_gate_test.go (leaf named
  recall_answer_test.go, which pre-exists) -- deviation already recorded in
  the committed gate header; no existing test touched.
- T2 implementation lives in recall_answer.go (renderRecallTaskSet) rather
  than changing buildSessionContextDigestExcluding itself: the digest's
  LastTask* contract is pinned by multiple existing tests (prefer-completed,
  caps, executing-agent); widening the shared digest struct would have
  churned those pins. The recall answer now composes header digest +
  bounded task-set -- same user-visible outcome, zero digest-contract change.
- No gate weakening, no commits, no scope creep.
