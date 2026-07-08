# Self-Improvement

## Overview
Meept can automatically detect and fix issues in its own code through a comprehensive self-improvement system. This includes issue detection, sandboxed fixing, safety constraints, and human approval workflows.

## Problem
Manual code maintenance is time-consuming and error-prone. The self-improvement system enables:
- Automated issue detection from multiple sources
- Safe, sandboxed code fixing
- Validation of changes before application
- Human oversight for critical changes

## Behavior

### Issue Detection
- **Pytest Integration**: Test failures trigger detection
- **Runtime Logs**: Error patterns identified from logs
- **Type Checking**: Static analysis issues detected
- **Linting**: Code style and quality issues flagged

### Automated Fixing
- **AI-Powered Fix Generation**: Uses Meept's own infrastructure
- **Sandboxed Validation**: Changes tested in isolated worktrees
- **Safety Constraints**: Prevents dangerous or breaking changes
- **Incremental Application**: Changes applied gradually

### Human Approval Workflow
- **Critical Changes**: Require explicit human approval
- **Review Interface**: Changes presented for review
- **Rollback Capability**: Failed changes can be reverted
- **Approval Tracking**: Audit trail of all changes

### Detection Sources
1. **Pytest Results**: Test failures and errors
2. **Runtime Exceptions**: Application crashes and warnings
3. **Type Check Errors**: Static analysis issues
4. **Lint Violations**: Code quality problems

## Configuration

```toml
[selfimprove]
enabled = false
data_dir = "~/.meept/selfimprove"

[selfimprove.detection]
scan_pytest = true
scan_runtime_logs = true
scan_type_check = true
scan_lint = true

[selfimprove.fixing]
max_fix_attempts = 3
sandbox_timeout_minutes = 10
require_human_approval = true
approval_timeout_hours = 24

[selfimprove.safety]
block_dangerous_changes = true
max_file_changes_per_run = 5
backup_before_changes = true
```

## Observability

### Logging
- Issue detection events
- Fix generation attempts
- Sandbox validation results
- Human approval decisions

### Metrics
- Detection accuracy rate
- Fix success rate
- Sandbox performance
- Approval workflow timing

### Debug Info
- Current detection rules
- Active safety constraints
- Approval queue status
- Change history

## Edge Cases

### Detection False Positive
- Legitimate code flagged as issue
- Manual override available
- Detection rules refined over time

### Fix Generation Failure
- Multiple attempts with different approaches
- Fallback to simpler fixes
- Human intervention requested if stuck

### Sandbox Validation Crash
- Changes reverted automatically
- Root cause analysis performed
- Safer alternative approaches attempted

### Human Approval Timeout
- Changes expire if not approved
- Notification sent to user
- Option to extend approval window

## Closed-Loop Skill and Model Improvement

In addition to the issue-detection flow above, Meept runs two continuous improvement loops:

### Skill Evolution Loop

Scheduled every 6 hours (configurable). Four passes:

1. **Pass A (Refine)** — Improves existing skills based on usage evidence and reflection proposals.
2. **Pass B (Promote)** — Promotes learned patterns meeting thresholds to new skills.
3. **Pass C (Prune)** — Archives low-performing skills.
4. **Pass D (Fill Gap)** — Mines low-match queries from the capability index; proposes new skills for unmet recurring needs.

Every proposal passes the four-dimension verifier (`grounded_in_evidence`, `preserves_existing_value`, `specificity_and_reusability`, `safe_to_publish`) before being applied or planned.

### Shadow Training Loop

Continuous. Captures production LLM traffic, scores against a teacher model, and produces LoRA training pairs. Trained adapters pass an eval gate before being hot-swapped into the serving alias. See [Shadow Training](./shadow-training.md).

### Reflection Queue

Per-turn reflection (`internal/agent/reflection_collector.go`) writes proposals to `.meept/improvements.md`. The skill evolver drains this queue at the start of every Pass A cycle, so reflection proposals feed directly into skill refinement without manual review.

### Routing Decision Log

Every model-resolution decision is persisted to `<data_dir>/routing.db`. See [Routing Decisions](./routing-decisions.md).