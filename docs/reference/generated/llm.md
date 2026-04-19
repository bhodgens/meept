# llm

LLM subsystem configuration including token budget, model broker, adaptive timeout, context firewall, and metrics.


## Example

```toml
[llm] budget.hourly_token_limit = 100000
```


## Fields

| Field | Type | Description | Default |
|-------|------|-------------|---------|
| Budget | BudgetConfig |  |  |
| Broker | LLMBrokerConfig |  |  |
| AdaptiveTimeout | LLMAdaptiveTimeoutConfig |  |  |
| ContextFirewall | LLMContextFirewallConfig |  |  |
| Metrics | LLMMetricsConfig |  |  |

