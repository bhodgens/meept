# Metrics & Observability

Meept provides comprehensive metrics collection and observability features to monitor system health, performance, and resource usage.

## Overview

The metrics system collects data from various components and provides insights into:
- Agent performance and behavior
- Tool execution statistics
- Memory usage and effectiveness
- Resource consumption
- Error rates and patterns

## Metrics Store

Meept uses a SQLite-backed metrics store for persistent metric collection.

### Configuration

```toml
[metrics]
enabled = true
store_path = "~/.meept/metrics.db"
retention_days = 30
flush_interval_seconds = 60

[metrics.agent]
enable_loop_metrics = true
enable_tool_metrics = true
enable_memory_metrics = true

[metrics.llm]
enable_token_tracking = true
enable_cost_tracking = true
enable_error_tracking = true
```

### Schema

Key metric tables:
- `agent_iterations` - Agent loop performance
- `tool_executions` - Tool usage and timing
- `memory_operations` - Memory access patterns
- `llm_requests` - LLM API usage and costs
- `security_events` - Security-related metrics

## Adaptive Timeout Behavior

Meept implements adaptive timeouts based on historical performance.

### Configuration

```toml
[timeouts]
adaptive_enabled = true
baseline_timeout_seconds = 300
learning_window_hours = 24
confidence_threshold = 0.8

[timeouts.agents]
dispatcher = 60
chat = 300
coder = 600
planner = 180
analyst = 300
```

### Adaptive Logic

1. **Baseline**: Start with configured timeout
2. **Learning**: Track completion times over 24-hour window
3. **Adjustment**: Calculate 95th percentile completion time
4. **Confidence**: Apply adjustment when confidence > 80%
5. **Safety**: Never exceed maximum timeout limits

## Debug Mode

Enable detailed debugging with `--log-level debug` or `--debug` flag.

### Debug Information Collected

- **Agent Loop**: Iteration counts, tool calls, token usage
- **Tool Execution**: Parameters, results, durations
- **Memory Operations**: Search patterns, hit rates, context injection
- **LLM Interactions**: Request/response timing, token counts
- **Security Events**: Permission checks, blocked operations

### Example Debug Output

```json
{
  "level": "DEBUG",
  "msg": "Agent iteration completed",
  "agent_id": "coder",
  "iteration": 5,
  "tool_calls": 3,
  "tokens_used": 245,
  "duration_ms": 1200,
  "tools": ["file_read", "shell_execute", "file_write"],
  "memory_refs_injected": 5,
  "context_size_tokens": 1500
}
```

## Agent Loop Metrics

### Key Metrics Collected

#### Iteration Statistics
- `iterations_total` - Total iterations across all agents
- `iterations_completed` - Successfully completed iterations
- `iterations_timed_out` - Iterations that exceeded timeout
- `iterations_failed` - Iterations that failed with errors

#### Tool Usage
- `tools_executed_total` - Total tool executions
- `tool_duration_ms` - Execution time per tool type
- `tool_success_rate` - Success percentage per tool
- `tool_errors` - Error counts by tool and error type

#### Token Usage
- `tokens_used_total` - Total tokens consumed
- `tokens_per_iteration` - Average tokens per iteration
- `tokens_by_agent` - Token usage breakdown by agent
- `token_efficiency` - Tokens per successful completion

### Metric Examples

```sql
-- Recent agent performance
SELECT
    agent_id,
    COUNT(*) as iterations,
    AVG(duration_ms) as avg_duration,
    AVG(tokens_used) as avg_tokens,
    AVG(tool_calls) as avg_tools
FROM agent_iterations
WHERE timestamp > datetime('now', '-1 hour')
GROUP BY agent_id;

-- Tool success rates
SELECT
    tool_name,
    COUNT(*) as executions,
    SUM(CASE WHEN success = 1 THEN 1 ELSE 0 END) as successes,
    AVG(duration_ms) as avg_duration
FROM tool_executions
WHERE timestamp > datetime('now', '-24 hours')
GROUP BY tool_name;
```

## Memory Statistics

### Memory Metrics

- `memory_stores_total` - Total memories stored
- `memory_searches_total` - Total search operations
- `memory_search_duration_ms` - Average search time
- `memory_hit_rate` - Percentage of successful searches
- `memory_context_injections` - Memories injected into context
- `memory_consolidations` - Memory consolidation operations

### Memory Effectiveness

- `recall_rate` - How often relevant memories are found
- `precision_rate` - How often found memories are actually relevant
- `context_relevance_score` - Average relevance of injected memories

## Budget Tracking

### LLM Cost Tracking

```toml
[llm.budget]
hourly_token_limit = 100000
daily_token_limit = 1000000
rate_limit_rpm = 30
aggressiveness = 0.5
```

### Cost Metrics

- `tokens_used_hourly` - Tokens used in current hour
- `tokens_used_daily` - Tokens used in current day
- `cost_estimated` - Estimated cost based on provider rates
- `budget_utilization` - Percentage of budget used

### Alerts

```toml
[alerts]
enable_budget_alerts = true
budget_warning_threshold = 0.8  # 80% utilization
budget_critical_threshold = 0.9  # 90% utilization

[alerts.channels]
console = true
log = true
notification = true
```

## Key Debugging Commands

### CLI Commands

```bash
# Check current metrics
meept metrics status

# View recent agent performance
meept metrics agents --hours 24

# Check tool usage patterns
meept metrics tools --days 7

# Monitor memory effectiveness
meept metrics memory --detailed

# Check budget utilization
meept metrics budget
```

### SQL Queries for Advanced Analysis

```sql
-- Find slowest tools
SELECT tool_name, AVG(duration_ms) as avg_duration
FROM tool_executions
WHERE timestamp > datetime('now', '-7 days')
GROUP BY tool_name
ORDER BY avg_duration DESC
LIMIT 10;

-- Agent success rates
SELECT
    agent_id,
    COUNT(*) as total_iterations,
    SUM(CASE WHEN status = 'completed' THEN 1 ELSE 0 END) as successes,
    (successes * 100.0 / total_iterations) as success_rate
FROM agent_iterations
WHERE timestamp > datetime('now', '-1 day')
GROUP BY agent_id;

-- Memory search patterns
SELECT
    query_type,
    COUNT(*) as searches,
    AVG(result_count) as avg_results,
    AVG(duration_ms) as avg_duration
FROM memory_operations
WHERE operation_type = 'search'
AND timestamp > datetime('now', '-7 days')
GROUP BY query_type;
```

## Performance Optimization

### Identifying Bottlenecks

1. **Tool Execution Time**: Look for tools with high average duration
2. **Memory Search Performance**: Monitor search times and hit rates
3. **LLM Response Time**: Track token generation speed
4. **Context Management**: Watch context size and injection times

### Optimization Strategies

```toml
[optimization]
# Reduce context size for faster iterations
max_context_tokens = 4000
max_memory_refs = 15

# Cache frequently used tools
tool_cache_enabled = true
tool_cache_ttl_minutes = 30

# Pre-warm common memory searches
memory_prefetch_enabled = true
prefetch_patterns = ["common", "frequent"]
```

## Health Monitoring

### Key Health Indicators

- **Agent Availability**: All agents responding within timeout
- **Tool Reliability**: High success rates for critical tools
- **Memory Effectiveness**: Good recall and precision rates
- **LLM Connectivity**: Low error rates and fast responses
- **Resource Usage**: Reasonable memory and CPU consumption

### Health Check Endpoints

```bash
# Basic health check
meept status

# Detailed component health
meept metrics health --detailed

# System resource usage
meept metrics resources
```

## Alerting Configuration

### Alert Rules

```toml
[alerts.rules]

# Agent performance
[[alerts.rules.agent_timeout]]
name = "Agent timeout rate high"
metric = "agent_timeout_rate"
threshold = 0.1  # 10% timeout rate
window = "1h"
severity = "warning"

[[alerts.rules.agent_timeout]]
threshold = 0.2  # 20% timeout rate
severity = "critical"

# Tool errors
[[alerts.rules.tool_errors]]
name = "High tool error rate"
metric = "tool_error_rate"
threshold = 0.05  # 5% error rate
window = "1h"
severity = "warning"

# Memory effectiveness
[[alerts.rules.memory_recall]]
name = "Low memory recall rate"
metric = "memory_recall_rate"
threshold = 0.3  # 30% recall rate
window = "24h"
severity = "warning"

# Budget alerts
[[alerts.rules.budget_usage]]
name = "Budget usage high"
metric = "budget_utilization"
threshold = 0.8  # 80% utilization
window = "1h"
severity = "warning"
```

### Notification Channels

```toml
[alerts.notifications]

# Console output
[[alerts.notifications.console]]
enabled = true

# Log file
[[alerts.notifications.log]]
enabled = true
level = "WARN"

# System notifications
[[alerts.notifications.system]]
enabled = true

# Custom webhook
[[alerts.notifications.webhook]]
enabled = false
url = "https://hooks.example.com/alerts"
```

## Integration with External Monitoring

### Prometheus Export

```toml
[metrics.prometheus]
enabled = true
port = 9090
path = "/metrics"
update_interval_seconds = 30
```

### Grafana Dashboard

Example dashboard queries:

```sql
-- Agent performance over time
SELECT
    time_bucket('1h', timestamp) as time,
    agent_id,
    AVG(duration_ms) as avg_duration
FROM agent_iterations
WHERE $__timeFilter(timestamp)
GROUP BY time, agent_id
ORDER BY time;

-- Tool usage patterns
SELECT
    time_bucket('6h', timestamp) as time,
    tool_name,
    COUNT(*) as executions
FROM tool_executions
WHERE $__timeFilter(timestamp)
GROUP BY time, tool_name
ORDER BY time;
```

## Troubleshooting Common Issues

### High Token Usage

**Symptoms:**
- Rapid budget consumption
- Slow response times
- Context window exceeded errors

**Solutions:**
- Reduce `max_context_tokens`
- Improve memory search precision
- Use cheaper models for simple tasks
- Implement response length limits

### Slow Tool Execution

**Symptoms:**
- High iteration durations
- Timeout errors
- Poor user experience

**Solutions:**
- Enable tool caching
- Optimize frequently used tools
- Increase timeout limits selectively
- Implement parallel execution where possible

### Memory Ineffectiveness

**Symptoms:**
- Low recall/precision rates
- Irrelevant context injection
- Repeated information requests

**Solutions:**
- Improve memory storage quality
- Tune search relevance thresholds
- Implement memory consolidation
- Use hybrid search (keyword + vector)