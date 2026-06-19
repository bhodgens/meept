# LLM Configuration

Meept supports multiple LLM providers and models with capability-based resolution and budget management.

## Configuration Files

LLM configuration uses two files:

- `~/.meept/meept.toml` - Budget and broker settings
- `~/.meept/models.json5` - Provider and model definitions

## Models Configuration (models.json5)

The `models.json5` file defines providers, models, and their capabilities.

### Root Configuration

```json5
{
  // Default model for general use (provider/model-id format)
  "model": "zai/glm-4.7",

  // Fast/cheap model for classification, summarization
  "small_model": "zai/glm-4.5-air",

  // Model for intent classification (defaults to small_model)
  "classifier_model": "zai/glm-4.5-air",

  // Model aliases with cooldown-based failover
  "model_aliases": {
    "coder": {
      "models": ["zai/glm-4.7", "ollama/llama3.2"],
      "timeout": 30,
      "max_fails": 3
    }
  },

  // Default timeout for all providers (in seconds)
  "default_timeout": 3000,

  // Providers to skip entirely
  "disabled_providers": ["gala-mlx", "gala-llama"],

  "providers": {
    // Provider definitions...
  }
}
```

### Provider Configuration

Each provider is configured with API settings and model definitions:

```json5
"zai": {
  "api": "openai",
  "options": {
    "baseURL": "https://api.z.ai/api/coding/paas/v4",
    "apiKey": "${ZAI_API_KEY}"
  },
  "models": {
    "glm-4.7": {
      "name": "glm-4.7",
      "capabilities": ["completion", "code", "reasoning", "tool_use"],
      "input_cost": 0.0,
      "output_cost": 0.0,
      "context_limit": 128000,
      "max_output": 8192,
      "temperature": 0.7,
      "top_p": 0.9
    }
  }
}
```

### Model Configuration

Each model declares capabilities and limits:

```json5
"glm-4.7": {
  "name": "glm-4.7",
  "capabilities": ["completion", "code", "reasoning", "tool_use"],
  "input_cost": 0.0,
  "output_cost": 0.0,
  "context_limit": 128000,
  "max_output": 8192,
  "temperature": 0.7,
  "top_p": 0.9,
  "max_concurrency": 2        // Max concurrent requests (0 = unlimited)
}
```

**Model Fields:**

| Field | Description | Default |
|-------|-------------|---------|
| `name` | Model identifier for the API | Required |
| `capabilities` | Supported capabilities | Required |
| `input_cost` | Cost per million input tokens | 0.0 |
| `output_cost` | Cost per million output tokens | 0.0 |
| `context_limit` | Maximum context window size | Required |
| `max_output` | Maximum completion tokens | Required |
| `temperature` | Sampling temperature | 0.7 |
| `top_p` | Nucleus sampling parameter | - |
| `max_concurrency` | Max concurrent requests to this model | 0 (unlimited) |

**Use case for `max_concurrency`:** Set this limit to prevent overwhelming:
- Local LLM servers (llama.cpp, MLX, Ollama) that have limited GPU memory
- Rate-limited API providers without proper 429 handling
- Shared model endpoints used by multiple agents simultaneously

### Model Capabilities

Models declare capabilities that determine their suitability for different tasks:

- **completion**: General text completion
- **code**: Programming and code generation
- **reasoning**: Complex problem solving
- **tool_use**: Tool calling and function usage
- **extended_thinking**: Chain-of-thought reasoning

Models declare capabilities that determine their suitability for different tasks:

- **completion**: General text completion
- **code**: Programming and code generation
- **reasoning**: Complex problem solving
- **tool_use**: Tool calling and function usage
- **extended_thinking**: Chain-of-thought reasoning

### Model Aliases

Model aliases provide cooldown-based failover for high-availability:

```json5
"model_aliases": {
  "coder": {
    "models": ["zai/glm-4.7", "ollama/llama3.2"],
    "timeout": 30,
    "max_fails": 3
  }
}
```

- **models**: Ordered list of fallback models
- **timeout**: Cooldown period after failure (seconds)
- **max_fails**: Maximum consecutive failures before switching

## Budget Configuration

For comprehensive budget documentation, see [Token Budgets Configuration](token-budgets.md).

Quick reference (`~/.meept/meept.json5`):

```json5
{
  "llm": {
    "budget": {
      "hourly_token_limit": 100000,      // Tokens per sliding hour
      "daily_token_limit": 1000000,      // Tokens per UTC day
      "daily_cost_limit": 10.0,          // Max USD per day
      "hourly_cost_limit": 2.0,          // Max USD per hour
      "rate_limit_rpm": 30,              // Max requests per minute
      "aggressiveness": 0.5,             // 0.0-1.0 usage factor
      "per_task_token_limit": 50000,     // Cap per single task
      "per_session_token_limit": 100000, // Cap per session
    }
  }
}
```

### Budget Options

| Field | Description | Default |
|-------|-------------|---------|
| `hourly_token_limit` | Maximum tokens in sliding 1-hour window | 100000 |
| `daily_token_limit` | Maximum tokens per UTC day | 1000000 |
| `daily_cost_limit` | Maximum USD cost per UTC day | 10.0 |
| `hourly_cost_limit` | Maximum USD cost per sliding hour | 2.0 |
| `rate_limit_rpm` | Maximum requests per minute | 30 |
| `aggressiveness` | Budget usage factor (0.0-1.0) | 0.5 |
| `per_task_token_limit` | Token cap per individual task | 50000 |
| `per_session_token_limit` | Token cap per conversation session | 100000 |

**Note:** Setting any limit to `0` disables that specific limit. When all limits are `0`, budget enforcement is completely disabled.

### Token Budget Enforcement

The budget system:
- Tracks token usage across all providers and models
- Enforces hourly and daily token limits
- Enforces hourly and daily dollar cost limits (requires model pricing)
- Blocks requests when limits are exceeded with `BudgetExceededError`
- Uses aggressiveness setting to apply safety margin (default 75% of configured limits)
- Implements per-task and per-session caps for isolation

See [Token Budgets Configuration](token-budgets.md) for detailed examples and troubleshooting.

## Model Broker Configuration

Advanced broker settings for model selection and fallback:

```toml
[llm.broker]
max_error_rate = 0.10        # Maximum error rate before fallback
max_p95_latency_ms = 30000   # Maximum P95 latency
fallback_enabled = true      # Enable automatic fallback
```

## Adaptive Timeout Configuration

Dynamic timeout calculation based on request characteristics:

```toml
[llm.adaptive_timeout]
enabled = true                    # Enable adaptive timeouts
stddev_multiplier = 3.0          # Multiplier for standard deviation
stddev_token_rate_timeout = true # Include token rate in timeout calculation
min_timeout_seconds = 10         # Minimum timeout
max_timeout_seconds = 300        # Maximum timeout
warmup_requests = 20             # Requests before adaptive mode
```

## Context Firewall Configuration

Context management and summarization settings:

```toml
[llm.context_firewall]
enabled = true                    # Enable context firewall
max_context_tokens = 32000       # Maximum context tokens
summarization_threshold = 0.75   # Threshold for auto-summarization
summarization_model = "small"    # Model for summarization
```

## Provider Examples

### Ollama (Local)

```json5
"ollama": {
  "api": "openai",
  "options": {
    "baseURL": "http://localhost:11434/v1"
  },
  "models": {
    "llama3.2": {
      "name": "llama3.2",
      "capabilities": ["code", "tool_use", "reasoning"],
      "input_cost": 0.0,
      "output_cost": 0.0,
      "context_limit": 128000,
      "max_output": 4096,
      "temperature": 0.7
    }
  }
}
```

### OpenRouter (External)

```json5
"openrouter": {
  "api": "openai",
  "options": {
    "baseURL": "https://openrouter.ai/api/v1",
    "apiKey": "${OPENROUTER_API_KEY}",
    "headers": {
      "HTTP-Referer": "https://github.com/your-project",
      "X-Title": "Meept"
    }
  },
  "models": {
    "claude-3-opus": {
      "name": "anthropic/claude-3-opus",
      "capabilities": ["completion", "reasoning", "extended_thinking"],
      "input_cost": 0.015,
      "output_cost": 0.075,
      "context_limit": 200000,
      "max_output": 4096
    }
  }
}
```

## Environment Variables

API keys are configured via environment variables:

```bash
export ZAI_API_KEY="your-api-key"
export OPENROUTER_API_KEY="your-key"
```

## Model Resolution Process

1. **Capability matching**: Skills declare required capabilities
2. **Cost optimization**: Select cheapest model with required capabilities
3. **Availability check**: Skip disabled providers
4. **Fallback handling**: Use model aliases for high availability
5. **Budget enforcement**: Respect token limits and rate limits

## Runtime Lifecycle Management

Meept can automatically manage local LLM runtimes (spawn on startup, health monitoring, graceful shutdown). See [LLM Runtime Lifecycle Management](llm-lifecycle.md) for configuration details.

### Localhost requirement

A provider's `lifecycle` block is only activated when its `options.baseURL` host is a loopback address (`localhost`, `127.0.0.1`, `::1`, or `0:0:0:0:0:0:0:1`). Providers with any other host (private ranges like `192.168.*` or `10.*`, public hostnames, public IPs, or missing `baseURL`) are skipped at daemon startup with a warning. This prevents the daemon from spawning subprocesses against remote or untrusted endpoints.

### Agent-gated startup

A runtime is only spawned at daemon startup when at least one of its provider's models is "in use" — referenced by an enabled agent's `model` field, one of the models.json5 slots (`model`, `small_model`, `classifier_model`, `summarizer_model`), or a `model_aliases` target. Runtimes with no in-use models are skipped with a debug log. Use `meept runtime status` to see the `would_start` verdict per provider.

### Shared process per port

Multiple providers (or multiple models within one provider) targeting the same `(runtime, host, port)` triplet share a single subprocess. The first provider registered for an endpoint wins the spawn command; later registrations merge their models into the existing process. See [LLM Runtime Lifecycle Management](llm-lifecycle.md) for the `model_paths` multi-model configuration.
