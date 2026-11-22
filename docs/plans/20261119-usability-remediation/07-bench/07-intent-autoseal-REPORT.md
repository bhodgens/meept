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
