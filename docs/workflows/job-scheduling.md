# Job Scheduling

## Overview
Meept provides cron-based job scheduling for automated task execution, including agent tasks, shell commands, and reminders. The scheduler supports pause/resume operations and human-friendly cron expressions.

## Problem
Manual task execution lacks automation and reliability. Job scheduling enables:
- Automated task execution at specified intervals
- Reliable job management with failure handling
- Flexible scheduling with cron expressions
- Centralized job monitoring and control

## Behavior

### Job Types
- **Agent Jobs**: Execute agent prompts automatically
- **Shell Jobs**: Run shell commands on schedule
- **Reminder Jobs**: Send notification messages

### Scheduling Tools
| Tool | Description |
|------|-------------|
| `schedule_create` | Create scheduled jobs |
| `schedule_list` | List existing jobs |
| `schedule_get` | Get job details |
| `schedule_pause` / `schedule_resume` | Control job execution |
| `schedule_run_now` | Trigger immediate execution |
| `schedule_delete` | Remove jobs |
| `cron_create` | Human-friendly cron expressions |

### Cron Expression Support
- **Standard Cron**: `0 2 * * *` (2 AM daily)
- **Human-Friendly**: `every day at 2am`, `every monday at 9am`
- **Validation**: Syntax and semantics checked
- **Time Zone Support**: Configurable timezone handling

### Job Management
- **Status Tracking**: Running, paused, completed, failed, dead-letter
- **Execution History**: Track job runs and outcomes
- **Error Handling**: Multi-layer retry logic (per-tool, job-level, agent-loop)
- **Resource Limits**: Global and per-agent concurrency semaphores
- **Dead-Letter Recovery**: Recover unrecoverable jobs via `RecoverFromDeadLetter(jobID)` API

### Retry Logic Integration

Jobs benefit from the multi-layer retry system:

| Layer | Location | Retry Policy |
|-------|----------|--------------|
| Per-Tool | `tools/registry.go` | Tool-specific (0-2 retries), exponential backoff for network |
| Job-Level | `queue/store.go` | Rate limit + transient errors, 2s/4s/8s backoff |
| Agent-Loop | `agent/loop.go` | Model failover, 5 attempts max, 2s-32s backoff |

### Dead-Letter Queue

Jobs that exhaust all retries are moved to the dead-letter queue:

- **Automatic**: Moved after `max_retries` exhausted
- **Recoverable**: Use `RecoverFromDeadLetter(jobID)` API
- **Queryable**: `ListDeadLetter(limit)` to inspect dead jobs
- **Statistics**: `DeadLetterStats()` for monitoring

## Configuration

```toml
[scheduler]
enabled = true
timezone = "UTC"
max_concurrent_jobs = 5
retry_attempts = 3
retry_delay_seconds = 60

[scheduler.jobs]
history_retention_days = 30
max_job_duration_minutes = 60

[scheduler.cron]
allow_human_expressions = true
validation_strictness = "standard"

# Retry configuration
[retry]
max_retries = 3
retry_delay_base = "2s"
transient_error_patterns = ["timeout", "connection refused", "network"]

# Dead-letter configuration
[dead_letter]
enable_recovery_api = true
max_dead_letter_retention_days = 7
```

## Observability

### Logging
- Job creation and modification
- Execution start/complete events
- Failure and retry events (with retry_count, backoff)
- Dead-letter events (job moved, recovered)
- Schedule changes

### Metrics
- Job execution success rate
- Average job duration
- Concurrent job count
- Schedule accuracy
- Retry success rate
- Dead-letter count

### Debug Info
- Current job queue status
- Active schedules
- Execution history
- Resource utilization
- Dead-letter queue contents

## Edge Cases

### Job Execution Failure
- Automatic retry with exponential backoff
- Failure notification to user
- Root cause analysis logging
- Dead-letter after max retries exhausted

### Schedule Conflict
- Concurrent job limits enforced via semaphores
- Queue management for overload
- User notification of delays

### Cron Expression Error
- Validation prevents invalid schedules
- Clear error messages for correction
- Human-friendly alternatives suggested

### System Resource Limits
- Job throttling under high load
- Priority-based execution
- Resource monitoring and alerts

### Dead-Letter Recovery
- Use `RecoverFromDeadLetter(jobID)` API
- Job reset to pending state with retry_count = 0
- Audit log records recovery event
