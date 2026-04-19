# Logging Reference

Meept uses structured logging with Go's `log/slog` package for comprehensive observability and debugging.

## Overview

Logging is configured to provide detailed insights into system operations while maintaining performance. The system uses structured logging with key-value pairs for better filtering and analysis.

## Log Levels

Meept supports standard log levels:

- **DEBUG** - Detailed debugging information
- **INFO** - General operational information
- **WARN** - Warning conditions that don't require immediate action
- **ERROR** - Error conditions that require attention

### Default Log Level

- **Daemon**: INFO (configurable via `[daemon] log_level`)
- **CLI**: WARN (unless `--debug` flag is used)

## Log Configuration

### Configuration File

Logging can be configured in `~/.meept/meept.toml`:

```toml
[daemon]
log_level = "INFO"  # DEBUG, INFO, WARN, ERROR

[logging]
# File-based logging (when running in background)
file_path = "~/.meept/meept.log"
max_size_mb = 100
max_backups = 5
max_age_days = 30

# Console logging format
format = "text"  # text or json
color = true
```

### Environment Variables

```bash
# Set log level
MEEPT_LOG_LEVEL=DEBUG

# Log to file
MEEPT_LOG_FILE=/path/to/logfile.log

# JSON format
MEEPT_LOG_FORMAT=json
```

### CLI Flags

```bash
# Enable debug logging
meept --debug status

# Debug to specific file
meept --debug=debug.log chat "Hello"

# Debug to stderr
meept --debug=- status
```

## Structured Fields

Log entries include structured fields for better filtering and analysis:

### Common Fields

- `component` - Component name (e.g., "rpc.server", "agent.coder")
- `agent_id` - Agent identifier
- `session_id` - Session identifier
- `tool` - Tool name being executed
- `duration_ms` - Operation duration
- `error` - Error details
- `file` - Source file
- `line` - Source line number

### Component-Specific Fields

#### Agent Logging
```json
{
  "level": "INFO",
  "msg": "Agent iteration completed",
  "component": "agent.coder",
  "agent_id": "coder",
  "session_id": "session-abc123",
  "iteration": 3,
  "tool_calls": 2,
  "tokens_used": 150,
  "duration_ms": 450
}
```

#### Tool Execution
```json
{
  "level": "DEBUG",
  "msg": "Tool executed",
  "component": "tools.executor",
  "tool": "file_read",
  "agent_id": "coder",
  "path": "/path/to/file.go",
  "duration_ms": 12
}
```

#### RPC Operations
```json
{
  "level": "INFO",
  "msg": "RPC request processed",
  "component": "rpc.server",
  "method": "chat.send",
  "duration_ms": 250,
  "client_addr": "@"
}
```

#### Memory Operations
```json
{
  "level": "DEBUG",
  "msg": "Memory stored",
  "component": "memory.manager",
  "memory_id": "mem-abc123",
  "type": "episodic",
  "category": "preferences"
}
```

## Log Output Destinations

### Foreground Mode

When running in foreground (`meept-daemon -f`):
- Logs go to stdout
- Colorized output (if terminal supports it)
- Human-readable format

### Background Mode

When running as daemon (`meept-daemon -d`):
- Logs go to file (`~/.meept/meept.log`)
- JSON format for machine parsing
- Log rotation with size and age limits

### Debug Mode

When `--debug` flag is used:
- All log levels (including DEBUG) are enabled
- Additional verbose information
- Tool parameter details
- Performance metrics

## Key Log Messages

### Startup/Shutdown

```
INFO  rpc: server started socket=~/.meept/meept.sock
INFO  agent: dispatcher agent started agent_id=dispatcher
INFO  daemon: meept daemon started version=0.2.0-go
INFO  daemon: meept daemon stopped
```

### Agent Operations

```
INFO  agent: task routed agent_id=coder task_id=task-123
DEBUG agent: tool execution started tool=file_read path=/path/to/file.go
WARN  agent: tool execution slow tool=shell_execute duration_ms=2500
ERROR agent: tool execution failed tool=web_fetch error="connection timeout"
```

### Security Events

```
WARN  security: command blocked command="rm -rf /" reason="dangerous pattern"
INFO  security: permission denied path="/etc/passwd" agent_id=coder
DEBUG security: input sanitized length=150 patterns_removed=2
```

### Memory Operations

```
INFO  memory: context injected count=5 session_id=session-abc123
DEBUG memory: search performed query="authentication" results=3 duration_ms=45
INFO  memory: consolidation completed memories_processed=150
```

## Debugging Techniques

### Enable Full Debug Logging

```bash
# Start daemon with debug logging
./bin/meept-daemon -f --debug

# Or set environment variable
MEEPT_LOG_LEVEL=DEBUG ./bin/meept-daemon -f
```

### Tail Log Files

```bash
# Tail daemon logs
tail -f ~/.meept/meept.log

# Filter for specific component
tail -f ~/.meept/meept.log | grep "agent.coder"

# JSON log parsing
jq -c '. | select(.component == "agent.coder")' ~/.meept/meept.log
```

### Performance Monitoring

Look for duration metrics in logs:

```bash
# Find slow operations
grep "duration_ms" ~/.meept/meept.log | jq -c 'select(.duration_ms > 1000)'

# Monitor tool execution times
grep "tool=" ~/.meept/meept.log | jq -c '{tool, duration_ms}'
```

### Error Analysis

```bash
# Find all errors
grep "\"level\": \"ERROR\"" ~/.meept/meept.log

# Error frequency by component
jq -r '.component' ~/.meept/meept.log | sort | uniq -c | sort -nr
```

## Log Rotation

Log files are automatically rotated:

- **Max size**: 100 MB per file
- **Max backups**: 5 files
- **Max age**: 30 days

Rotation creates files like:
- `meept.log` (current)
- `meept.log.1` (previous)
- `meept.log.2` (older)
- etc.

## Custom Log Handlers

For advanced use cases, you can implement custom log handlers:

```go
import "log/slog"

// Custom JSON handler with additional fields
handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
    Level: slog.LevelDebug,
    AddSource: true,
})

// Add global attributes
handler = handler.WithAttrs([]slog.Attr{
    slog.String("version", version.String()),
    slog.String("deployment", "production"),
})

slog.SetDefault(slog.New(handler))
```

## Integration with Monitoring

Logs can be integrated with monitoring systems:

### Prometheus/Grafana

```yaml
# Log scraping configuration
scrape_configs:
  - job_name: 'meept'
    static_configs:
      - targets: ['localhost:9090']
    file_sd_configs:
      - files:
          - '/var/log/meept/*.log'
```

### ELK Stack

```yaml
# Filebeat configuration
filebeat.inputs:
- type: log
  paths:
    - /home/user/.meept/meept.log
  json.keys_under_root: true
  json.add_error_key: true
```

## Best Practices

1. **Use appropriate log levels**: DEBUG for development, INFO for production
2. **Include structured fields**: Always add relevant context
3. **Avoid sensitive data**: Never log passwords, tokens, or personal information
4. **Monitor log volume**: Large applications should use log sampling
5. **Set up alerts**: Monitor for ERROR level logs and unusual patterns

## Troubleshooting

### Common Issues

**No logs appearing:**
- Check log level configuration
- Verify file permissions
- Ensure daemon is running

**Log file too large:**
- Adjust rotation settings
- Increase log level to reduce verbosity
- Implement log sampling

**Performance issues:**
- Avoid excessive DEBUG logging in production
- Use async logging if available
- Consider structured logging over string concatenation