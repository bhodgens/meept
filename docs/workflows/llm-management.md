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
- **Aggressiveness Setting**: Cost control granularity
- **Usage Tracking**: Real-time budget monitoring

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

## Edge Cases

### Provider Outage
- Automatic failover to alternative providers
- Graceful degradation of capabilities
- Health monitoring for recovery

### Budget Exceeded
- Requests blocked or throttled
- User notified of budget limits
- Alternative cost-saving suggestions

### Capability Mismatch
- No model satisfies requirements
- Fallback to closest available capability
- User notified of limitation

### Model Alias Resolution
- Alias cooldown periods enforced
- Failover rotation maintains availability
- Usage patterns optimized over time