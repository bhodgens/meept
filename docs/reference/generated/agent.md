# agent

Agent configuration including progress streaming, caching, error handling, review workflow, validation, and watchdog monitoring.


## Example

```toml
[agent] progress_enabled = true
```


## Fields

| Field | Type | Description | Default |
|-------|------|-------------|---------|
| ProgressEnabled | bool | ProgressEnabled turns on streaming progress updates |  |
| ProgressIntervalSeconds | int | ProgressIntervalSeconds is the minimum interval between progress events |  |
| Cache | CacheConfig | Cache holds tool result caching settings |  |
| Errors | ErrorsConfig | Errors holds error handling settings |  |
| Review | ReviewConfig | Review holds code review settings |  |
| Validation | ValidationConfig | Validation holds task completion validation settings |  |
| Watchdog | WatchdogConfig | Watchdog holds agent monitoring settings |  |

