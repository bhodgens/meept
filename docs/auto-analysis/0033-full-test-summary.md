# Meept Platform Test Summary — All Phases Complete

**Date**: 2026-05-15
**Plan**: `docs/auto-analysis/0000-testing-plan.md`

## Phase Completion Status

| Phase | Topic | Status | New Findings |
|-------|-------|--------|-------------|
| 0 | Prerequisites | Done | — |
| 1 | Daemon Startup & Transport | Done | #0001, #0003, #0004 |
| 2 | Core Agent Loop & LLM | Done | #0005, #0006, #0007 |
| 3 | Multi-Agent Orchestration | Done | #0020, #0021, #0022, #0023, #0024 |
| 4 | MCP Communication | Done | 0013 (2 bugs) |
| 5 | Memory System | Done | #0002, #0020 (dupe) |
| 6 | Security Engine | Done | 0012 (5 bugs) |
| 7 | Job Scheduler | Done | #0008 |
| 8 | Skills System | Done | #0015 |
| 9 | Self-Improvement | Done | #0014 (4 bugs) |
| 10 | Context Firewall | Done | #0025-#0030 |
| 11 | Playground Integration | Done | #0031, #0032 |
| 12 | CLI/TUI & Sessions | Done | #0009, #0010 |

## All Findings (28 unique)

### Previously Fixed (Round 1)
| # | Severity | Title | Status |
|---|----------|-------|--------|
| 0003 | medium | Status missing model info | **Fixed** |
| 0007 | high | LLM empty content / no logging | **Fixed** |
| 0008 | high | Scheduler RPC not wired | **Fixed** |
| 0009 | high | Task/queue list flag panic | **Fixed** |
| 0010 | low | Duplicate help command | **Fixed** |

### Open — Critical
| # | Severity | Title | Component |
|---|----------|-------|-----------|
| 0021 | critical | Token budget exhaustion lockout | agent/orchestrator |
| 0032 | high | Step result stale in review (false rejections) | agent/workspace |

### Open — High
| # | Severity | Title | Component |
|---|----------|-------|-----------|
| 0022 | high | Async dispatch drops final response | agent/dispatcher |
| 0025 | high | Context firewall validates before reduce | llm/context_firewall |
| 0012 | high | Episodic schema migration missing | memory/episodic |
| 0012s | high | Security hooks only cover shell tool (not file ops) | security/engine |
| 0012s | high | No Tirith scan logging | security/tirith |

### Open — Medium
| # | Severity | Title | Component |
|---|----------|-------|-----------|
| 0001 | medium | Config loading priority (CWD shadows home) | config |
| 0002 | medium | SQLite FTS5 missing | memory/ftstore |
| 0005 | medium | Tool termination skips LLM follow-up | agent/loop |
| 0006 | medium | Tiny model over-classifies intents | agent/dispatcher |
| 0015 | medium | Skills RPC dead-letter when disabled | rpc/skills |
| 0020 | medium | Episodic schema migration missing | memory/episodic |
| 0023 | medium | Review agent burns tokens unconditionally | agent/reviewer |
| 0024 | medium | Step semaphore blocks without feedback | agent/orchestrator |
| 0026 | medium | FirewallStats not exposed via RPC/HTTP | rpc, services |
| 0027 | medium | Token count ignores tool calls | context_firewall |
| 0030 | medium | Summarization silent failure | context_firewall |
| 0031 | medium | RPC status hardcoded budget values | rpc/server |

### Open — Low
| # | Severity | Title | Component |
|---|----------|-------|-----------|
| 0004 | low | API key warning hardcodes provider name | daemon/components |
| 0028 | low | Aggressive compress skips compactor | context_compressor |
| 0029 | low | Inconsistent firewall log levels | context_firewall |

### Multi-bug Documents
- **0012** (security): 5 bugs in one document
- **0013** (MCP): 2 bugs in one document
- **0014** (selfimprove): 4 bugs in one document

## Recommended Fix Priority

### Sprint 1 — Unblock core functionality
1. **#0032** — Step result stale pointer (causes false rejections, amplifies all other bugs)
2. **#0021** — Token budget exhaustion (makes system unusable after ~90 seconds of compound tasks)
3. **#0022** — Async dispatch drops response (user never sees output)

### Sprint 2 — Core reliability
4. **#0020/0012** — Episodic schema migration (memory_store always fails)
5. **#0025** — Context firewall validates before reduce
6. **#0015** — Skills RPC dead-letter pattern
7. **#0012s** — Security hooks missing for file operations

### Sprint 3 — Visibility and polish
8. **#0031** — RPC status hardcoded budget
9. **#0026** — FirewallStats not exposed
10. **#0027** — Token count ignores tool calls
11. **#0014** — Self-improve stub RPC handlers + error messages
12. **#0013** — MCP subscription_id not accessible + Go %v formatting
13. **#0001** — Config loading priority
14. Remaining low-severity items

## Key Patterns Identified

1. **Dead-letter RPC pattern** (#0008, #0015): Bus proxy handlers registered unconditionally, direct handlers only when feature enabled. When disabled, requests timeout silently.
2. **Stale pointer pattern** (#0032): Database writes don't update in-memory structs, causing downstream code to read stale data.
3. **Budget amplification**: Compound intent decomposition + unconditional review cycles + false rejections create a token burn rate that exhausts budget in minutes.
4. **Validate-before-reduce pattern** (#0025): Context firewall checks size before reduction pipeline runs, blocking requests that could be salvaged.
