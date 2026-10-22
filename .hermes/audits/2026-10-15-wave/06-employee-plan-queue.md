# Bug Hunt Audit: Employee / Plan / Queue Stack

Date: 2026-10-15
Scope: internal/employee, internal/plan, internal/queue, internal/worker, internal/scheduler, internal/gate

## Findings

### F1: StateSynthesizing as Terminal Sink (M10 - FIXED in 5ef76c59)

- **Location:** `internal/plan/manager.go:142-160`
- **Severity:** MEDIUM
- **Mechanism:** StateSynthesizing is a LEASE STATE. Plans claimed into it with an EXPIRED lease are reclaimed by SynthesisReclaimer. Without this recovery driver, StateSynthesizing is a terminal sink.
- **Test:** Verify that `SynthesisReclaimer.ReclaimExpiredSynthesis` is called in `Manager.SynthesizeRecovery` and that it transitions expired StateSynthesizing plans back to StateDraft.
- **Evidence:** Source code at `internal/plan/manager.go:159` calls `ListPlansByState(ctx, StateSynthesizing, ...)` and `store.ReclaimExpiredSynthesis(ctx, id)`.

### F2: Worker Retry - Non-Retryable Error Path (M7 - FIXED)

- **Location:** `internal/worker/worker.go:422-433`
- **Severity:** LOW
- **Mechanism:** The worker checks `llm.IsNonRetryable(processErr)` before retrying. Non-retryable errors go directly to dead letter. Retry is attempt-aware via claim_token.
- **Test:** Verify that `llm.IsNonRetryable` correctly identifies quota/block errors and that `queue.Retry` is not called for those errors.
- **Evidence:** Source code at `internal/worker/worker.go:424-428` logs "Non-retryable error - skipping retry" and skips the `job.CanRetry()` branch.

### F3: Gate Command Configuration - Potential Command Injection

- **Location:** `internal/employee/goal_loop.go:1125-1183`
- **Severity:** MEDIUM (confidence: medium)
- **Mechanism:** `runCompletionGate` executes `cfg.Command` via `backend.Execute`. The command is config-driven from `GateConfig`, which allows arbitrary shell commands. If config is user-editable, this is a command injection risk.
- **Test:** Verify that `GateConfig.Command` is validated/sanitized before execution, or that only whitelisted commands are allowed.
- **Evidence:** Source code at `internal/gate/gate.go:150-154` calls `backend.Execute(ctx, runtime.Command{Cmd: cfg.Command, ...})` with no validation.

### F4: Gate Exit Status Mapping

- **Location:** `internal/gate/gate.go:159-162`
- **Severity:** LOW
- **Mechanism:** `GateResult.Passed` is true iff `res.ExitCode == 0`. The outcome is correctly mapped to the gate result.
- **Test:** Verify that a gate command exiting with code 1 results in `Passed: false` and that this triggers the `GoalAtRisk` health transition in `goal_loop.go:1044-1053`.
- **Evidence:** Source code at `internal/gate/gate.go:160` sets `Passed: res.ExitCode == 0`.

### F5: Goal Loop Paused Employee - Reflect Skip

- **Location:** `internal/employee/goal_loop.go:913-935`
- **Severity:** LOW
- **Mechanism:** When `statusFn()` returns "paused", Reflect is skipped. The goal health is set to GoalUnknown. This prevents progress while paused.
- **Test:** Verify that a paused employee's goal health transitions to GoalUnknown and that no LLM calls are made during Reflect.
- **Evidence:** Source code at `internal/employee/goal_loop.go:914-934` checks `statusFn()` and returns `GoalUnknown` without calling `reflectViaLLM`.

### F6: Queue Job Claim Semantics - No Duplicate Pickup

- **Location:** `internal/queue/store.go`
- **Severity:** LOW
- **Mechanism:** Jobs use `claim_token` for atomic claiming. `ClaimNextForAgent` writes a fresh token on every claim; `CompleteAttempt` matches it. This prevents duplicate pickup.
- **Test:** Verify that two workers racing to claim the same job results in only one success (ErrJobAlreadyClaimed for the loser).
- **Evidence:** Source code at `internal/queue/store.go` defines `claim_token` column and `ErrJobAlreadyClaimed` error.

## Checked and Clean

- `internal/scheduler/scheduler.go` - Minimal wrapper, no state machine logic found
- `internal/queue/queue.go` - Thin wrapper around store, no reserve/lease leaks
- `internal/plan/compiler.go` - Plan compilation logic, no state machine transitions
- `internal/plan/parser.go` - Plan parsing, no state machine logic
- `internal/employee/types.go` - Data types, no state machine transitions

## Gates Run

No gates were executed during this audit (read-only investigation).
