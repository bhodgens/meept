# Model Aliases with Cooldown/Rotating Failover

## Overview

Model aliases allow you to define a prioritized list of models for a given purpose (e.g., "coder", "planner") with automatic failover when models fail or become slow.

## Configuration

Add a `model_aliases` section to `config/models.json5`:

```json5
{
  "model_aliases": {
    "coder": {
      "models": ["zai/glm-4.7", "ollama/llama3.2"],
      "timeout": 30,
      "max_fails": 3
    },
    "planner": {
      "models": ["zai/glm-4.5-air", "ollama/llama3.2"],
      "timeout": 15,
      "max_fails": 2
    }
  }
}
```

## Fields

| Field | Type | Description | Default |
|-------|------|-------------|---------|
| `models` | string[] | List of "provider/model-id" in priority order | Required |
| `timeout` | int | Base cooldown timeout in seconds | 30 |
| `max_fails` | int | Max consecutive failures before rotation | 3 |

## Behavior

### Exponential Backoff

When a model fails, the cooldown period follows exponential backoff:
- 1st failure: `timeout * 2^0` = 30s
- 2nd failure: `timeout * 2^1` = 60s
- 3rd failure: `timeout * 2^2` = 120s (triggers rotation if `max_fails` is 3)

### Rotation

After `max_fails` consecutive failures, the system automatically rotates to the next model in the list. If all models are exhausted, the global default model is used as a fallback.

### Advanced Features

#### Default Model Reversion

When `default_model` is set, the alias reverts to the specified model after cooldown expires instead of continuing round-robin:

```json5
"model_aliases": {
  "coder": {
    "models": ["zai/glm-4.7", "ollama/llama3.2"],
    "default_model": "zai/glm-4.7",
    "timeout": 30,
    "max_fails": 3
  }
}
```

#### Sticky Request Distribution

When `balanced_sticky_requests` is enabled, each concurrent caller (session/task) is pinned to a single model within the alias:

```json5
"model_aliases": {
  "coder": {
    "models": ["zai/glm-5.2", "ollama/llama3.2", "zai/glm-4.7"],
    "balanced_sticky_requests": true,
    "timeout": 30,
    "max_fails": 3
  }
}
```

Benefits:
- Different sessions use different models (load distribution)
- Each session sees consistent model behavior
- Pins release automatically when the pinned model enters cooldown

### Usage in Agent Specs

Agent specs can reference an alias by name in the `Model` field:

```go
CoderAgentSpec() *AgentSpec {
    return &AgentSpec{
        Model: "coder",  // Uses the "coder" alias
        // ...
    }
}
```

## API

```go
// Resolve an alias to get the current active model
modelConfig, err := resolver.ResolveForAlias("coder")

// Record a failure for cooldown tracking
resolver.RecordAliasFailure("coder", err)

// Record a success to reset failure counter
resolver.RecordAliasSuccess("coder")

// Check if an alias exists
if resolver.HasAlias("coder") { ... }
```
