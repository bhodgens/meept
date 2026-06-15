# Token Budgets Configuration

Token budgets control LLM API consumption by enforcing configurable limits on token usage and cost. The system prevents runaway spending, rate limit violations, and ensures predictable operational costs.

## What Are Token Budgets

Token budgets are limits that constrain how many tokens or how much money the LLM subsystem can spend within specified time windows. Budgets are enforced **before every LLM API call** via `CheckBudget()` in `internal/llm/client.go`. If a budget limit would be exceeded, the call is blocked and returns a `BudgetExceededError`.

## Why Use Token Budgets

- **Prevent runaway costs** from infinite agent loops or broken retry logic
- **Enforce per-task isolation** - a single broken task cannot consume the entire budget
- **Rate limit control** to avoid provider API throttling (RPM limits)
- **Dollar cost tracking** alongside token counts for financial visibility
- **Aggressiveness factor** for conservative vs. aggressive usage modes

## Configuration Options

All budget settings are in `~/.meept/meept.json5` under `llm.budget`:

```json5
{
  "llm": {
    "budget": {
      // Token limits (sliding window)
      "hourly_token_limit": 100000,      // Max tokens in sliding 1-hour window (0 = unlimited)
      "daily_token_limit": 1000000,      // Max tokens per UTC day (0 = unlimited)

      // Cost limits (USD)
      "daily_cost_limit": 10.0,          // Max dollars per UTC day (0 = no cost limit)
      "hourly_cost_limit": 2.0,          // Max dollars in sliding 1-hour window (0 = no limit)

      // Rate limiting
      "rate_limit_rpm": 30,              // Max requests per minute (0 = unlimited)
      "aggressiveness": 0.5,             // Usage factor: 0.0-1.0 (see formula below)

      // Scope-based caps
      "per_task_token_limit": 50000,     // Max tokens per single task (0 = no cap)
      "per_session_token_limit": 100000, // Max tokens per single session (0 = no cap)
      "per_task_cost_limit": 5.0,        // Max USD per single task (0 = no cap)
      "per_session_cost_limit": 10.0,    // Max USD per single session (0 = no cap)
    }
  }
}
```

### Field Descriptions

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `hourly_token_limit` | int | 100000 | Maximum tokens allowed in any sliding 1-hour window. Set to `0` to disable. |
| `daily_token_limit` | int | 1000000 | Maximum tokens allowed per UTC day (resets at midnight UTC). Set to `0` to disable. |
| `daily_cost_limit` | float | 10.0 | Maximum USD cost per UTC day. Requires model pricing in `models.json5`. Set to `0` to disable. |
| `hourly_cost_limit` | float | 2.0 | Maximum USD cost in any sliding 1-hour window. Set to `0` to disable. |
| `rate_limit_rpm` | int | 30 | Maximum requests per minute. When exceeded, requests block and wait (not rejected). |
| `aggressiveness` | float | 0.5 | Multiplier applied to all limits (see formula below). Range: 0.0-1.0. |
| `per_task_token_limit` | int | 50000 | Maximum tokens a single task can consume. Prevents one task from exhausting the budget. |
| `per_session_token_limit` | int | 100000 | Maximum tokens a single conversation session can consume. |
| `per_task_cost_limit` | float | 5.0 | Maximum USD cost a single task can incur. Prevents expensive tasks from exhausting the budget. |
| `per_session_cost_limit` | float | 10.0 | Maximum USD cost a single conversation session can incur. |

### Per-Task vs. Per-Session Budgets

The budget system supports both token-based and dollar-cost limits with per-task and per-session scoping:

**Per-Task Budgets** (`per_task_token_limit`, `per_task_cost_limit`):
- Limits apply to individual tasks executed via `RunWithTask()`
- When exceeded, the task fails with a non-retryable `BudgetExceededError`
- Budget entries are automatically cleaned up when tasks complete
- Prevents a single runaway task from consuming the entire budget

**Per-Session Budgets** (`per_session_token_limit`, `per_session_cost_limit`):
- Limits apply to conversation sessions (identified by session ID)
- Multiple tasks within the same session share the session budget
- Useful for limiting per-user or per-conversation spending
- Cleanup occurs when tasks complete to prevent unbounded map growth

**Scope Application**:
- Scoped budgets are applied when using `agent/loop.go`'s `RunWithTask()` method
- Direct chat methods (`RunOnce()`, `RunWithSkill()`) use global hourly/daily budgets only
- The agent layer automatically tracks task/session scope during execution

### The Aggressiveness Factor

The `aggressiveness` field applies a safety multiplier to all configured limits:

```
effective_limit = base_limit * (0.5 + 0.5 * aggressiveness)
```

| Aggressiveness | Effective Limit | Use Case |
|----------------|-----------------|----------|
| `0.0` | 50% of base | Very conservative, production safety margin |
| `0.3` | 65% of base | Conservative, staging environments |
| `0.5` | 75% of base | **Default** - balanced approach |
| `0.7` | 85% of base | Aggressive, development |
| `1.0` | 100% of base | Full budget utilization, trusted workloads |

**Example:** With `hourly_token_limit: 100000` and `aggressiveness: 0.5`:
- Effective hourly limit = 100,000 * (0.5 + 0.5 * 0.5) = 100,000 * 0.75 = **75,000 tokens**

## Example Configurations

### Minimal (Local Development)

For local development with Ollama or other free local models:

```json5
{
  "llm": {
    "budget": {
      "hourly_token_limit": 50000,
      "daily_token_limit": 500000,
      "rate_limit_rpm": 60,
      "aggressiveness": 1.0,
      // Cost limits disabled for local models
      "daily_cost_limit": 0,
      "hourly_cost_limit": 0,
    }
  }
}
```

### Recommended (General Use)

For general use with paid providers (OpenAI, Anthropic, OpenRouter):

```json5
{
  "llm": {
    "budget": {
      "hourly_token_limit": 100000,
      "daily_token_limit": 1000000,
      "daily_cost_limit": 10.0,
      "hourly_cost_limit": 2.0,
      "rate_limit_rpm": 30,
      "aggressiveness": 0.5,
      "per_task_token_limit": 50000,
      "per_session_token_limit": 100000,
    }
  }
}
```

### Strict (Production/Shared)

For production environments or shared infrastructure:

```json5
{
  "llm": {
    "budget": {
      "hourly_token_limit": 30000,
      "daily_token_limit": 200000,
      "daily_cost_limit": 5.0,
      "hourly_cost_limit": 1.0,
      "rate_limit_rpm": 10,
      "aggressiveness": 0.3,
      "per_task_token_limit": 10000,
      "per_session_token_limit": 50000,
    }
  }
}
```

## Per-Task vs. Per-Session Budgets

### Per-Task Budget (`per_task_token_limit`)

Limits tokens consumed by a **single task** across all its LLM calls. Useful when running many concurrent tasks to ensure one misbehaving task cannot starve others.

```
Task A: 30,000 tokens used (limit: 50,000)
Task B: 8,000  tokens used (limit: 50,000)
Task C: 52,000 tokens -> BLOCKED (exceeds per-task limit)
```

When a task hits its per-task budget, the task fails with a `BudgetExceededError` marked as `NonRetryable()`.

### Per-Session Budget (`per_session_token_limit`)

Limits tokens consumed within a **single conversation session**. Useful for multi-user deployments to cap individual user consumption.

```
Session 1 (User A): 80,000 tokens used (limit: 100,000)
Session 2 (User B): 15,000 tokens used (limit: 100,000)
Session 1 (User A): 25,000 more tokens -> BLOCKED (session exhausted)
```

## Monitoring Budget Usage

### CLI Status

Use `meept status` to view current budget status:

```bash
$ meept status

Daemon Status
-------------
  Status:     running
  PID:        12345
  Uptime:     2h 15m

Token Budget
------------
  Hourly:     45,123 / 75,000 (60.2%)
  Daily:      312,456 / 750,000 (41.7%)
  Cost:       $1.87 / $15.00 (12.5%)

Per-Task Budget
---------------
  Limit:      50,000 tokens
  Exhausted:  No

Per-Session Budget
------------------
  Limit:      100,000 tokens
  Exhausted:  No
```

### JSON Output

For programmatic access:

```bash
$ meept status --json
{
  "status": "running",
  "budget": {
    "hourly_used": 45123,
    "hourly_limit": 75000,
    "hourly_remaining": 29877,
    "daily_used": 312456,
    "daily_limit": 750000,
    "daily_remaining": 437544,
    "daily_cost_used": 1.87,
    "daily_cost_limit": 15.0,
    "daily_cost_remaining": 13.13,
    "hourly_cost_used": 0.42,
    "hourly_cost_limit": 1.5,
    "hourly_cost_remaining": 1.08,
    "within_budget": true,
    "task_budget_exhausted": false,
    "session_budget_exhausted": false
  }
}
```

### HTTP API

If HTTP transport is enabled, budget status is available at `/api/v1/status`:

```bash
$ curl -H "Authorization: Bearer YOUR_API_KEY" \
       https://localhost:8081/api/v1/status
```

## Behavior Details

### Zero Means "Unconfigured"

When **all** budget limits are `0`, the budget system allows all requests without tracking:

```json5
{
  "hourly_token_limit": 0,
  "daily_token_limit": 0,
  "daily_cost_limit": 0,
  "hourly_cost_limit": 0
}
// Result: All requests allowed, no budget enforcement
```

**Partial zeros work correctly:** If only `hourly_token_limit` is set and others are `0`, only the hourly limit is enforced.

### Daily Reset at Midnight UTC

Daily counters reset at **midnight UTC**, not local midnight. The system uses `dayOrdinal()` which computes `int(t.Unix() / 86400)` for cross-platform consistency.

At daily reset:
- `dailyUsed` counter resets to 0
- `dailyCostUsed` resets to 0.0
- Hourly windows are also cleared

### RPM Rate Limiting

When `rate_limit_rpm > 0`, the system uses a **sliding window with slot reservation**:

- Requests within the limit proceed immediately
- When all slots are taken, callers **block and wait** (not rejected)
- Uses atomic slot reservation to prevent concurrent over-subscription

### Cost Tracking Requirements

For dollar cost limits to work, models must have pricing defined in `~/.meept/models.json5`:

```json5
{
  "models": [
    {
      "id": "claude-sonnet-4-5-20241022",
      "provider": "anthropic",
      "input_cost": 0.000003,
      "output_cost": 0.000015
    }
  ]
}
```

Without pricing data, cost limits cannot be enforced (but token limits still work).

## Troubleshooting

### Common Error Messages

All budget errors implement `NonRetryableError` - the agent will **not** retry and must adjust its approach.

#### Hourly Token Budget Exceeded

```
meept hourly token budget reached: 75000 / 75000 tokens used
(config: llm.budget.hourly_token_limit)
```

**Fix:** Wait for the hourly window to roll off, or increase `hourly_token_limit`.

#### Daily Token Budget Exceeded

```
meept daily token budget reached: 1200000 / 1000000 tokens used
(config: llm.budget.daily_token_limit)
```

**Fix:** Wait until midnight UTC, or increase `daily_token_limit`.

#### Cost Budget Exceeded

```
meept hourly cost budget reached: $2.10 / $2.00 used
(config: llm.budget.hourly_cost_limit)
```

**Fix:** Wait for the window to expire, switch to cheaper models, or increase cost limits.

#### Per-Task Budget Exceeded

```
meept per-task token budget reached: 52000 / 50000 tokens used
(config: llm.budget.per_task_token_limit)
```

**Fix:** Split the task into smaller subtasks, or increase `per_task_token_limit`.

#### Per-Session Budget Exceeded

```
meept per-session token budget reached: 105000 / 100000 tokens used
(config: llm.budget.per_session_token_limit)
```

**Fix:** Start a new conversation session, or increase `per_session_token_limit`.

#### Per-Task Cost Budget Exceeded

```
meept per-task cost budget reached: $5.50 / $5.00 used
(config: llm.budget.per_task_cost_limit)
```

**Fix:** Split the task into smaller subtasks, or increase `per_task_cost_limit`.

#### Per-Session Cost Budget Exceeded

```
meept per-session cost budget reached: $12.00 / $10.00 used
(config: llm.budget.per_session_cost_limit)
```

**Fix:** Start a new conversation session, or increase `per_session_cost_limit`.

### Budget Seems Wrong After Config Change

Budget config is **dynamic** - changes take effect immediately without restart. However:

- The `aggressiveness` factor applies to all limits - remember that effective limits are 75% of configured when `aggressiveness: 0.5`
- Daily reset happens at UTC midnight, not local time
- Hourly budget is a **sliding window**, not fixed hours

### Budget Enabled But Not Tracking

If budgets are configured but not being enforced:

1. Check that at least one limit is non-zero (all zeros = disabled)
2. Verify model pricing is configured for cost limits
3. Check startup logs for "LLM client initialized" showing budget values

### Runaway Agent Loop Protection

The combination of `per_task_token_limit` + `daily_cost_limit` provides strong protection against runaway agents:

```json5
{
  "per_task_token_limit": 50000,     // Single task can't go wild
  "daily_cost_limit": 10.0,          // Total daily spend capped
  "hourly_cost_limit": 2.0,          // Per-hour spend also capped
}
```

## Related Documentation

- [LLM Configuration](llm.md) - Overview of LLM subsystem config
- [Metrics Reference](../reference/metrics.md) - Budget-related metrics
- [LLM Management Workflow](../workflows/llm-management.md) - Budget management procedures

## Implementation Details

For developers interested in the budget implementation:

- Core logic: `internal/llm/budget.go`
- Error types: `internal/llm/errors.go` (`BudgetExceededError`, `NonRetryableError`)
- Config schema: `internal/config/schema.go` (`BudgetConfig`)
- Daemon wiring: `internal/daemon/components.go`
- Enforcement: `internal/llm/client.go` (`CheckBudget()` called before each API call)
