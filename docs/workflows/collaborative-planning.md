# Collaborative Planning

## Overview

Collaborative planning enables review and approval workflows for agent work, with specialized reviewer agents providing validation and feedback. This ensures quality control and enables iterative improvement of agent outputs.

The system integrates with the **Deterministic Execution Framework** for evidence-based validation:
- Steps are validated for evidence before review begins
- Validation failures trigger `needs_info` status for human intervention
- Review focuses on quality; validation focuses on correctness

## Problem
Single-agent execution lacks validation mechanisms. Collaborative planning provides:
- Quality assurance through peer review
- Iterative improvement based on feedback
- Specialized validation for different task types
- Automated approval for simple tasks

## Behavior

### Review/Approval Workflow
1. **Task Submission**: Agent completes initial work
2. **Reviewer Assignment**: Appropriate reviewer selected based on task type
3. **Validation**: Reviewer assesses work quality
4. **Feedback**: Specific issues identified
5. **Revision**: Original agent addresses feedback
6. **Final Approval**: Work accepted or rejected

### Reviewer Mapping
| Task Type | Reviewer Agent | Focus Areas |
|-----------|----------------|-------------|
| Code Changes | `code-reviewer` | Correctness, style, security, completeness |
| Debugging | `debug-reviewer` | Root cause, solution effectiveness, testing |
| Analysis | `analyst-reviewer` | Accuracy, completeness, clarity, actionability |
| Planning | `planner-reviewer` | Feasibility, completeness, ordering, risk |
| Testing | `test-reviewer` | Verification, expectation matching, validation |

### Revision Cycles
- **Maximum Cycles**: Configurable limit (default: 3)
- **Progressive Improvement**: Each cycle addresses feedback
- **Auto-Approval Patterns**: Simple tasks approved automatically
- **Final Decision**: Forced resolution after cycle limit

### Reviewer Agent Behavior
- **Structured Feedback**: JSON responses with status, feedback, issues, confidence
- **Pragmatic Evaluation**: Focus on practical usefulness over perfection
- **Actionable Suggestions**: Clear guidance for improvements
- **Confidence Scoring**: 0.0-1.0 confidence in evaluation

## Configuration

```toml
[collaborative]
enabled = true
reviewer_mapping = {}
auto_approve_simple = true
max_revision_cycles = 3

[collaborative.reviewers]
code_timeout_minutes = 5
debug_timeout_minutes = 5
analyst_timeout_minutes = 5
planner_timeout_minutes = 5
test_timeout_minutes = 5

[collaborative.auto_approve]
max_complexity = 3
max_duration_minutes = 10
required_confidence = 0.8
```

## Observability

### Logging
- Review workflow initiation
- Reviewer assignment decisions
- Feedback generation
- Approval/rejection events

### Metrics
- Review cycle count distribution
- Approval/rejection rates
- Reviewer response times
- Revision effectiveness

### Debug Info
- Active review workflows
- Reviewer availability
- Auto-approval patterns
- Cycle progress tracking

## Edge Cases

### Reviewer Unavailable
- Alternative reviewers considered
- Task queued for retry
- User notified of delay

### Conflicting Feedback
- Consensus mechanism applied
- Most critical issues prioritized
- User arbitration option

### Revision Cycle Limit
- Final decision forced
- User notified of resolution
- Lessons learned captured

### Low Confidence Review
- Additional reviewers consulted
- User input requested
- Conservative decision making