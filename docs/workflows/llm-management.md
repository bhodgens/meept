# LLM Provider Management

## Overview
Meept supports multiple LLM providers with intelligent model resolution, failover mechanisms, budget tracking, and native Anthropic driver support. The system optimizes for cost, capability matching, and reliability.

## Problem
Different tasks require different LLM capabilities and cost profiles. LLM management addresses:
- Multi-provider support with consistent interfaces
- Capability-based model selection
- Cost optimization and budget control
- Reliability through failover mechanisms

## Behavior

### Multi-Provider Support
- **OpenAI**: GPT models via API
- **Anthropic**: Claude models with native driver
- **Google**: Gemini models
- **Ollama**: Local models
- **Custom**: OpenAI-compatible endpoints

### Model Resolution
- **Skill Requirements**: Skills declare `requires: [code, reasoning]`
- **Model Capabilities**: Models declare `capabilities: [code, tool_use]`
- **Cost Optimization**: Cheapest capable model selected
- **Automatic Fallback**: Retryable errors trigger failover

### Token Budgeting
- **Hourly/Daily Limits**: Configurable token ceilings
- **Rate Limiting**: Requests per minute control
- **Aggressiveness Setting**: Cost control granularity (0.0-1.0)
- **Usage Tracking**: Real-time budget monitoring
- **Cost Limits**: Dollar-based budget caps (requires model pricing)
- **Per-Task Caps**: Prevent single task from exhausting budget
- **Per-Session Caps**: Limit individual conversation consumption

See [Token Budgets Configuration](../configuration/token-budgets.md) for detailed setup.

### Native Anthropic Driver
- **Messages API**: Native implementation
- **Extended Thinking**: Mode support
- **Streaming**: Progress callbacks
- **SSE Parsing**: Real-time updates

## Configuration

```toml
[llm.budget]
hourly_token_limit = 100000
daily_token_limit = 1000000
rate_limit_rpm = 30
aggressiveness = 0.5

[llm.providers.anthropic]
base_url = "https://api.anthropic.com"
api_key_env = "ANTHROPIC_API_KEY"

[llm.providers.openai]
base_url = "https://api.openai.com/v1"
api_key_env = "OPENAI_API_KEY"

[llm.providers.ollama]
base_url = "http://localhost:11434"
api_key_env = ""
```

### Models Configuration (`config/models.json5`)
```json5
{
  providers: {
    anthropic: {
      base_url: "https://api.anthropic.com",
      api_key_env: "ANTHROPIC_API_KEY",
      models: {
        "claude-opus-4-5-20251101": {
          capabilities: ["code", "tool_use", "extended_thinking"],
          max_tokens: 8192,
        }
      }
    }
  }
}
```

## Observability

### Logging
- Model selection decisions
- Provider API calls
- Budget usage events
- Failover triggers

### Metrics
- Model utilization rates
- API response times
- Token consumption rates
- Budget utilization percentages

### Debug Info
- Available models and capabilities
- Current budget status
- Provider health status
- Model alias mappings


## Budget Management Workflow

### Check Current Budget Status

```bash
# View budget status via CLI
meept status

# JSON output for programmatic access
meept status --json
```

### Adjust Budget Limits Dynamically

Budget limits are **dynamic** - changes take effect immediately without daemon restart:

1. Edit `~/.meept/meept.json5`
2. Modify `llm.budget` section
3. Changes apply to next LLM call (no restart needed)

```json5
{
  "llm": {
    "budget": {
      "hourly_token_limit": 50000,   // Reduce for testing
      "daily_cost_limit": 5.0,        // Strict daily cap
      "aggressiveness": 0.3,          // More conservative
    }
  }
}
```

### Per-Task Budget Isolation

When running many concurrent tasks, set `per_task_token_limit` to prevent one task from consuming the entire budget:

```json5
{
  "per_task_token_limit": 25000,  // Each task limited to 25k tokens
}
```

Benefits:
- Prevents runaway tasks from starving others
- Forces task decomposition for large jobs
- Predictable per-task cost bounds

### Per-Session Budget Caps

For multi-user deployments, limit individual session consumption:

```json5
{
  "per_session_token_limit": 50000,  // Each conversation session capped
}
```

### Cost Tracking Setup

1. Ensure models have pricing in `~/.meept/models.json5`:
```json5
{
  "models": [{
    "id": "claude-sonnet-4-5-20241022",
    "provider": "anthropic",
    "input_cost": 0.000003,
    "output_cost": 0.000015
  }]
}
```

2. Enable cost limits:
```json5
{
  "daily_cost_limit": 10.0,   // $10/day max
  "hourly_cost_limit": 2.0,   // $2/hour max
}
```

### Rate Management

Set `rate_limit_rpm` to pace API calls and avoid provider rate limits:

```json5
{
  "rate_limit_rpm": 30,  // Max 30 requests/minute
}
```

When exceeded, requests **block and wait** (not rejected) until capacity is available.

### Tuning Aggressiveness

The `aggressiveness` factor applies a safety multiplier to all limits:

```
effective_limit = base_limit * (0.5 + 0.5 * aggressiveness)
```

| Use Case | Aggressiveness | Effective Limit |
|----------|----------------|-----------------|
| Production safety | 0.0-0.3 | 50-65% of base |
| Default (balanced) | 0.5 | 75% of base |
| Development | 0.7-1.0 | 85-100% of base |

### Monitoring and Alerts

Watch for budget warnings in daemon logs:
```
WARN budget hourly limit approaching (85% used)
ERROR budget daily cost exceeded: $10.00 / $10.00
```

Use `meept status` periodically or integrate with monitoring via the JSON output.

## Edge Cases

### Provider Outage
- Automatic failover to alternative providers
- Graceful degradation of capabilities
- Health monitoring for recovery

### Budget Exceeded
- Requests blocked with `BudgetExceededError` (non-retryable)
- User notified with specific limit exceeded (hourly/daily/task/session)
- CLI and API return descriptive error messages
- Alternative: lower aggressiveness, split tasks, or increase limits

### Capability Mismatch
- No model satisfies requirements
- Fallback to closest available capability
- User notified of limitation

### Model Alias Resolution
- Alias cooldown periods enforced
- Failover rotation maintains availability
- Usage patterns optimized over time