# Routing Decision Logging

## Overview
Every model-resolution decision the LLM resolver makes is persisted to a SQLite store. The routing log is the training-set foundation for future routing-classifier work and provides observability into why each request went where.

## Schema

```sql
CREATE TABLE routing_decisions (
    id TEXT PRIMARY KEY,
    request_id TEXT NOT NULL,
    timestamp DATETIME NOT NULL,
    chosen_model_id TEXT NOT NULL,
    chosen_provider_id TEXT NOT NULL,
    alias TEXT,
    reason TEXT,           -- "round_robin" | "capability_escalation" | ...
    skill TEXT,            -- populated when escalation was for a skill
    employee_id TEXT,
    candidates_json TEXT   -- JSON array of all candidates considered
);
```

Location: `<data_dir>/routing.db`

## What's logged

| Method | Reason | When |
|---|---|---|
| `ResolveForAlias` | `round_robin` | every alias resolution (the production hot path) |
| `ResolveForSkill` | `capability_escalation` | when skill requires capabilities the current model lacks |
| `ResolveRef` | `explicit` | when a specific model ref is requested |

## CLI

```bash
meept routing recent [N]            # last N decisions (default 20)
meept routing by-model <model-id>   # decisions that chose a specific model
```

## Future: routing classifier

The routing log plus enriched PreferencePairs together enable a future enhancement where the resolver itself learns from past outcomes. See plan `2026-07-07-self-improvement-loop-closure.md` Gap A4 stage 2 (out of scope for first ship).
