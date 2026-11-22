# 04-watchdog-exemption PROGRESS

## T1 - Exemption in reap loop (implementation written)
- Files: internal/agent/turn_watchdog.go
  - New seam SetTaskStateFunc(func(taskID string) task.TaskState) (mutex-guarded, nil-safe).
  - taskStateFor + exemptFromReap: planning | awaiting_approval | "awaiting_seal" exempt.
  - reapOnce skips exempt stale records WITHOUT Complete (task->turn edge preserved).
  - nil accessor keeps pre-C7 behavior; executing turns still reap.
- Verify: pending (overlay run below)

## T2 - Activity Touch (implementation written)
- Files: internal/agent/plan_draft.go, internal/agent/strategic.go
  - StrategicPlanner.turnLivenessTouch field + SetTurnLivenessTouch setter + touchTurnLiveness nil-safe helper.
  - Touch sites: SaveDraft (after persist), SealDraft (after persist), awaitUserApproval (gate entry), ApprovePlan (after schedule).
- Verify: pending