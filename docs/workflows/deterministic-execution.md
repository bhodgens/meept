# Deterministic Execution Framework

## Overview

Meept implements a comprehensive deterministic execution framework that ensures reliable, verifiable task completion through evidence-based validation, concurrency control, and retry/repair mechanisms. The system achieves production-grade determinism through multiple layers of verification and control.

## Problem

LLM-based agent systems face several challenges in production environments:

- **Hallucinated completions**: Agents may claim work done without verification
- **Resource exhaustion**: Uncontrolled concurrent execution can overwhelm systems
- **Silent failures**: Errors may go undetected without proper validation
- **Non-reproducible results**: Lack of state tracking prevents debugging
- **Cascading failures**: No retry mechanisms for transient errors

## Solution

The deterministic execution framework addresses these challenges through:

1. **Evidence-based validation**: All claims require verifiable evidence
2. **Concurrency control**: Global and per-agent semaphores prevent resource exhaustion
3. **Multi-layer retry logic**: Transient errors trigger automatic retries
4. **State transition logging**: Complete audit trail for debugging
5. **Checkpoint/rollback**: Git-based checkpoints enable recovery
6. **Validator coverage**: Type-specific validators for all tool categories

## Architecture

### Evidence Flow Pipeline

```
┌─────────────┐    ┌──────────────┐    ┌─────────────┐    ┌────────────┐
│ ToolResult  │ -> │ Execution    │ -> │ TaskStep    │ -> │ Validator  │
│ .Evidence   │    │ Result       │    │ .Evidence   │    │            │
│             │    │ .Evidence    │    │             │    │            │
└─────────────┘    └──────────────┘    └─────────────┘    └────────────┘
```

**Components:**
- **Tools**: Produce evidence automatically (file hash, exit code, etc.)
- **Executor**: Extracts and propagates evidence from ToolResult
- **Tactical Scheduler**: Persists evidence to TaskStep before validation
- **Validator**: Verifies evidence against ground truth

### Evidence Types

| Type | Description | Produced By |
|------|-------------|-------------|
| `file_exists` | File exists at path with metadata | ReadFile, WriteFile, DeleteFile, ListDirectory |
| `file_hash` | SHA256 hash of file content | ReadFile, WriteFile |
| `process_exit` | Process exit code | Shell |
| `shell_output` | Command output (hashed) | Shell |
| `api_response` | HTTP status and response size | WebFetch, WebSearch |
| `db_row` | Database operation metadata | Memory operations |

### Concurrency Control

```
┌─────────────────────────────────────────────────────────┐
│                   Global Semaphore                       │
│                   (max 10 concurrent)                    │
└─────────────────────────────────────────────────────────┘
         │                    │                    │
         ▼                    ▼                    ▼
┌─────────────┐      ┌─────────────┐      ┌─────────────┐
│ Agent: coder│      │Agent:debugger│     │ Agent:other │
│ (max 3)     │      │  (max 3)     │      │  (max 3)    │
└─────────────┘      └─────────────┘      └─────────────┘
```

**Features:**
- Non-blocking acquisition with immediate fallback
- Blocked steps remain in "ready" state for next scheduling cycle
- Configurable limits via `MaxConcurrentJobs` and `MaxConcurrentPerAgent`

### Validation Gates

```
Step 1 → Step 2 → Step 3 → [Gate] → Step 4 → Step 5 → Step 6 → [Gate]
                                    ↑                          ↑
                              Check all completed        Check all completed
                              steps validated            steps validated
```

**Features:**
- Configurable interval (default: every 3 steps)
- Non-blocking: logs warnings without stopping execution
- Checks all completed steps have `Validated = true`

### Retry Logic Hierarchy

```
┌─────────────────────────────────────────────────────────────┐
│ L1: Per-Tool Retry (tools/registry.go)                       │
│     - Tool-specific policies (0-2 retries)                   │
│     - Exponential backoff for network operations             │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│ L2: Job-Level Retry (queue/store.go)                         │
│     - Rate limit retry with backoff (2s, 4s, 8s)             │
│     - Transient error detection                              │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│ L3: Agent Loop Retry (agent/loop.go)                         │
│     - Model failover (rotate through alias)                  │
│     - Exponential backoff (2s, 4s, 8s, 16s, 32s)             │
│     - Max 5 attempts                                         │
└─────────────────────────────────────────────────────────────┘
```

### Claim-Evidence Matching

The validator detects mismatches between claims and evidence:

| Claim Pattern | Required Evidence |
|---------------|-------------------|
| "created", "wrote", "modified", "updated" | `file_exists` or `file_hash` |
| "executed", "ran", "command", "shell" | `process_exit` |
| "fetch", "api", "http", "web" | `api_response` |
| "memory", "stored", "retrieved", "context" | `db_row` |

## Behavior

### Task Completion Flow

1. **Agent executes tools** → Tools produce `ToolResult.Evidence`
2. **Executor extracts evidence** → `ExecutionResult.Evidence`
3. **Tactical Scheduler persists** → `TaskStep.Evidence`
4. **Validator checks evidence** → `Validated = true/false`
5. **If validation fails** → Step marked as `needs_info` for human review

### Validation Process

```
┌─────────────────────────────────────────────────────────────┐
│ StepValidator.Validate()                                     │
├─────────────────────────────────────────────────────────────┤
│ 1. Validate each evidence with appropriate validator         │
│    - FilesystemValidator for file_exists, file_hash          │
│    - ShellValidator for process_exit, shell_output           │
│    - WebValidator for api_response                           │
│    - MemoryValidator for db_row                              │
│                                                                │
│ 2. Validate claims against evidence                          │
│    - Check claim patterns match evidence types               │
│    - Fail if claims exist without evidence                   │
│                                                                │
│ 3. Return ValidationResult                                   │
│    - Valid: true/false                                        │
│    - Errors: []string                                         │
│    - Warnings: []string                                       │
└─────────────────────────────────────────────────────────────┘
```

### Retryable Error Patterns

| Category | Patterns |
|----------|----------|
| Rate limits | `llm.IsRateLimitErrorMessage()` |
| Timeouts | "timeout" |
| Network | "connection refused", "connection reset", "broken pipe", "network" |
| Resource | "busy", "lock", "deadlock", "unavailable" |
| Transient | "temporary", "try again later" |

### Checkpoint Operations

| Operation | Description |
|-----------|-------------|
| `CreateCheckpoint(taskID, label)` | Creates git tag `checkpoint-{taskID}-{label}-{timestamp}` + metadata |
| `RestoreCheckpoint(taskID, label)` | Checks out most recent checkpoint tag |
| `ListCheckpoints(taskID)` | Lists all checkpoints for task |
| `DeleteCheckpoint(taskID, label)` | Removes checkpoint tag and directory |

### State Transitions

All step state transitions are logged to `task_state_transitions` table:

```sql
CREATE TABLE task_state_transitions (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    step_id     TEXT NOT NULL,
    from_state  TEXT NOT NULL,
    to_state    TEXT NOT NULL,
    reason      TEXT,
    agent_id    TEXT,
    timestamp   DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

Transition logging is configurable via `SetTransitionLogging(enabled bool)`.

## Configuration

```toml
[execution]
# Concurrency control
max_concurrent_jobs = 10
max_concurrent_per_agent = 3

# Validation gates
validation_gate_interval = 3  # Run gate every N steps

[retry]
# Job-level retry
max_retries = 3
retry_delay_base = "2s"
transient_error_patterns = ["timeout", "connection refused", "network"]

# Per-tool retry (in tools section)
[tools.retry.file_read]
max_retries = 1
retry_delay = "100ms"
retryable = true

[tools.retry.web_fetch]
max_retries = 2
retry_delay = "1s"
exponential = true

[validation]
# Evidence requirements
require_evidence = true
fail_unknown_evidence_types = true

# Checkpoints
enable_checkpoints = true
checkpoint_label_format = "{step_id}-{timestamp}"
```

## Validator Coverage

| Tool Hint | Validator | Evidence Types |
|-----------|-----------|----------------|
| `code`, `refactor` | FilesystemValidator | `file_exists`, `file_hash` |
| `file_write`, `file_read` | FilesystemValidator | `file_exists`, `file_hash` |
| `list_dir`, `file_delete` | FilesystemValidator | `file_exists` |
| `shell` | ShellValidator | `process_exit`, `shell_output` |
| `web_fetch`, `web_search` | WebValidator | `api_response` |
| `memory_*` | MemoryValidator | `db_row` |

## Observability

### Logging

- Evidence collection: "Tool produced evidence" (count, type)
- Validation: "Validation passed/failed" (step_id, errors)
- Concurrency: "Step blocked due to execution limit" (step_id, agent_id)
- Retry: "Retryable error detected" (job_id, retry_count, reason)
- Checkpoints: "Checkpoint created/restored" (task_id, label, tag)
- Transitions: State changes logged to `task_state_transitions` table

### Metrics

| Metric | Description |
|--------|-------------|
| `validation_success_rate` | Percentage of steps passing validation first time |
| `evidence_density` | Average evidence items per completed step |
| `semaphore_block_rate` | Percentage of steps blocked by semaphore |
| `retry_success_rate` | Percentage of retries that eventually succeed |
| `claim_evidence_mismatch_rate` | Percentage of steps with mismatched claims |

### Debug Info

- `GET /api/v1/steps/:id/evidence` - Step evidence details
- `GET /api/v1/steps/:id/transitions` - State transition history
- `GET /api/v1/tasks/:id/checkpoints` - Task checkpoints

## Edge Cases

### Evidence Pipeline Failure

**Detection:** Tools produce evidence but step shows empty `Evidence` array
**Resolution:** Check `executor.go` evidence extraction and `tactical.go` persistence

### Validator Not Found

**Detection:** Unknown tool hint logs "No validator for tool hint"
**Resolution:** Register validator via `ValidatorManager.RegisterValidator()`

### Semaphore Deadlock

**Detection:** Steps stuck in "ready" state indefinitely
**Resolution:** Check semaphore acquisition/release balance

### Revision Count Not Incrementing

**Detection:** Steps exceed max revision cycles without triggering limit
**Resolution:** `ReviewManager.HandleReviewResult` increments count BEFORE creating revision

### Unknown Evidence Type

**Detection:** Validator returns "unknown evidence type: X" error
**Resolution:** Add evidence type to `models.EvidenceType` constants and implement validator

### Checkpoint Restore in Dirty Workspace

**Detection:** Git checkout fails with "local changes would be overwritten"
**Resolution:** Stash current changes before restore, or force checkout with warning

### Dead-Letter Recovery

**Detection:** Jobs disappear from queue without completion
**Resolution:** Check dead_letter table, use `RecoverFromDeadLetter(jobID)` API

## Testing

### Evidence Flow Test

```go
func TestEvidenceFlow(t *testing.T) {
    // 1. Tool produces evidence
    toolResult := &ToolResult{
        Result: "file written",
        Evidence: []models.Evidence{
            NewEvidence(EvidenceFileExists, "/tmp/test.txt", "size=100", "file_write"),
        },
    }

    // 2. Executor extracts evidence
    execResult := executor.Execute(ctx, toolCall)
    assert.Len(t, execResult.Evidence, 1)

    // 3. Tactical Scheduler persists evidence
    tactical.OnJobCompleted(ctx, jobID, execResult)

    // 4. Step has evidence
    step, _ := stepStore.GetByJobID(jobID)
    assert.Len(t, step.Evidence, 1)

    // 5. Validator checks evidence
    err := validatorManager.ValidateStep(ctx, step)
    assert.NoError(t, err)
}
```

### Concurrency Test

```go
func TestSemaphoreLimits(t *testing.T) {
    scheduler := NewTacticalScheduler(TacticalSchedulerConfig{
        MaxConcurrentJobs: 10,
        MaxConcurrentPerAgent: 3,
    })

    // Schedule 5 steps for same agent
    for i := 0; i < 5; i++ {
        scheduler.ScheduleReadySteps(ctx, taskID)
    }

    // Verify only 3 are scheduled (semaphore limit)
    steps, _ := stepStore.ListByTaskID(taskID)
    scheduled := countByState(steps, StepScheduled)
    ready := countByState(steps, StepReady)

    assert.Equal(t, 3, scheduled)  // At semaphore limit
    assert.Equal(t, 2, ready)      // Blocked, remain ready
}
```

### Claim-Evidence Mismatch Test

```go
func TestClaimEvidenceMismatch(t *testing.T) {
    step := &TaskStep{
        Claims: []string{"Created file /tmp/test.txt"},
        Evidence: []models.Evidence{},  // No evidence!
    }

    result := validator.Validate(ctx, step)
    assert.False(t, result.Valid)
    assert.Contains(t, result.Errors, "claims made without supporting evidence")
}
```

## Migration Guide

### Enabling Deterministic Execution

1. **Evidence Collection** (automatic):
   - All builtin tools already produce evidence
   - Custom tools: add evidence to `ToolResult.Evidence`

2. **Validation** (automatic):
   - `ValidatorManager` is initialized with all validators
   - Configure `validation_gate_interval` for intermediate checks

3. **Concurrency Control** (automatic):
   - Semaphores initialized in `NewTacticalScheduler()`
   - Adjust limits in configuration

4. **Retry Logic** (automatic):
   - Per-tool policies in `defaultRetryPolicies`
   - Job-level retry in `OnJobFailed()`

5. **Checkpoints** (manual):
   - Call `CreateCheckpoint(taskID, label)` at key milestones
   - Use `RestoreCheckpoint(taskID, label)` for rollback

6. **Transition Logging** (configurable):
   - Enable via `stepStore.SetTransitionLogging(true)`
   - Query via `GetTransitions(stepID)`

## Metrics Dashboard

```
┌─────────────────────────────────────────────────────────────┐
│ Deterministic Execution Dashboard                           │
├─────────────────────────────────────────────────────────────┤
│ Validation Success Rate    │████████░░░░░░░░│ 82%          │
│ Evidence per Step (avg)    │ 2.3             │              │
│ Semaphore Block Rate       │██░░░░░░░░░░░░░░│ 12%          │
│ Retry Success Rate         │████████████░░░░│ 78%          │
│ Claim-Evidence Mismatch    │░░░░░░░░░░░░░░░░│ 3%           │
├─────────────────────────────────────────────────────────────┤
│ Active Checkpoints: 5       Dead-Letter Jobs: 2            │
│ Avg Validation Gate Time: 45ms                             │
└─────────────────────────────────────────────────────────────┘
```

## Related Documents

- [audit-determinism-mk2.md](../audit-determinism-mk2.md) - Original comprehensive audit
- [audit-determinism-mk2-verification.md](../audit-determinism-mk2-verification.md) - Gap closure verification
- [tool-routing.md](tool-routing.md) - Tool matching and execution
- [collaborative-planning.md](collaborative-planning.md) - Review/approval workflow
- [security.md](security.md) - Security engine integration
