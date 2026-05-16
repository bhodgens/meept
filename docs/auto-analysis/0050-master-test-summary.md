# Meept Platform — Master Test Summary

**Date**: 2026-05-16
**Testing Initiative**: Systematic platform QA across all 15 phases
**Status**: Partially complete — blocked by API rate limit

## Executive Summary

Comprehensive testing of the Meept platform identified **37+ unique bugs** across agent routing, LLM orchestration, token budgeting, RPC communication, and security engines. Testing was conducted via parallel subagent waves, each targeting specific capability areas.

**Critical finding**: The platform has **cascading failure modes** where a single point of failure (local LLM classifier unavailable) triggers system-wide collapse through agent misrouting, empty responses, and budget exhaustion.

## Test Waves Executed

| Wave | Phases | Agents | Tests Planned | Issues Found |
|------|--------|--------|---------------|--------------|
| Wave 1 Phase 0+1 | Prereqs, Daemon Startup | 1 | environment checks | 9 issues (0041-0049) |
| Wave 1 Phase 2 | Chat Intent | 1 | 19 tests | 6 issues (0034-0040 Phase 2) |
| Wave 1 Phase 3 | Code Intent | 1 | 24 tests | 10 issues (0034-0043 Phase 3) |
| Wave 1 Phase 4+5 | Debug + Plan | 1 | 18 tests | *blocked by rate limit* |
| Wave 1 Phase 7+8+12 | Git + Schedule + Security | 1 | 20 tests | *blocked by rate limit* |
| Wave 2 (sequential) | Phases 2-13 | 1 | 50+ tests | *blocked by rate limit after ~5 tests* |

##所有 Issues by Severity

### Critical (5 issues)
| # | Component | Title |
|---|-----------|-------|
| 0021 | agent/orchestrator | Token budget exhaustion lockout |
| 0034 (Phase 3) | dispatcher/loop | Chat RPC returns agent manifest JSON instead of LLM response |
| 0034 (Phase 2) | llm/budget | Token budget blocks ALL chat at 0% utilization |
| 0036 (Phase 3) | dispatcher | Classifier fallback causes severe agent misrouting |
| 0037 (Phase 3) | agent/loop | Chat agent produces empty reply (has_report=false) |

### High (8 issues)
| # | Component | Title |
|---|-----------|-------|
| 0022 | agent/dispatcher | Async dispatch drops final response |
| 0025 | llm/context_firewall | Context firewall validates before reduce |
| 0012 | memory/episodic | Episodic schema migration missing |
| 0012s | security/engine | Security hooks only cover shell tool (not file ops) |
| 0012s | security/tirith | No Tirith scan logging |
| 0035 (Phase 2) | cli | CLI chat command silently swallows error responses |
| 0036 (Phase 2) | agent/dispatcher | Over-classification of simple chat as compound intent |
| 0037 (Phase 2) | daemon | Daemon binary mismatch: running version differs from development |
| 0038 (Phase 2) | rpc | Chat response returns status JSON instead of chat response |
| 0038 (Phase 3) | daemon/queue | Stale queue jobs consume budget on daemon restart |

### Medium (15 issues)
| # | Component | Title |
|---|-----------|-------|
| 0001 | config | Config loading priority (CWD shadows home) |
| 0002 | memory/ftstore | SQLite FTS5 missing |
| 0005 | agent/loop | Tool termination skips LLM follow-up |
| 0006 | agent/dispatcher | Tiny model over-classifies intents |
| 0015 | rpc/skills | Skills RPC dead-letter when disabled |
| 0020 | memory/episodic | Episodic schema migration missing |
| 0023 | agent/reviewer | Review agent burns tokens unconditionally |
| 0024 | agent/orchestrator | Step semaphore blocks without feedback |
| 0026 | rpc, services | FirewallStats not exposed via RPC/HTTP |
| 0027 | context_firewall | Token count ignores tool calls |
| 0030 | context_firewall | Summarization silent failure |
| 0031 | rpc/server | RPC status hardcoded budget values |
| 0039 (Phase 2) | agent/dispatcher | Async dispatch bypasses budget gate, creates zombie tasks |
| 0039 (Phase 3) | agent/loop | Planner returns empty content, hits convergence detection |
| 0040 (Phase 3) | agent/loop | Tool termination signals skip LLM follow-up (#0005 recurrence) |
| 0041 (Phase 3) | daemon/components | Classifier uses unreachable local LLM, no fallback |
| 0042 (Phase 3) | orchestrator | BudgetExceededError classified non-retryable but still retried |

### Low (5 issues)
| # | Component | Title |
|---|-----------|-------|
| 0004 | daemon/components | API key warning hardcodes provider name |
| 0028 | context_compressor | Aggressive compress skips compactor |
| 0029 | context_firewall | Inconsistent firewall log levels |
| 0043 (Phase 3) | memory/ftstore | FTS5 unavailable, using LIKE fallback |
| 0044-0049 | various | Daemon instability, worker state issues, RPC timeout |

## Key Patterns Identified

### 1. Classifier Single Point of Failure
When local LLM classifier (127.0.0.1:8080) is unavailable:
- Keyword fallback routes code tasks to `chat`/`scheduler`/`committer` instead of `coder`
- Confidence scores as low as 0.02 are accepted without warning
- The `coder` agent is effectively unreachable

### 2. Token Budget Death Spiral
```
Classifier fails → Wrong agent → Empty response → Retry → Budget exhausted → All LLM calls blocked
```
- Stale queue jobs on daemon restart consume budget before new requests
- Async dispatch bypasses budget check entirely (bug 0039)
- Review cycles burn tokens unconditionally (bug 0023)
- Status endpoint hardcodes values, hiding real state (bug 0031)

### 3. Silent Failure Cascade
Errors are swallowed at every layer:
- RPC `Chat()` ignores error field
- CLI prints empty reply with exit 0
- Agent loops return `has_report=false` with no explanation
- No logs, no warnings, no user-visible errors

### 4. Over-Classification
Simple queries dispatched as compound multi-agent tasks:
- "hello" → chat + scheduler (2 subtasks, 8-13 min estimate)
- "2+2" → analyst + scheduler (8-13 min estimate)
- "what can you do?" → chat + scheduler

### 5. Serial Bottleneck
Daemon handles one RPC request at a time:
- Concurrent `meept chat` commands get empty responses
- 6 parallel requests = 5 empty, 1 works
- Watchdog/timeout insufficient for long LLM calls

## Test Coverage by Phase

| Phase | Topic | Tests Run | Pass | Partial | Fail | Blocked |
|-------|-------|-----------|------|---------|------|---------|
| 0 | Prerequisites | 6 env checks | 3 | 0 | 3 | 0 |
| 1 | Daemon/Transport | 12 | 4 | 1 | 7 | 0 |
| 2 | Chat Intent | 19 | 0 | 4 | 15 | 0 |
| 3 | Code Intent | 24 | 0 | 4 | 20* | *infra blocked |
| 4 | Debug Intent | 0 | 0 | 0 | 0 | rate limit |
| 5 | Plan Intent | 0 | 0 | 0 | 0 | rate limit |
| 6 | Analyze Intent | 0 | 0 | 0 | 0 | rate limit |
| 7 | Git Intent | 0 | 0 | 0 | 0 | rate limit |
| 8 | Schedule Intent | 0 | 0 | 0 | 0 | rate limit |
| 10 | Memory | 0 | 0 | 0 | 0 | rate limit |
| 12 | Security | 0 | 0 | 0 | 0 | rate limit |
| 13 | Communication Quality | 0 | 0 | 0 | 0 | rate limit |

## Environment Factors

| Factor | Status | Impact |
|--------|--------|--------|
| z.ai API | ✅ Working | 12s response time, good quality |
| Local classifier | ❌ Unavailable | 127.0.0.1:8080 unreachable |
| FTS5 | ❌ Not available | Memory uses slow LIKE fallback |
| Hourly budget | ⚠️ 100K tokens | Exhausts in ~90s of compound tasks |
| Daily budget | ⚠️ 1M tokens | Not yet tested |
| Rate limit | ⚠️ Active | 1308: "Usage limit reached for 5 hour" |
| Daemon serial | ⚠️ Single-threaded | Concurrent requests fail |

## Recommendations — Fix Priority

### Sprint 1 — Unblock Core Functionality (P0)
1. **#0031** — Fix RPC status to read actual budget state (5 line fix)
2. **#0038** — Clear stale queue jobs on daemon restart
3. **#0037** — Fix chat agent empty response (has_report=false path)
4. **#0036** — Improve classifier fallback routing for code tasks

### Sprint 2 — Core Reliability (P1)
5. **#0021** — Add budget safeguards (reserve for new requests, clear stale)
6. **#0022** — Fix async dispatch to return final response
7. **#0020/0012** — Episodic schema migration
8. **#0025** — Context firewall: reduce before validate
9. **#0035** — CLI error handling (print errors, non-zero exit)

### Sprint 3 — Visibility and Polish (P2)
10. **#0026** — Expose FirewallStats via RPC/HTTP
11. **#0015** — Skills RPC dead-letter pattern
12. **#0023** — Conditional review cycles
13. **#0027** — Token count includes tool calls
14. **#0014** — Self-improve stub RPC handlers
15. **#0013** — MCP subscription_id access
16. Remaining low-severity items

## Files Produced

| Range | Count | Topics |
|-------|-------|--------|
| 0030-0033 | 4 | Context firewall findings + full summary |
| 0034-0043 (Phase 2) | 6 | Chat intent testing |
| 0034-0043 (Phase 3) | 10 | Code intent testing |
| 0041-0049 | 9 | Daemon/transport/RPC issues |
| 0050 | 1 | This master summary |

## Unblocked Next Steps

1. **Wait for rate limit reset** (~3 hours from 2026-05-16 15:52:51)
2. **Start local llama.cpp** at 127.0.0.1:8080 for classifier
3. **Run serialized tests** — one subagent, sequential commands
4. **Complete Phases 4-13** — debug, plan, analyze, git, schedule, memory, security, communication
5. **End-to-end regression** — full user workflow tests

## Lessons Learned

1. **Parallel subagents fighting over daemon** — Don't run multiple test agents concurrently; they restart the daemon and pollute each other's test runs.

2. **Serial daemon is a bottleneck** — Future testing should use a queue: one command at a time, 60-90s timeout per test.

3. ** Budget visibility is critical** — The hardcoded status values (bug 0031) made debugging impossible. Always instrument budget/token counters.

4. **Classifier dependency is a fragility** — The entire platform collapses when the classifier is down. Either make it redundant or use the main LLM for classification.

5. **Error propagation matters** — Silent failures at every layer made debugging exponentially harder. Always propagate errors to the user.
