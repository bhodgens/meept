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
[llm]
# Directory for locally pulled GGUF models (default shown)
models_dir = "~/.meept/models"

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

## Grammar-Constrained Tool Calling (GBNF)

Small local models frequently emit malformed tool-call JSON. When the
endpoint supports grammar constraints, meept can attach a grammar that makes
invalid tool-call output impossible.

### Endpoint support matrix

| Endpoint type | Constraint key | `tool_constraint` value | Notes |
|---|---|---|---|
| llama.cpp server (managed or remote) | `grammar` (GBNF) | `"llamacpp"` | Full enum-tight grammar; managed runtimes auto-declare this |
| vLLM | `guided_grammar` (GBNF) | `"vllm"` | Same GBNF grammar, different wire key |
| OpenAI-compat structured outputs | `response_format: {type:"json_schema"}` | `"json_schema"` | JSON Schema instead of GBNF; enum tightness may be lower depending on server |
| MLX-server, Ollama, most remote APIs | none | `""` (default) | No grammar attached — not an error |

### Configuration

```toml
# config.toml — global kill-switch (default false)
[agent.tools]
gbnf_constrained = true
```

```json5
// models.json5 — per-provider and per-model constraint declaration
{
  "providers": {
    "local": {
      "options": { "baseURL": "http://127.0.0.1:8080", "tool_constraint": "llamacpp" },
      "models": {
        "my-model": { "name": "...", "tool_constraint": "json_schema" } // optional override
      }
    }
  }
}
```

Behavior:
- Attach requires ALL of: `gbnf_constrained = true`, a declared matching
  `tool_constraint`, tools present on the request.
- Unsupported schema constructs exclude that tool from the grammar (never
  brick generation); the exclusion is logged once per session.
- Managed llama.cpp runtimes (`llama-cpp`) auto-declare `llamacpp`; MLX
  runtimes declare nothing.

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

## Local Model Lifecycle

`meept model` manages local GGUF model *files* (runtimes themselves are managed by `meept runtime`):

```sh
# Pull a GGUF from Hugging Face (resumable; picks the single matching quant)
meept model pull bartowski/Llama-3.2-1B-Instruct-GGUF --quant Q4_K_M
meept model pull <repo> --force        # re-download

# List pulled models
meept model list
meept model list --json

# One-token completion probe through the local runtime (starts it if needed)
meept model test <name>
```

Behavior:

- Downloads use plain HTTPS against `https://huggingface.co/api/models/<id>` and
  `.../resolve/main/<file>`. Interrupted downloads resume via a `.part` file and
  Range requests; servers without Range support restart cleanly.
- `sha256` is computed streaming during the pull; a stored file whose size or
  hash disagrees with the manifest is re-downloaded.
- If multiple `.gguf` files match, the command fails listing candidates — pass
  `--quant` to disambiguate.
- `HF_TOKEN`, when set in the environment, is sent as a bearer token (never logged).
- Pulled models register under the `local-models` provider alias on a shared
  llama.cpp-style endpoint (`127.0.0.1:8080`), making them selectable through
  normal provider/alias resolution.
- Storage location defaults to `models_dir = "~/.meept/models"` under `[llm]`.

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