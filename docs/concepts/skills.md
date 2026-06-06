# Skills

Skills are reusable instruction sets that extend agent capabilities. They follow the same markdown-with-frontmatter pattern as agent definitions.

## Skill Discovery

Skills are discovered from a three-tier hierarchy with priority shadowing:

| Priority | Location | Description |
|----------|----------|-------------|
| 1 (highest) | `.meept/skills/` | Project-local skills |
| 2 | `~/.meept/skills/` | User-global skills |
| 3 | `~/.config/meept/skills/` | System-wide skills |
When multiple skills have the same name, the highest-priority version wins.

## SKILL.md Format

```markdown
---
name: deploy-checklist
description: Run deployment verification checklist
requires:
  - code
  - reasoning
triggers:
  - deploy
  - release
  - ship
---

# Deployment Checklist

Before deploying, verify:
1. All tests pass
2. No TODO comments in changed files
3. Version number updated
4. Changelog updated
```

### Frontmatter Fields

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Skill identifier |
| `description` | string | What the skill does |
| `requires` | string[] | Required model capabilities |
| `triggers` | string[] | Keywords for auto-invocation |

## Model Resolution

Skills declare `requires` capabilities. The model resolver finds the cheapest model satisfying those requirements:

```json5
{
  "providers": {
    "ollama": {
      "models": {
        "llama3.2": {
          "capabilities": ["code", "tool_use", "reasoning"],
          "input_cost": 0.0,
          "output_cost": 0.0
        }
      }
    }
  }
}
```

A skill requiring `["code", "reasoning"]` matches `llama3.2` but not a model with only `["completion"]`.

## Skill Invocation

### From Agent Conversation

```
You: "/deploy-checklist production"
```

The agent detects the trigger keyword or explicit `/skill-name` invocation and loads the skill instructions.

### From CLI

```bash
./bin/meept skills list
./bin/meept skills show deploy-checklist
./bin/meept skills run deploy-checklist "production"
```

See [Skill System](../workflows/skills.md) for the full feature specification.
