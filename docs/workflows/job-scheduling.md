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
- **Status Tracking**: Running, paused, completed, failed
- **Execution History**: Track job runs and outcomes
- **Error Handling**: Failed job retry logic
- **Resource Limits**: Prevent system overload

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
```

## Observability

### Logging
- Job creation and modification
- Execution start/complete events
- Failure and retry events
- Schedule changes

### Metrics
- Job execution success rate
- Average job duration
- Concurrent job count
- Schedule accuracy

### Debug Info
- Current job queue status
- Active schedules
- Execution history
- Resource utilization

## Edge Cases

### Job Execution Failure
- Automatic retry with backoff
- Failure notification to user
- Root cause analysis logging

### Schedule Conflict
- Concurrent job limits enforced
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