# Result topic stamp - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below. Make the 00T topic test pass. Do not weaken it.
> Do NOT commit. Do NOT git add.

## Meta

- **Parent:** master.md
- **Scope:** Stamp ResultTopic on the plan at round start. Execute and ApproveAndExecute read that stamp.
- **Dependencies:** 00T-test-gate.md, contract C1
- **Estimated Context:** 55K
- **Concurrency Group:** A

## Goal

A later bus event must not change the topic of a plan that already started.

## Tasks

1. Add `ResultTopic string` to `PlanRef` in `internal/employee/goal_loop.go`. Empty means `employee.result`.

2. Persist the field on the stored plan. Find the plan create and load path used by GoalLoop (plan store or PlanBrief). Add the field there. Resume and ApproveAndExecute must load it back. A load miss uses `employee.result` and logs a warning. Do not invent `bot.result`.

3. When `triggerViaGoalLoop` builds a round, copy the topic for that trigger onto the PlanRef at construction time. `bus_event` stamps `bot.result`. Every other source stamps `employee.result`. Tier-1 builds its PlanRef inside decideTier1. Stamp it there from the trigger, not from a later read of the loop flag.

4. Change `GoalLoop.Execute` so the publish topic comes from `plan.ResultTopic`. Delete the read of `l.resultTopic` for publish selection. `SetResultTopic` may remain only as a deprecated setter that tests still call, or be removed if no production caller remains. Do not leave Execute reading it.

5. `ApproveAndExecute` must not call `SetResultTopic`. It passes the loaded PlanRef into Execute.

6. Make `TestPlanRefTopicSurvivesLaterBusEvent` pass.

## Interface Contract

Exposes C1. Consumers read `plan.ResultTopic` only.

## Self-Verification Checklist

- [ ] Execute does not read l.resultTopic to choose the topic
- [ ] A deferred approve still publishes the topic stamped at round start
- [ ] gofmt on touched files
- [ ] Do NOT commit

## Review Checklist

- [ ] C1 field name and empty-means-employee rule match master.md
- [ ] No kind field added to ExecutionResultEvent
- [ ] Do NOT commit
