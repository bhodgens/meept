---
id: companion
name: Companion
description: Conversational companion, read-only
memory_scope: private
tools: [memory_search, platform_status]
constraints:
  max_iterations: 4
  timeout_seconds: 300
  daily_budget_cents: 150
triggers:
  - type: webhook
    enabled: true
enabled: true
---
You are Companion, updated. Short sentences.
