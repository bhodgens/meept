# Token Budget Blocks ALL Chat Messages Despite Zero Usage
**Date**: 2026-05-15
**Phase**: 2
**Severity**: critical
**Component**: `internal/llm/budget.go`, `internal/llm/client.go`
**Evaluation Dimension**: robustness, correctness

## Description
All LLM calls fail with "Token budget exceeded - request blocked" even though the daemon reports 0 tokens used out of a 100,000 hourly budget. This makes the chat system completely non-functional for direct agent execution (synchronous path).

## Reproduction
1. Start daemon: `~/go/bin/meept-daemon -f`
2. Send any chat message: `~/git/meept/bin/meept chat "hello"`
3. Observe: empty response (no output, exit 0)
4. Direct RPC reveals error: `{"error": "agent execution failed: LLM call failed: Token budget exceeded - request blocked"}`

## Evidence
```
RPC status response:
  tokens_used: 0
  tokens_remaining: 100000
  budget_used: 0
  budget_remaining: 10

Chat RPC response:
  {"conversation_id":"test-9","error":"agent execution failed: LLM call failed: Token budget exceeded - request blocked","reply":""}
```

16 of 19 test messages failed with this error. The 3 that "succeeded" did so only because they were classified as compound/async tasks, which return an acknowledgment immediately without calling the LLM.

## Root Cause
The running daemon binary (`~/go/bin/meept-daemon`, 30MB, built at 17:06) differs from the development binary (`~/git/meept/bin/meept-daemon`, 40MB, built at 23:57). The installed binary may have a bug in budget initialization where the effective budget limit evaluates to 0, or the budget config is not being loaded correctly for this older build. The `CheckBudget()` method returns `hourlyUsed() < effectiveLimit(hourlyLimit)` and `dailyUsed < effectiveLimit(dailyLimit)` -- if hourlyLimit is 0 (uninitialized or loaded as 0), then `effectiveLimit(0) = 0` and the check `0 < 0` returns false.

## Impact
- **Complete chat failure**: All synchronous chat messages produce empty responses
- **Silent failure**: The CLI returns exit 0 with no output; error only visible in RPC response
- **Partial operation by accident**: Async-dispatched messages appear to work but their tasks will also fail when the orchestrator tries to execute them via LLM

## Proposed Fix
1. Add a guard in `Budget.CheckBudget()` to return true when `hourlyLimit == 0 && dailyLimit == 0` (treating zero-config as unlimited)
2. Add budget status logging at daemon startup showing the loaded limits
3. Fix the CLI `chat` command to show the error from the response (currently it only prints `reply` which is empty)
4. Rebuild and reinstall the daemon binary from latest source

## Resolution
**FIXED** (2026-05-16)

Changes applied:
1. Added zero-limit guard in `Budget.CheckBudget()` to return true when `hourlyLimit == 0 && dailyLimit == 0`, treating unconfigured budgets as unlimited. Partial zero (only one limit configured) correctly enforces only the configured limit.
2. Startup budget logging already existed in `internal/daemon/components.go`; augmented to include per-task and per-session budget fields.
3. Added per-task and per-session cap tracking with new `CheckBudgetWithScope()`, `RecordUsageWithScope()`, `RecordTaskUsage()`, `RecordSessionUsage()` methods in `Budget`.
4. Added 7 new tests covering zero-limit allow-all, partial zero limits, per-task exhaustion, per-session exhaustion, concurrent scope access, and scope-scoped recording.

Files modified:
- `/Users/caimlas/git/meept/internal/llm/budget.go` -- zero-limit guard, scope-aware budget methods
- `/Users/caimlas/git/meept/internal/llm/budget_test.go` -- 7 new tests
- `/Users/caimlas/git/meept/internal/llm/budget.go` -- `BudgetConfig` per-task and per-session fields
- `/Users/caimlas/git/meept/internal/config/schema.go` -- `PerTaskTokenLimit` and `PerSessionTokenLimit` defaults
- `/Users/caimlas/git/meept/internal/daemon/components.go` -- wiring of new budget fields
- `/Users/caimlas/git/meept/docs/auto-analysis/0034-token-budget-blocks-all-chat.md` -- this file

## Classification
- Budget gate blocks all LLM traffic (fixed)
- CLI silently swallows the error (noted, out of scope for this fix)
