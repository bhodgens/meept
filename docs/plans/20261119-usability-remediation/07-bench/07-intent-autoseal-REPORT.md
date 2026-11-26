# 07-bench / 07 intent auto-seal -- REPORT

Feature: intent-driven plan auto-seal. A compound plan+implement prompt
("create the plan and then implement it") seals the plan after compilation --
the approval park is skipped and the task proceeds straight to scheduling with
an audit trail. Plain plans still park.

## Changed files

1. internal/agent/strategic.go (+189)
   - PlanRequest.AutoSeal bool (json auto_seal) -- intent-driven only, no
     config knob.
   - requiresApproval: first check, req.AutoSeal -> false (gate bypass).
     The flag is only ever set on the compound lane, so plain plans can
     never reach it.
   - New pure detector PlanAutoSealFromIntent(input): explicit seal markers
     ("implement the plan", "and implement it", "implement it immediately")
     short-circuit true; otherwise requires plan verb (plan/design/outline/
     draft/"map out"/"break down") AND execute verb (implement/execute/
     build/create/code/write/apply) AND compound connector (" and "/" then "/
     comma/" plus ") AND plan noun (plan/approach/design/blueprint/roadmap);
     negated execute verb ("don't implement", "do not execute") vetoes.
     ASCII word-boundary matching so "planned"/"codes" never match.
   - autoSealPlanAudit: Info log, strategic_planner.auto_seal metric,
     task metadata {"auto_sealed": true, "auto_seal_reason":
     "compound plan+implement intent"}.
   - Plan() tail: audit runs after the gate branch when AutoSeal is set;
     task flows through the EXISTING persist -> StateExecuting ->
     PromoteReadySteps -> task.planned -> orchestrator.schedule path.
     task.planned now carries auto_sealed (true/false) so the UI can show
     "auto-sealed by intent". No new events, no new states, no new knob.
2. internal/agent/handler.go (+12)
   - publishPlanRequest, inside the existing Intent==compound block:
     req.AutoSeal = PlanAutoSealFromIntent(result.Task.Description). The
     compound parent task description is deliberately the FULL user input
     (pinned by dispatcher routeCompoundWithModel). AutoSeal rides the
     orchestrator.plan JSON payload to the strategic planner.
   - dispatcher.go: UNTOUCHED (conflict surface with other live sessions;
     no change was needed there).
3. internal/agent/strategic_autoseal_test.go (new, 4 tests)
   - TestPlanAutoSealFromIntent: 18-case table (compound prompts true;
     plain plans, negated forms, empty, partial-word traps false).
   - TestRequiresApproval_AutoSealBypassesGate: AutoSeal + 6 steps +
     threshold 5 -> false; plain plan same shape -> true; plain plan below
     threshold -> false (baseline unchanged).
   - TestAutoSealSkipsApprovalPark: real store+bus harness. AutoSeal
     request: gate bypassed, task lands StateExecuting, NO pending_steps
     metadata, auto_sealed=true metadata, task.planned carries
     auto_sealed=true, orchestrator.schedule published,
     task.pending_approval NOT published, 4 steps persisted, 3 ready.
   - TestAutoSeal_PlainPlanStillParks: plain plan past threshold:
     StateAwaitingApproval, pending_steps present, task.pending_approval
     published, auto_sealed metadata ABSENT.

## Safety posture

- Intent-driven ONLY: detection is gated behind the compound dispatch lane
  (Intent==compound) AND the lexical detector. A plain plan prompt can never
  auto-seal even if it trips the lexical shape.
- Mis-detection risk accepted per design: a sealed plan still runs evidence
  review + the full output-filter chain; only the human sign-off pause is
  skipped, and the user explicitly asked for execution.
- The gate still applies unchanged to: un-approved interviews, quick_plan
  un-approved interviews, and over-threshold plain plans.

## Verification (all real runs, -count=1)

- go test ./internal/agent/ -run 'TestCompound|TestAutoSeal|TestAwaitApproval|
  TestPlan' -count=1  -> ok (1.677s)
- go test ./internal/agent/ ./internal/rpc/ -count=1
  -> ok internal/agent 98.726s, ok internal/rpc 6.710s. Failure set did not
  grow: BOTH packages pass outright.
- go build ./... -> exit 0
- gofmt -l internal/agent/ -> clean

## Not done / handoff notes

- Not committed (per instructions; orchestrator commits).
- ASCII throughout; no em-dashes in code or docs.
- The audit-trail metadata keys (auto_sealed / auto_seal_reason) and the
  task.planned auto_sealed field are new UI surfaces; no UI code was changed
  in this leaf (the flag is additive and self-describing).

---

# Addendum: F-BENCH-R7 fix (bench repo, not committed)

Worktree: /Users/caimlas/git/meept-bench/.hermes/worktrees/usability-acceptance
(base a85b074 + da1b2e9; no commit made, per instructions).

## Root cause (confirmed from run-7 artifacts)

The REAL parked turn.terminal payload, captured from
/tmp/usability-rig-acc/bench-results-run7-final/transcripts/
usability-u-plan-mode-request-a1.json (turn-e77fa1b4e0fb3b2f), carries
status="parked", handler_case="async_dispatch", error ABSENT (the key is
not present at all), and no marker text anywhere in the 13-key payload.
The "Plan awaiting user approval" line exists ONLY in the daemon log and
the task.pending_approval bus event (internal/agent/strategic.go
awaitUserApproval). Worse, EVERY async_dispatch ack parks with this same
terminal shape (u-steer, u-context, u-direct all show identical parked
payloads), so the terminal payload alone cannot distinguish an
approval-gate park from any other async ack. The a85b074 gate
(strings.Contains(p.Error, planParkMarker)) could therefore never fire on
a real daemon, and the unit fake -- which carried the marker inside
error -- masked the gap (fake parity defect).

## Fix (meept-bench, 3 files)

1. internal/daemonclient/daemonclient.go: the auto-approver predicate is
   now just status=="parked" + opt-in (one attempt per turn). The REAL
   gate moved to where the true signal lives: FindAndApproveParkedTask
   approves only after task.list_extended observes the session task in
   state awaiting_approval (strategic.go StateAwaitingApproval).
   planParkMarker const removed; stale doc comments updated. Non-opt-in
   behavior is byte-identical to before (park stalls exactly as in run 7).

2. internal/daemonclient/daemonclient_async_test.go (fake parity, two
   defects):
   - The harness's scripted parked terminal now uses the REAL run-7
     payload shape (no error key, full 13-key fixture), via
     realParkedTerminalFixture() in plan_approve_test.go.
   - The fake daemon kept a single shared bufio reader slot (f.buf) for
     ALL connections; the approval harness's forked approver connection
     stole the main connection's request frames and answered them on the
     wrong socket, hanging ChatAsync forever in readFrame (the approval
     tests deadlocked -- deterministic under load; reproduced on
     PRISTINE a85b074 in a scratch copy, so pre-existing, not introduced
     here). serve() now uses a per-connection reader; cleanup closes all
     accepted connections. This matches the runner topology, which forks
     a second connection for the approver by design.

3. internal/daemonclient/plan_approve_test.go:
   - realParkedTerminalFixture(): the actual run-7 parked JSON, only
     turn_id/task_id parameterized for harness matching.
   - TestApproveFiresOnRealParkedShape: real-shape park -> exactly one
     task.approve -> post-approval completed terminal. The old
     marker-in-error gate fires NOTHING on this shape, so this test
     would have caught the F-BENCH-R7 seam gap.
   - TestNoApproveWithoutOptInOnRealShape: same real shape without
     opt-in -> stall, zero task.approve (gate stays default-off).

## Verification (all real runs, -count=1)

- go build ./... -> exit 0
- go test ./internal/daemonclient/ -run 'TestApprove|TestChatAsyncAutoApprove|
  TestFindAndApprove|TestNoApprove|TestChatAsync' -count=1
  -> ok 7.635s (real-shape pass, fake-shape pass, wire pass, failure-keeps-
  park, disabled-by-default, resolved-state no-op, never-parked timeout,
  all ChatAsync async suite) -- NOTE: the full package now runs in ~8s;
  the >380s real-time park awaits in the task brief no longer reproduce
  because the shared-reader deadlock (which the waits were masking) is
  fixed.
- go test ./internal/daemonclient/ -count=1 -> ok 8.343s (full package)
- go test ./internal/runner/ ./internal/suite/ ./internal/checkers/
  -count=1 -> ok 11.4s / 0.9s / 1.2s (no regressions)
- gofmt: clean on all three touched files; git diff carries zero non-ASCII
  additions (one pre-existing em-dash comment line rewritten to "--" in
  passing).

## Honest residuals

- The fake's awaiting_approval task state is scripted; the daemon-side
  park -> approve -> execute sequence remains proven only by the run-7
  manual task.approve probe, not by this unit tier. An end-to-end u-plan
  re-run is still the definitive closure for the F-BENCH-R7 graded
  timeout verdict.
- startApprovalHarness's re-arm watcher reads approveCalls from a
  buffered respCh signal; harmless here, but a longer-lived fake might
  want an explicit state machine.
