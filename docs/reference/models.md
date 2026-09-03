# Models Configuration Reference

Meept uses `models.json5` to define providers, models, aliases, and routing behavior. The config file lives at `~/.meept/models.json5` (user-level) and falls back to `config/models.json5` (bundled template).

## Overview

Models are organized in layers:

1. **Providers** — API endpoints (OpenAI, Anthropic, Ollama, etc.)
2. **Models** — Individual model entries with capabilities, pricing, and limits
3. **Aliases** — Failover rotation groups with cooldown-based backoff
4. **Slots** — Named model references (`model`, `small_model`, `classifier_model`, etc.)

## Root Configuration

```json5
{
  // Default chat model (provider/model-id or alias name)
  "model": "zai/glm-5.2",

  // Fast/cheap model for classification, summarization
  "small_model": "local/lfm-1.2b-q8",

  // Model for intent classification (falls back to small_model)
  "classifier_model": "classifier",

  // Model for session summarization (falls back to small_model)
  "summarizer_model": "summarizer",

  // Vision/language tasks (opportunistic loading)
  "vision_model": "ollama/llama3.2",

  // Image generation model
  "image_model": "xai-oauth/grok-imagine-image-2.0",

  // Video generation model
  "video_model": "xai-oauth/grok-imagine-video",

  // Model aliases with cooldown-based failover
  "model_aliases": { ... },

  // Global HTTP timeout in seconds
  "default_timeout": 3000,

  // Providers to skip entirely
  "disabled_providers": ["gala-mlx", "gala-llama"],

  "providers": { ... }
}
```

## Provider Configuration

Each provider defines an API endpoint and the models it hosts:

```json5
"providers": {
  "zai": {
    "api": "openai",
    "options": {
      "baseURL": "https://api.z.ai/api/coding/paas/v4",
      "apiKey": "${ZAI_API_KEY}"
    },
    "models": {
      "glm-5.2": {
        "name": "glm-5.2",
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
}
```

### Provider Fields

| Field | Type | Description |
|-------|------|-------------|
| `api` | string | Transport protocol (`openai`, `anthropic_messages`, `gemini`, `comfyui`, `infsh`, `bedrock_converse`) |
| `options.baseURL` | string | API endpoint URL |
| `options.apiKey` | string | API key (supports `${ENV_VAR}` substitution) |
| `models` | map | Model definitions keyed by model ID |

## Model Fields

Each model entry supports these fields:

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `name` | string | (required) | Model identifier sent to the API |
| `capabilities` | array | (required) | Feature tags: `completion`, `code`, `reasoning`, `tool_use`, `extended_thinking`, `image`, `video` |
| `input_cost` | float | 0.0 | Cost per million input tokens (USD) |
| `output_cost` | float | 0.0 | Cost per million output tokens (USD) |
| `context_limit` | int | (required) | Maximum context window size |
| `max_output` | int | (required) | Maximum completion tokens |
| `temperature` | float | 0.7 | Sampling temperature |
| `top_p` | float | — | Nucleus sampling parameter |
| `frequency_penalty` | float | — | Frequency penalty |
| `presence_penalty` | float | — | Presence penalty |
| `stop_sequences` | array | — | Stop sequence strings |
| `max_concurrency` | int | 0 | Max concurrent requests (0 = unlimited) |
| `timeout` | int | 0 | Per-request HTTP timeout in seconds (0 = use `default_timeout`) |
| `tool_constraint` | string | "" | Grammar constraint mode: `"llamacpp"`, `"vllm"`, `"json_schema"` |
| `schema_mode` | string | "" | Tool schema mode: `"full"` or `"indexed"` |
| `oauth_provider` | string | "" | OAuth provider for token-based auth |
| `extra_headers` | map | — | Additional HTTP headers sent with every request. Value `"${session_id}"` is substituted with the current turn's session ID at request time; empty values are omitted. Model-level entries merge over provider-level `options.extra_headers` per key |
| `default_reasoning` | object | — | Thinking/reasoning config (see below) |
| `prompt_cache` | object | — | Prompt caching config (see below) |
| `lifecycle` | object | — | Local runtime management (see Lifecycle section) |

### Capabilities

| Capability | Description |
|------------|-------------|
| `completion` | General text completion |
| `code` | Programming and code generation |
| `reasoning` | Complex problem solving |
| `tool_use` | Tool calling and function usage |
| `extended_thinking` | Chain-of-thought reasoning |
| `image` | Image generation |
| `video` | Video generation |

## Model Aliases

Aliases provide failover rotation with health tracking:

```json5
"model_aliases": {
  "coder": {
    "models": [
      "zai/glm-5.2",
      "ollama/llama3.2"
    ],
    "timeout": 30,
    "max_fails": 3
  }
}
```

### Alias Fields

| Field | Type | Description |
|-------|------|-------------|
| `models` | array | Ordered list of `provider/model-id` references (priority order) |
| `timeout` | int | Base cooldown in seconds after failure |
| `max_fails` | int | Consecutive failures before rotating to next model |

### Cooldown Behavior

When a model fails, the alias enters exponential backoff:

- **Cooldown duration**: `timeout * 2^(fails-1)`, capped at 1024x
- **Example**: `timeout: 30`, `max_fails: 3`
  - Fail 1→2: 30s cooldown
  - Fail 2→3: 60s cooldown
  - Fail 3+: 120s, 240s, ... up to 30,720s

When cooldown expires, the model gets a fresh attempt. Non-current models are always available for rotation.

### Use Cases

- **Failover**: Primary model unavailable → automatic fallback
- **Cost optimization**: Cheap local model first, expensive cloud fallback
- **Latency optimization**: Fast model primary, high-quality model fallback

## Reasoning Configuration

Models can declare default reasoning/thinking behavior:

```json5
"glm-5.2": {
  "name": "glm-5.2",
  "capabilities": ["completion", "code", "reasoning"],
  "default_reasoning": {
    "effort": "high",
    "budget_tokens": 16000
  }
}
```

### Reasoning Fields

| Field | Type | Description |
|-------|------|-------------|
| `effort` | string | Reasoning tier: `"none"`, `"low"`, `"medium"`, `"high"`, `"xhigh"`, `"max"` |
| `budget_tokens` | int | Explicit thinking budget in tokens |
| `enabled` | bool | Force thinking on/off |
| `force` | bool | Bypass capability gate (warns if model lacks reasoning tag) |

### Vendor-Specific Mapping

| Vendor | Field | Effort Tiers |
|--------|-------|--------------|
| OpenAI / xAI / Gemini | `reasoning_effort` | low, medium, high, xhigh, max |
| Anthropic | `thinking.type` + `thinking.budget_tokens` | none, low, medium, high, xhigh, max |
| GLM / Kimi | `thinking.type` + `thinking.budget_tokens` | same |
| Qwen / llama.cpp | `enable_thinking` + `thinking_budget` | boolean + budget |
| DeepSeek | (none) | Built-in reasoning |

### Duplicate Models for Different Thinking Levels

Since each model entry has one `default_reasoning`, create duplicate entries with different names:

```json5
"providers": {
  "zai": {
    "models": {
      "glm-5.2-deep": {
        "name": "glm-5.2",
        "capabilities": ["completion", "code", "reasoning"],
        "default_reasoning": { "effort": "high", "budget_tokens": 16000 }
      },
      "glm-5.2-fast": {
        "name": "glm-5.2",
        "capabilities": ["completion", "code", "reasoning"],
        "default_reasoning": { "effort": "low" }
      }
    }
  }
}
```

Then reference `zai/glm-5.2-deep` in one alias and `zai/glm-5.2-fast` in another.

## Prompt Cache

Prompt caching is **enabled by default** for Anthropic models. When enabled and the system prompt contains a boundary marker, static sections get `cache_control: ephemeral` markers.

```json5
"claude-sonnet-4-6": {
  "name": "claude-sonnet-4-6",
  "capabilities": ["completion", "code", "reasoning", "tool_use"],
  "prompt_cache": {
    "enabled": true
  }
}
```

To disable:

```json5
"prompt_cache": {
  "enabled": false
}
```

### How It Works

1. The prompt builder inserts `__MEEPT_PROMPT_CACHE_BOUNDARY__` between static sections (constitution, tools, capabilities) and dynamic sections (memory, task context)
2. The Anthropic client splits the prompt at this boundary
3. Static blocks get `cache_control: {type: "ephemeral"}`
4. Dynamic blocks are sent without cache markers

**Note:** Prompt caching is currently only implemented for Anthropic. Other providers ignore this setting.

## Max Concurrency

Limit concurrent requests to a model:

```json5
"lfm-8b-f16": {
  "name": "lfm-8b-f16",
  "max_concurrency": 2
}
```

Use cases:
- Local LLM servers with limited GPU memory
- Rate-limited API providers
- Shared endpoints used by multiple agents

When set, the client uses a semaphore to queue excess requests.

## Lifecycle Management

Local models can be auto-managed:

```json5
"local": {
  "api": "openai",
  "options": {
    "baseURL": "http://127.0.0.1:8080/v1"
  },
  "lifecycle": {
    "runtime": "llama-cpp",
    "model_path": "/Volumes/LLMs/model.gguf",
    "auto_start": true,
    "auto_stop_on_exit": true,
    "pid_file": "~/.meept/run/llama.pid",
    "spawn_command": ["llama-server", "--model", "${MODEL_PATH}", "--port", "8080"],
    "spawn_timeout_seconds": 180,
    "health_check": {
      "endpoint": "/health",
      "interval_seconds": 10,
      "timeout_seconds": 5,
      "unhealthy_threshold": 18
    },
    "restart_policy": {
      "enabled": true,
      "max_attempts": 3,
      "cooldown_seconds": 30,
      "reset_after_seconds": 300
    }
  }
}
```

Requirements:
- `baseURL` must be a loopback address (`localhost`, `127.0.0.1`, etc.)
- At least one model using this provider must be in use (by an agent, slot, or alias)

## Agent Model Assignment

Agents select models via their `AGENT.md` frontmatter:

```yaml
---
id: coder
name: Code Specialist
model: "coder"  # alias name or provider/model-id
---
```

If empty, the agent uses the default model (`model` slot).

## Model Resolution

When resolving a model reference:

1. Check if it's an alias name → use alias resolver with rotation
2. Parse as `provider/model-id` → look up in provider configs
3. For skill-based resolution → find cheapest model with required capabilities
4. Apply capability filtering, budget checks, and health status

## CLI Commands

```bash
# Interactive setup wizard
meept models setup

# List configured models
meept models list

# Add a model interactively
meept models add

# Remove a model
meept models remove anthropic/claude-sonnet-4-6

# Set default model
meept models set-default zai/glm-5.2

# View/edit config
meept models config

# Manage credentials
meept models credentials add anthropic
meept models credentials list
```

## Budget Configuration

Token and cost limits are configured in `~/.meept/meept.json5`:

```json5
{
  "llm": {
    "budget": {
      "hourly_token_limit": 100000,
      "daily_token_limit": 1000000,
      "daily_cost_limit": 10.0,
      "hourly_cost_limit": 2.0,
      "rate_limit_rpm": 30,
      "per_task_token_limit": 50000,
      "per_session_token_limit": 100000
    }
  }
}
```

Set any limit to `0` to disable it.

## Supported Providers

| Provider | Transport | Auth | Example Models |
|----------|-----------|------|----------------|
| Anthropic | anthropic_messages | API Key | Claude Opus/Sonnet/Haiku |
| OpenAI | openai_chat | API Key | GPT-4.1, GPT-4o |
| OpenRouter | openai_chat | API Key | Multi-vendor gateway |
| Ollama | openai_chat | None | Llama, Qwen, local models |
| Z.ai | openai_chat | API Key | GLM-4.7, GLM-4.5 Air |
| Google AI | openai_chat | API Key | Gemini 2.5 Pro/Flash |
| DeepSeek | openai_chat | API Key | DeepSeek Chat/Coder |
| xAI | openai_chat | API Key | Grok 3 |
| Groq | openai_chat | API Key | Llama 3.3 70B |
| Together AI | openai_chat | API Key | Llama 3.3 70B Instruct |
| AWS Bedrock | bedrock_converse | IAM | Bedrock models |
| ComfyUI | comfyui | None | Local image generation |
| inference.sh | infsh | API Key | FAL.ai models |
