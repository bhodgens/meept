# Plan: Skill evolution: improve parallel-subagent-code-review

## Meta

- plan_id: plan-20260831214823-0032
- created: 2026-08-31
- status: planning

## Summary

Effectiveness is critically low at 0.16 with 35 negatives vs 10 positives out of 63 uses, indicating the skill's instructions likely produce incorrect, unreliable, or misaligned outputs. The concept needs restructuring for clearer scope, error handling, and output quality.

Candidate content:
---
name: parallel-subagent-code-review
description: |
  Perform parallel code review by spawning multiple specialized sub-agents,
  each focused on a different review dimension. Synthesizes results into
  a single structured report.
metadata:
  author: Sapiens AI
  version: 2.0
  category: code-review
  tags:
    - code-review
    - parallel-agents
    - multi-agent
---

## Overview

Spawns up to 3 focused sub-agents in parallel, each reviewing a specific
dimension of the target code, then synthesizes their findings into one
actionable report.

## When to Use

- Large PRs or complex files where a single pass review is insufficient
- Team review processes needing structured, categorized feedback
- Security, performance, and correctness reviews performed concurrently

**Do not use** for:
- Small files or trivial changes (use `standard-code-review`)
- One-off quick checks
- Situations where sub-agent parallelism isn't supported

## Sub-Agent Dimensions

Each sub-agent reviews one dimension independently:

| Agent | Focus |
|-------|-------|
| `security-reviewer` | Vulnerabilities, injection risks, secret exposure, unsafe patterns |
| `performance-reviewer` | Algorithmic complexity, resource leaks, inefficient loops, I/O bottlenecks |
| `correctness-reviewer` | Logic errors, edge cases, type safety, error handling gaps |

## Steps

1. **Parse the target**: Identify file(s), language, and scope of changes.
2. **Select dimensions**: Choose up to 3 relevant reviewers based on file type and change scope.
3. **Spawn parallel sub-agents**: Run each selected reviewer against the same target simultaneously.
4. **Aggregate results**: Collect all sub-agent outputs.
5. **Deduplicate & merge**: Remove overlapping comments, resolve contradictions by weighting (security > correctness > performance).
6. **Generate final report** using the template below.

## Output Template

```markdown
## Code Review Summary

**Files reviewed:** <list>
**Reviewers active:** <list>
**Overall severity:** <critical | high | medium | low>

### Critical / Must-Fix
<bulleted list, only items flagged by ≥1 reviewer as critical>

### Warnings
<bulleted list, moderate severity findings>

### Suggestions
<bulleted list, optional improvements>

### Cross-Dimensional Conflicts
<integer> conflicts detected between reviewers (resolved per priority).
```

## Constraints

- Max 3 sub-agents per invocation.
- Each sub-agent must receive: file content, language, and its specific focus directive.
- If any sub-agent fails, proceed with remaining results and note the failure.
- Never return raw sub-agent output verbatim — always synthesize into the template.


## Notes

