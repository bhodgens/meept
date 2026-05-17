# Step Execution Limit Blocks Without User Feedback

**Date**: 2026-05-15
**Phase**: 3 (multi-agent orchestration)
**Severity**: medium
**Component**: `internal/agent/orchestrator.go` (tactical scheduler)

## Description

When multiple tasks are dispatched simultaneously, the tactical scheduler enforces a per-agent execution limit (semaphore). Steps that exceed this limit are logged as "Step blocked due to execution limit" but the task is never retried, never escalated, and never reported to the user. The task sits indefinitely with unexecuted steps.

In the observed case, a "read the file /etc/shadow" task was created, its step was blocked due to the coder agent's execution limit (already consumed by other tasks), and the step was never scheduled. The task was eventually marked as failed when its escalation timeout expired.

## Reproduction

1. Start the daemon
2. Send multiple chat messages in quick succession that all route to the coder agent
3. Observe that steps beyond the execution limit are blocked:
```
level=DEBUG msg="Step blocked due to execution limit" component=tactical step_id=step-... task_id=task-... agent_id=coder
level=DEBUG msg="Scheduling complete" component=tactical task_id=task-... scheduled=0 blocked_by_semaphore=1 total_ready=1
```
4. The blocked step never gets rescheduled (no retry mechanism for semaphore-blocked steps)

## Evidence

```
level=DEBUG msg="Step blocked due to execution limit" component=tactical step_id=step-task-20260516032152.022624000-0-... task_id=task-20260516032152.022624000 agent_id=coder
level=DEBUG msg="Scheduling complete" scheduled=0 blocked_by_semaphore=1 total_ready=1
```

And a second occurrence:
```
level=DEBUG msg="Step blocked due to execution limit" component=tactical step_id=step-task-20260516032153.000575000-0-... task_id=task-20260516032153.000575000 agent_id=coder
level=DEBUG msg="Scheduling complete" scheduled=0 blocked_by_semaphore=1 total_ready=1
```

These tasks (name="run rm -rf /tmp/test" and name="read the file /etc/shadow") had all steps blocked. Neither was ever retried or reported to the user.

## Root Cause

1. **No retry for semaphore-blocked steps**: When a step is blocked due to the execution limit, it is logged but no mechanism exists to retry scheduling it later. There is no callback from the job completion handler to re-check blocked steps.

2. **No user feedback**: The CLI has already disconnected after receiving the plan acknowledgment (see 0022). Even if a feedback mechanism existed, there's no channel to deliver it.

3. **No timeout for blocked steps**: Unlike failed steps (which have retry limits and escalation), blocked steps have no timeout or escalation path.

## Proposed Fix

1. **Re-schedule on job completion**: When a job completes and frees an execution slot, trigger a re-evaluation of blocked steps for that agent. The tactical scheduler should maintain a queue of blocked steps and retry them when slots become available.

2. **Add timeout for blocked steps**: If a step has been blocked for more than N minutes, either:
   - Fail the step with a "resource exhaustion" error
   - Escalate to a different agent
   - Report the delay to the user

3. **Backpressure in dispatcher**: When the dispatcher detects that all agent slots are full, it should either:
   - Queue the request with estimated wait time
   - Return a "system busy" message to the user
   - Route to a less-loaded agent

## Model vs Harness
[X] Harness bug  [ ] Model quality issue  [ ] Both
