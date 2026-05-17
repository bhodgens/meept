# RPC Status Handler Returns Hardcoded Budget Values

**Date**: 2026-05-15
**Phase**: 11 (playground integration)
**Severity**: medium
**Component**: `internal/rpc/server.go` (status handler), `internal/llm/budget.go` (budget tracker)

## Description

The RPC `status` handler returns hardcoded token budget values instead of reading from the live `Budget` instance. The status command always reports `Used: 0 / 100000 (0.0%)` and `Cost: $0.0000 / $10.0000`, regardless of actual token consumption. This makes it impossible to monitor budget exhaustion via the CLI, which is critical for diagnosing system lockout (bug 0021).

## Reproduction

1. Start the daemon
2. Send a few chat messages to consume tokens
3. Run `./bin/meept status`
4. Observe `Token Budget: Used: 0 / 100000 (0.0%)` even though the daemon log shows `hourly=78350`

## Evidence

`internal/rpc/server.go` lines 325-328:
```go
return map[string]any{
    // ...
    "tokens_used":        0,
    "tokens_remaining":   100000,
    "budget_used":        0.0,
    "budget_remaining":   10.0,
    // ...
}, nil
```

Daemon log shows actual budget usage:
```
hourly=78350 daily=78350
```

CLI status output shows:
```
Token Budget
------------
  Used:       0 / 100000 (0.0%)
  Cost:       $0.0000 / $10.0000 (0.0%)
```

## Root Cause

The status handler in `internal/rpc/server.go` (registered at line 333) does not have access to the `Budget` instance. It returns static placeholder values instead of calling `budget.GetStatus()`. The handler was likely written as a stub and never connected to the actual budget tracker.

The `Budget` is created in `internal/daemon/components.go` and passed to the LLM client via `WithBudget()`, but it is not exposed to the RPC server's status handler.

## Proposed Fix

1. Add a `Budget` field to the RPC server struct or provide a `BudgetStatusFunc func() llm.Status` callback.
2. In the status handler, call the function to get live budget data:
   ```go
   if s.budgetStatusFn != nil {
       bs := s.budgetStatusFn()
       result["tokens_used"] = bs.HourlyUsed
       result["tokens_remaining"] = bs.HourlyRemaining
       result["budget_used"] = /* cost calculation */
       result["budget_remaining"] = /* cost remaining */
   }
   ```
3. Wire the budget tracker through the daemon component initialization to the RPC server constructor.

## Model vs Harness
[X] Harness bug  [ ] Model quality issue  [ ] Both
