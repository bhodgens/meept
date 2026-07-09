# Routing CLI

The routing CLI provides visibility into LLM routing decisions and enables manual control over model selection for skills and sessions.

## Overview

Meept's LLM resolver makes routing decisions based on:
- Cost optimization
- Model capabilities
- Skill requirements
- User overrides

These decisions are persisted to SQLite for audit trails and training data mining.

## Commands

### `meept routing decisions`

List recent routing decisions with filtering options.

```bash
# Show last 20 decisions
meept routing decisions

# Filter by session
meept routing decisions --session=abc123

# Filter by date range
meept routing decisions --since=2h --until=now

# Show full details including cost estimates
meept routing decisions --verbose
```

**Output format:**
```
TIMESTAMP            SESSION    SKILL              MODEL              COST    CHOICE
2026-07-08 14:32:01  abc123     code.review        claude-sonnet    $0.002  cost-optimized
2026-07-08 14:33:15  abc123     creative.writing   claude-opus      $0.015  capability-match
```

### `meept routing stats`

Show routing statistics and cost summaries.

```bash
# Overall stats
meept routing stats

# Stats for specific session
meept routing stats --session=abc123

# Stats by model
meept routing stats --group-by=model
```

**Output format:**
```
Routing Statistics (last 24h)

Total decisions: 156
Total cost: $2.34

By model:
  claude-sonnet:  89 (57%)  $0.89
  claude-opus:    45 (29%)  $1.23
  claude-haiku:   22 (14%)  $0.22

By skill:
  code.review:      67  $1.02
  creative.writing: 34  $0.89
  chat:             55  $0.43
```

### `meept routing override`

Set a manual model override for a session or skill.

```bash
# Override model for current session
meept routing override --session=abc123 --model=claude-opus

# Override model for specific skill
meept routing override --skill=code.review --model=claude-sonnet

# Clear override
meept routing override --session=abc123 --clear
```

**Notes:**
- Overrides persist until cleared
- Overrides are logged in routing decisions
- Agent loop respects overrides on next turn

### `meept routing explain`

Explain why a particular routing decision was made.

```bash
meept routing explain --decision-id=12345
```

**Output format:**
```
Decision #12345 at 2026-07-08 14:32:01

Request:
  Session: abc123
  Skill: code.review
  Input tokens: 4,521
  Expected output: 500 tokens

Candidate models:
  1. claude-sonnet-4-6:  $0.0018 (selected)
     - Cost score: 0.95
     - Capability score: 0.88
     - Combined: 0.91

  2. claude-opus-4-6:    $0.0150
     - Cost score: 0.42
     - Capability score: 0.98
     - Combined: 0.70

  3. claude-haiku-4-5:   $0.0003
     - Cost score: 1.00
     - Capability score: 0.65
     - Combined: 0.82

Decision factors:
  - Cost optimization: 60% weight
  - Capability match: 40% weight
  - Selected: claude-sonnet-4-6 (best combined score)
```

## Implementation Details

### Storage

Routing decisions are stored in `~/.meept/routing_log.sqlite3`:

```sql
CREATE TABLE routing_decisions (
    id INTEGER PRIMARY KEY,
    timestamp DATETIME,
    session_id TEXT,
    skill_id TEXT,
    selected_model TEXT,
    candidate_models TEXT,  -- JSON array
    cost_estimate REAL,
    decision_reason TEXT,
    override_active BOOLEAN
);
```

### Integration Points

1. **LLM Resolver** (`internal/llm/resolver.go`):
   - Emits `RoutingDecision` on every `ResolveForSkill` call
   - Logs to `RoutingLogger` before returning model selection

2. **Agent Loop** (`internal/agent/loop.go`):
   - Checks for model overrides before resolution
   - Applies override or uses resolver's decision

3. **Shadow Training** (`internal/shadow/`):
   - Mining routing decisions for training pairs
   - Comparing selected model vs. teacher model performance

## Troubleshooting

### "No routing decisions found"

- Check if routing logger is enabled in config
- Verify SQLite database exists at `~/.meept/routing_log.sqlite3`
- Ensure you're querying the correct session ID

### "Model override not taking effect"

- Overrides apply on next turn, not mid-turn
- Check if override was cleared by session reset
- Verify model name matches available models in config

### High cost warnings

Use `meept routing stats --group-by=model` to identify expensive models. Consider:
- Setting capability-appropriate models per skill
- Using `meept routing override` for cost control
- Reviewing skill definitions for appropriate model hints

## See Also

- `docs/workflows/skills.md` - Skill system and model hints
- `docs/workflows/self-improvement.md` - Self-improvement loop using routing data
- `docs/features.md` - Feature overview including routing
