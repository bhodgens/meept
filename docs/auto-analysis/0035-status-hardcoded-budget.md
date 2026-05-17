# 0035: Status RPC Reports Hardcoded Token Budget (Always 0/100000)

| Field | Value |
|-------|-------|
| Date | 2026-05-16 |
| Phase | 3 |
| Severity | **High** |
| Component | `internal/rpc/server.go` (lines 325-328) |
| Evaluation Dimension | Correctness, Robustness |
| Reporter | QA Phase 3 |

## Description

The `status` RPC handler returns hardcoded values for token budget metrics instead of reading from the actual `Budget` tracker. The status always reports `tokens_used: 0, tokens_remaining: 100000, budget_used: 0.0, budget_remaining: 10.0` regardless of actual budget state.

## Reproduction

```bash
# Run several chat commands to consume tokens
~/git/meept/bin/meept chat "hello"
~/git/meept/bin/meept chat "what time is it?"
# Check status - still shows 0/100000
~/git/meept/bin/meept status
```

Status always shows:
```
Token Budget
------------
  Used:       0 / 100000 (0.0%)
  Cost:       $0.0000 / $10.0000 (0.0%)
```

## Evidence

In `internal/rpc/server.go`, the `statusHandler` function at line 307:

```go
return map[string]any{
    RPCKeyStatus:           "running",
    "tokens_used":        0,           // HARDCODED
    "tokens_remaining":   100000,      // HARDCODED
    "budget_used":        0.0,         // HARDCODED
    "budget_remaining":   10.0,        // HARDCODED
    ...
}
```

The budget tracker is available in the components but not referenced by the status handler.

## Root Cause

The status handler was likely implemented as a stub during early development and never wired to the actual `Budget` instance. The `Budget` struct has a `GetStatus()` method that returns `Status{HourlyUsed, HourlyLimit, DailyUsed, DailyLimit, ...}` but the RPC handler doesn't call it.

## Impact

- **High**: Users cannot monitor token consumption
- Budget exhaustion appears as "0/100000" which is confusing when the daemon is actually budget-locked
- Makes debugging budget exhaustion issues impossible without checking daemon logs
- Misleading operational visibility

## Proposed Fix

Wire the budget tracker to the status handler:

```go
// In RegisterDefaults, pass budget reference:
func (s *Server) SetBudget(b *llm.Budget) {
    s.budget = b
}

// In statusHandler:
if s.budget != nil {
    budgetStatus := s.budget.GetStatus()
    // Use actual values from budgetStatus
}
```

## Classification

- Type: Bug (incomplete implementation)
- Regression: No
- Priority: P1 - critical for operational monitoring
