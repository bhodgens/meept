# 07-bench / 07 intent auto-seal -- PROGRESS

Feature: intent-driven plan auto-seal. A user prompt that says
"create the plan and then implement it" (compound plan + execute intent)
must seal the plan after compilation -- the approval park is skipped and the
task proceeds directly to scheduling with an audit trail. Plain plan prompts
still park at the approval gate.

## T1: root-cause / inspect (DONE)

How the compound flow feeds PlanRequest today:

- dispatcher.go `classifyMultiIntent` (~:2962) runs the keyword + LLM
  multi-intent classifiers, then `MultiIntent.DetectCompound` (~:2772) sets
  IsCompound + CompoundType ("sequential" when a planning lane
  -- IntentPlan/Architect/Collaborate/QuickPlan -- is in the strong-intent
  set via laneForcesSequentialCompound, else "parallel").
- routeCompoundWithModel (~:2851) creates the parent task with
  Intent=compound and writes metadata {"compound_type", "compound_intents",
  "compound_intent_types"}. The task description is the FULL user input
  (pinned by the comment at ~:2862: e2e T1 2026-09-10).
- handler.go publishPlanRequest (~:2030) reads that metadata back:
  Intent==compound sets req.IsCompound=true + req.CompoundType.
- strategic.go Plan: the gate at ~:1207 (`sp.requiresApproval(req, steps)`
  -> `awaitUserApproval` ~:2360) parks the task at StateAwaitingApproval with
  pending_steps metadata + task.pending_approval event.
- requiresApproval (~:2336) fires on: un-approved interview, quick_plan
  interview, or step count >= ApprovalStepThreshold.

DETECTION-POINT DECISION: detect in the STRATEGIC PLANNER layer from
PlanRequest.Input via a pure helper (PlanAutoSealFromIntent), wired by the
compound lane in handler.publishPlanRequest. NOT a dispatcher-set flag.

Rationale: PlanRequest.Input is the full user prompt on the compound lane;
the strategic planner is the single choke point for the approval gate; the
detector is a pure, unit-testable function; and handler.go/dispatcher.go
carry the least conflict surface with the other live sessions (dispatcher.go
was left untouched entirely).

## T2: detection + skip-park path (DONE)

internal/agent/strategic.go:

1. PlanRequest.AutoSeal bool (json "auto_seal,omitempty") -- intent-driven
   only, NO config knob.
2. New pure detector `PlanAutoSealFromIntent(input string) bool`:
   - explicit seal markers ("implement the plan", "execute the plan",
     "and implement it", "implement it immediately") short-circuit true;
   - otherwise requires plan verb (plan/design/outline/draft/"map out"/
     "break down") AND execute verb (implement/execute/build/create/code/
     write/apply) AND a compound connector (" and "/" then "/comma/" plus ")
     AND a plan noun (plan/approach/design/blueprint/roadmap);
   - negated execute verb ("don't implement", "do not execute") vetoes;
   - ASCII word-boundary matching (autoSealWord/autoSealWordBoundary) so
     "planned"/"codes" never match; lowercase via strings.ToLower (ASCII-safe
     per the repo convention -- non-ASCII letters are simply unmatched, a
     conservative false).
3. requiresApproval: FIRST check -- req.AutoSeal returns false (gate
   bypass). The flag is only ever set on the compound lane, so plain plans
   cannot reach it.
4. autoSealPlanAudit helper: Info log ("Plan auto-sealed by compound
   plan+implement intent" + task_id/steps), metric
   strategic_planner.auto_seal, task metadata {"auto_sealed": true,
   "auto_seal_reason": "compound plan+implement intent"} via mergeMetadata.
5. Plan() tail: when the gate is bypassed with AutoSeal set, the audit runs
   right after the gate branch; the task flows through the EXISTING
   persist-steps -> spec -> SetPlanCounters -> StateExecuting ->
   UpdateWithoutCounters -> PromoteReadySteps -> task.planned ->
   orchestrator.schedule path. task.planned now carries "auto_sealed"
   (true/false) so the UI can show "auto-sealed by intent". No new events,
   no new states: the existing approve-path machinery is reused.

## T3: end-to-end wiring (DONE)

internal/agent/handler.go publishPlanRequest: inside the existing
Intent==compound block, req.AutoSeal =
PlanAutoSealFromIntent(result.Task.Description) -- the full user prompt
(the compound parent task description is deliberately the full input).
PlanRequest JSON-serializes AutoSeal across the orchestrator.plan bus hop.
dispatcher.go: UNTOUCHED (conflict avoidance; no change needed there --
the compound lane already delivers Intent=compound + full-input task).

## T4: tests (DONE)

internal/agent/strategic_autoseal_test.go (new):

- TestPlanAutoSealFromIntent: 18-case table. Compound prompts
  ("create the plan and then implement it", "plan the migration and
  implement it", "design the auth flow, then implement it", "map out the
  approach and execute it", "outline the refactor plan plus code it", ...)
  = true. Plain plans ("create a plan for the migration", "design the auth
  flow", "break down the work into steps", "plan and design the approach",
  "draft the roadmap", "how should I structure the plan", negated
  ("plan the migration but don't implement it yet", "design a plan, do not
  execute anything"), empty, and partial-word traps ("planned communities
  and citywide codes") = false.
- TestRequiresApproval_AutoSealBypassesGate: AutoSeal + 6 steps +
  threshold 5 -> false; plain plan same shape -> true; plain plan below
  threshold -> false (baseline unchanged).
- TestAutoSealSkipsApprovalPark: real stores + bus harness
  (mirrors TestAwaitUserApproval). Proves requiresApproval=false for the
  AutoSeal request, then exercises the seal tail: task lands
  StateExecuting, NO pending_steps metadata, auto_sealed=true metadata,
  task.planned event carries auto_sealed=true, orchestrator.schedule
  published, task.pending_approval NOT published, 4 steps persisted, 3
  ready (roots + first dependent promoted).
- TestAutoSeal_PlainPlanStillParks: same harness, plain plan past threshold
  -> StateAwaitingApproval, pending_steps present, task.pending_approval
  published, auto_sealed metadata ABSENT.

All four pass (-count=1). No existing tests weakened.
