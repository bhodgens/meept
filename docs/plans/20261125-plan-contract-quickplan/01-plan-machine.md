# Leaf 01 — Plan machine: SubmitAndAutoApprove + quickplan through Synthesize

Contract: master.md C-1, C-2 (quickplan half). Decisions D1, D2.

## Deliverables

1. internal/plan/manager.go: `SubmitAndAutoApprove(ctx, planID)` —
   SubmitPlan + ApprovePlan(by="system") + Synthesize via existing CAS
   transitions; reuse the auto-approve body at ~:592; every step
   CAS-guarded; mid-step failure leaves the row at the last committed
   state for the reclaimer to drive.
2. internal/agent/dispatcher.go routeToPlan quick_plan branch (~:2352-
   2378): delete the separate `d.createTask(...)` orphan. Call
   SubmitAndAutoApprove when intent/adjudication does NOT require
   approval; call SubmitPlan (pending_approval) when it does. Dispatch
   Result: Task = the plan's primary task (read back from the plan
   after Synthesize), SuggestedMode quick_plan kept, Response empty
   (async ack carries the brief plan), AgentOverride preservation
   (researcher-extract contract) unchanged.
3. Ack/notification copy: "executing the plan" naming the brief plan
   text; the async subtask list surfaces from the plan's steps.

## Files owned

internal/plan/manager.go, internal/plan/*_test.go (new pin file),
internal/agent/dispatcher.go (quick_plan branch only),
internal/agent/dispatcher_quickplan_plan_contract_test.go (new).

## Pins

- quick_plan turn (plans.mode default path): plan row ends executing->
  completed as task rollup; exactly ONE primary task exists; no
  pending_approval zombie; plan.TaskID set.
- SubmitAndAutoApprove concurrent with TUI plan.approve: both CAS arms
  land on approved once; single signoff row.
- Synthesize failure mid-sequence: row stays approved, reclaimer
  re-drives (existing machine, pin that SubmitAndAutoApprove does not
  roll the CAS backwards).
- ApprovalRequested=true variant: row parks at pending_approval, no
  task created until approve (pin the branch, seam-provided by leaf 03).

## Gates

go build ./internal/plan/ ./internal/agent/; go test -count=1
./internal/plan/ + targeted dispatcher pins; full agent suite ~100s;
gofmt. Commit per concern naming M11/D1/D2.
