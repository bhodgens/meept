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

### Model Capabilities

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

## Budget Configuration (meept.toml)

Budget settings control LLM usage costs and rate limiting:

```toml
[llm.budget]
hourly_token_limit = 100000
daily_token_limit = 1000000
rate_limit_rpm = 30
aggressiveness = 0.5  # 0.0 = very conservative, 1.0 = use full budget
```

### Budget Options

- **hourly_token_limit**: Maximum tokens per hour
- **daily_token_limit**: Maximum tokens per day
- **rate_limit_rpm**: Maximum requests per minute
- **aggressiveness**: Budget usage strategy (0.0-1.0)

### Token Budget Enforcement

The budget system:
- Tracks token usage across all providers
- Enforces hourly and daily limits
- Automatically throttles when limits are approached
- Uses aggressiveness setting to balance cost vs. performance

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