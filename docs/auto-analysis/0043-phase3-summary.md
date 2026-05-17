# Phase 3: Code Intent Testing Summary

| Field | Value |
|-------|-------|
| Date | 2026-05-16 |
| Phase | 3 |
| Tester | QA Automation |
| Total Tests Attempted | 24 (8 standard + 7 creative + environment issues) |
| Tests Completed with Output | 4 |
| Tests Blocked by Infrastructure | 20 |

## Infrastructure Issues (Blocking)

The following systemic issues prevented execution of most test cases:

1. **Token Budget Exhaustion**: The 100K hourly budget was consumed by task orchestration overhead (planning, review, retry loops) before user-facing work could complete. Stale queue jobs from previous sessions consumed the budget on restart.

2. **Daemon Stability**: Competing processes (other QA agents in the same environment) were simultaneously restarting the daemon, causing socket connection failures mid-test.

3. **Empty Chat Responses**: The `chat` agent consistently returned empty replies, making it impossible to evaluate the LLM's code generation quality.

## Test Results

### Standard Test Cases

| Case | Description | Agent Routed To | Expected Agent | Result | Rating |
|------|-------------|-----------------|----------------|--------|--------|
| 17 | Simple code generation | chat / scheduler / committer | coder | FAIL - wrong agent, empty response or agent manifest dump | C:fail, Co:poor, E:wasteful |
| 18 | File creation | committer / scheduler | coder | PARTIAL - routed async, created task plan but wrong agents | C:fail, Co:adequate, E:wasteful |
| 19 | File reading + modification | chat (0.02 conf) | coder | FAIL - routed to chat with near-zero confidence | C:fail, Co:poor, E:wasteful |
| 20 | Multi-file module creation | compound -> orchestrator | coder/planner | PARTIAL - detected compound intent but planner returned empty | C:partial, Co:adequate, E:excessive |
| 21 | Shell execution (tests) | scheduler (0.104 conf) | coder | FAIL - routed to scheduler | C:fail, Co:poor, E:wasteful |
| 22 | Code explanation | chat (0.3 conf) | analyst/coder | FAIL - routed to chat with low confidence | C:fail, Co:poor, E:wasteful |
| 23 | Refactoring | committer (0.945 conf) | coder | FAIL - routed to committer with high confidence | C:fail, Co:poor, E:wasteful |
| 24 | Complex implementation | compound -> orchestrator | coder/planner | PARTIAL - compound intent detected, planner failed (empty content) | C:partial, Co:adequate, E:excessive |

### Creative Test Variations

| Case | Description | Agent Routed To | Expected Agent | Result |
|------|-------------|-----------------|----------------|--------|
| V1 | Implicit intent (sort users) | chat | coder | FAIL - chat returns empty |
| V2 | Error-driven (crash fix) | compound -> chat | debugger | PARTIAL - compound detected but falls to chat |
| V3 | Partial instruction (add auth) | committer (0.945) | planner/coder | FAIL - routed to committer, creates git branch instead of planning |
| V4 | Bad path test | N/A | any | Blocked - couldn't test |
| V5 | Language switch (Python) | N/A | coder | Blocked - couldn't test |
| V6 | Optimization (lock contention) | N/A | coder | Blocked - couldn't test |
| V7 | Test generation | N/A | coder | Blocked - couldn't test |

## Issues Found

| ID | Severity | Component | Summary |
|----|----------|-----------|---------|
| 0034 | Critical | dispatcher/loop | Chat RPC returns agent manifest JSON instead of LLM response |
| 0035 | High | rpc/server | Status RPC hardcodes token budget (always 0/100000) |
| 0036 | High | dispatcher | Classifier fallback causes severe agent misrouting |
| 0037 | High | agent/loop | Chat agent produces empty reply (has_report=false) |
| 0038 | High | daemon/queue | Stale queue jobs consume budget on daemon restart |
| 0039 | Medium | agent/loop | Planner returns empty content, hits convergence detection |
| 0040 | Medium | agent/loop | Tool termination signals skip LLM follow-up (#0005 recurrence) |
| 0041 | Medium | daemon/components | Classifier uses unreachable local LLM, no fallback grace |
| 0042 | Medium | orchestrator | BudgetExceededError classified non-retryable but still retried |

## Patterns Observed

### 1. Agent Routing Failure Cascade
When the local LLM classifier is down (127.0.0.1:8080 unreachable):
- Keyword fallback has near-random routing accuracy
- Code tasks go to `chat`, `scheduler`, or `committer` -- never to `coder`
- Low confidence scores (0.02-0.3) are accepted without warning
- The `coder` agent is effectively unreachable without a working classifier

### 2. Token Budget Death Spiral
Once budget is exceeded:
1. All LLM calls fail immediately
2. Job retry logic re-queues with backoff
3. Escalation creates new planning steps that also fail
4. Each failure cycle generates 5-10 log entries
5. Dead letter queue fills rapidly (49 entries in <10 minutes)

### 3. Empty Response Pattern
Multiple agents exhibit `has_report=false`:
- `chat` agent: Always empty (every test)
- `committer` agent: Empty after tool termination
- `planner` agent: Empty content from LLM, triggers convergence detection
- The common thread is the LLM response extraction failing silently

### 4. Task Orchestration Overhead
A simple "write a Go function" request triggers:
1. Classifier call (fails, falls to keyword)
2. Dispatcher routing
3. Agent loop creation
4. LLM call
5. If compound: planner call + strategic planning + tactical scheduling + worker claiming + execution + review
6. Review may reject and create revision steps

This multi-step pipeline consumes significant tokens before any user-facing work happens.

## Ratings Summary

| Dimension | Rating | Notes |
|-----------|--------|-------|
| Correctness | Fail | 0/8 standard tests passed; all routed to wrong agent or empty response |
| Communication | Poor | No useful responses returned; empty output or JSON dumps |
| Efficiency | Wasteful | Task orchestration consumes budget before user work; retry storms |
| Cleverness | Minimal | No evidence of proactive behavior; no code reading before modification |
| Robustness | Fragile | Single point of failure (classifier) cascades to total system failure |
| Helpfulness | Pointless | Users receive no useful output from any code intent query |

## Recommendations

1. **Fix agent routing first** (P0): Without correct routing, no code task can succeed. Improve keyword fallback with explicit code-related rules.
2. **Fix empty response bug** (P0): The `has_report=false` -> empty reply path must return something useful.
3. **Add budget safeguards** (P1): Reserve budget for new requests; clear stale tasks on restart.
4. **Add classifier resilience** (P1): Circuit breaker for unreachable classifier; use main LLM as fallback.
5. **Reduce orchestration overhead** (P2): Simple code generation shouldn't need planning/review cycles.
