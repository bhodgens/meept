# Skill System

## Overview
Meept's skill system enables extensibility through modular, discoverable skills. Skills are defined using SKILL.md files with YAML frontmatter and loaded dynamically from multiple discovery tiers.

## Problem
Hardcoded functionality limits adaptability and requires code changes for new capabilities. The skill system provides:
- Modular, reusable functionality
- Multi-tier discovery with priority shadowing
- Model resolution based on capability requirements
- User customization without code changes

## Behavior

### Skill Discovery Hierarchy (Priority Order)
1. **Project-local**: `.meept/skills/` (highest priority)
2. **User-global**: `~/.meept/skills/`
3. **System-wide**: `~/.config/meept/skills/`
When multiple skills have the same name, the highest-priority version wins.

### SKILL.md Format
```markdown
---
name: Code Reviewer
requires: [code, reasoning]
tools: [file_read, memory_search]
triggers: [review, code, check]
---

# Code Reviewer Skill

Review code changes for correctness, style, security, and completeness.

## Usage
When reviewing code, check for:
- Correctness: Does it accomplish the intended goal?
- Style: Follows best practices and conventions
- Security: No vulnerabilities or issues
- Completeness: Error cases handled appropriately
```

### Model Resolution
- Skills declare `requires: [code, reasoning]` in YAML
- Models declare `capabilities: [code, tool_use]` in config
- Resolver finds cheapest model satisfying requirements

### Skill Invocation
1. **Trigger Matching**: Keywords matched against skill triggers
2. **Capability Check**: Agent capabilities verified
3. **Tool Availability**: Required tools checked
4. **Execution**: Skill logic executed with context

## Configuration

```toml
[skills]
enabled = true
search_paths = []
auto_reload = false

```

## Observability

### Logging
- Skill discovery and loading
- Model resolution decisions
- Skill invocation events
- Capability mismatch warnings

### Metrics
- Skill discovery time
- Model resolution latency
- Skill execution success rate
- Capability matching accuracy

### Debug Info
- Available skills per agent
- Model capability mappings
- Skill trigger patterns
- Discovery path resolution

## Edge Cases

### Skill Not Found
- Clear error message indicating missing skill
- Suggests similar available skills
- Logs discovery failure for monitoring

### Capability Mismatch
- Agent lacks required capabilities
- Alternative skills suggested
- Model upgrade recommended

### Tool Unavailable
- Required tools not accessible to agent
- Permission or capability issue
- Alternative approaches suggested

### Discovery Path Conflict
- Multiple versions of same skill
- Highest priority path wins
- Shadowing logged for transparency