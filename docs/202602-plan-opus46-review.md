# Meept Codebase Review & Remediation Plan

**Date**: 2026-02-06
**Reviewer**: Claude Opus 4.6 (8 parallel code-explorer subagents)
**Scope**: Full codebase — all Python, Rust, JS, config, docs, and tests

---

## Executive Summary

An in-depth review of the entire meept codebase identified **~220 issues** across 8 subsystems. The most alarming finding: **three major security components (PromptGuard, OutputMonitor, InputSanitizer) are implemented but never integrated into the agent loop**, leaving the system completely vulnerable to prompt injection and credential leakage despite having defenses available. Additional critical issues include fail-open permission defaults, dual security systems creating bypass opportunities, unbounded memory growth, non-atomic data operations, and numerous incomplete implementations.

### Issue Counts by Severity

| Subsystem | Critical | High | Medium | Low | Total |
|-----------|----------|------|--------|-----|-------|
| Security | 17 | 28 | 15 | 0 | 60 |
| Agent | 5 | 7 | 27 | 17 | 56 |
| Memory & Scheduler | 2 | 18 | 23 | 4 | 47 |
| Tools & Communication | 3 | 10 | 14 | 8 | 35 |
| Core Infrastructure | 1 | 4 | 8 | 6 | 19 |
| LLM & Skills | 1 | 5 | 8 | 5 | 19 |
| CLI & ClawSkills | 0 | 3 | 6 | 5 | 14 |
| Test Coverage | 0 | 5 | 7 | 2 | 14 |
| **Total** | **29** | **80** | **108** | **47** | **~264** |

---

## Sprint 1 — CRITICAL: Security Integration (Weeks 1-2)

These are must-fix-now issues. The system should not be deployed without them.

### 1.1 Integrate PromptGuard into Agent Loop

**Files**: `src/meept/agent/loop.py`, `src/meept/security/prompt_guard.py`

**Problem**: `PromptGuard` is fully implemented (boundary markers, safety reminders, input/output wrapping) but **never instantiated or called anywhere in the codebase**. User messages go directly into conversation history without wrapping — complete prompt injection vulnerability.

**Action**:
- [ ] Instantiate `PromptGuard` in daemon startup, pass to `AgentLoop`
- [ ] Call `wrap_user_input()` before appending user messages to history (loop.py ~line 143)
- [ ] Call `wrap_tool_output()` in `ActionExecutor` before returning tool results
- [ ] Add post-response validation that model didn't echo boundary marker instructions

### 1.2 Integrate OutputMonitor

**Files**: `src/meept/agent/executor.py`, `src/meept/security/output_monitor.py`, `src/meept/core/daemon.py`

**Problem**: `OutputMonitor` is fully implemented (credential detection, path scanning, exfiltration detection) but **never instantiated**. Daemon creates `PermissionManager` (daemon.py:216) but never `OutputMonitor`. API keys, passwords, AWS credentials can leak freely in tool output.

**Action**:
- [ ] Instantiate `OutputMonitor` in daemon startup
- [ ] Pass to `ActionExecutor` (currently receives `None`)
- [ ] Call `check_output()` before returning responses from `AgentLoop.run_once()`
- [ ] Ensure `redact_sensitive()` redacts ALL detected threats, not just some categories

### 1.3 Integrate InputSanitizer for All User Input

**Files**: `src/meept/agent/loop.py`, `src/meept/security/sanitizer.py`

**Problem**: `InputSanitizer` exists (53-196 lines of injection patterns) but is **only used for ClawSkills** (clawskills/security.py:59). User input and MCP tool output bypass sanitization entirely.

**Action**:
- [ ] Instantiate `InputSanitizer` in daemon startup
- [ ] Sanitize all user messages in `run_once()` before processing
- [ ] Sanitize MCP tool outputs before injecting into agent context
- [ ] Add `reject_on_threat` mode that raises exception for severe threats

### 1.4 Remove Dual Security System — Consolidate to SecurityEngine Only

**Files**: `src/meept/security/permissions.py`, `src/meept/security/engine.py`, `src/meept/agent/executor.py`, `src/meept/core/daemon.py`

**Problem**: Both `PermissionManager` (permissions.py) and `SecurityEngine` (engine.py) exist and are used. Dual code paths create inconsistent enforcement and bypass opportunities.

**Action**:
- [ ] Deprecate `PermissionManager` — mark as legacy
- [ ] Update daemon.py to use only `SecurityEngine` for all security checks
- [ ] Update `ActionExecutor` to require `SecurityEngine` (not `Any`)
- [ ] Remove `isinstance(SecurityEngine)` branching in executor/orchestrator

### 1.5 Fix Fail-Open Permission Default

**File**: `src/meept/agent/loop.py:474`

**Problem**: If security manager has no `check` or `check_permission` method, the function returns `(True, "Security manager has no check method")` — **allows all actions by default** when security is misconfigured.

**Action**:
- [ ] Change default to DENY: return `(False, "Security manager has no check method")`
- [ ] Add startup validation that security manager has required methods

### 1.6 Fix Financial Blocking Bypass

**File**: `src/meept/security/engine.py:411-412`

**Problem**: Financial blocking can be disabled via config, violating the P6 immutable deny principle from the security plan.

**Action**:
- [ ] Remove config check — financial patterns should ALWAYS block
- [ ] Make immutable rule checking apply at ALL risk levels (not just CRITICAL)

### 1.7 Implement Actual Confirmation Workflow

**Files**: `src/meept/security/permissions.py:287-333`, `src/meept/comm/server.py`

**Problem**: `request_confirmation()` creates a Future but nothing ever resolves it. No RPC method, no UI integration. HIGH/CRITICAL actions that "require confirmation" are actually blocked forever.

**Action**:
- [ ] Add `daemon.approve_action(request_id)` RPC method
- [ ] Publish bus message to frontend with confirmation request
- [ ] Implement timeout mechanism to auto-deny (current 300s timeout in executor)
- [ ] Wire UI (web/telegram/CLI) to display and handle confirmation requests

---

## Sprint 2 — HIGH: Agent System Bugs (Weeks 3-4)

### 2.1 Fix Critical `AttributeError` in `front.py`

**File**: `src/meept/agent/front.py:154-166`

**Problem**: Line 154 calls `planner.decompose()` which returns `list[TaskStep]`, stored in `plan`. But line 161 accesses `plan.steps`, treating it as a `TaskPlan` object. This raises `AttributeError` on every planning path.

**Action**:
- [ ] Change to `steps = await self._planner.decompose(message)` and use `steps` directly
- [ ] Or wrap in `TaskPlan(steps=plan, ...)` before publishing

### 2.2 Fix None LLM Client Crash in WorkerFactory

**File**: `src/meept/agent/worker_factory.py:95-135`

**Problem**: If both `model_resolver` and `llm_factory` fail, `llm_client` remains `None` and is passed to `AgentLoop`. The loop crashes when it tries to call `self._llm.chat()`.

**Action**:
- [ ] Raise exception if `llm_client` is `None` after resolution attempts
- [ ] Do not create broken `AgentLoop` instances

### 2.3 Fix Unbounded Conversation History Growth

**File**: `src/meept/agent/loop.py:102, 383-387`

**Problem**: `_conversations` dict grows unbounded. Each conversation accumulates all messages forever — memory leaks and token limit issues.

**Action**:
- [ ] Implement conversation pruning (keep last N messages, summarize old context)
- [ ] Add max conversation count with LRU eviction
- [ ] Add explicit cleanup method called on conversation end

### 2.4 Fix Pending Security Context Leak

**File**: `src/meept/agent/loop.py:106, 208-216, 162-166`

**Problem**: If LLM call fails, `_pending_security_context` is never cleared. It leaks to the next turn, potentially applying wrong security context.

**Action**:
- [ ] Clear `_pending_security_context` in error paths or in a `finally` block

### 2.5 Fix Memory Leak in CollaborativePlanner

**File**: `src/meept/agent/collaborative_planner.py:160, 276, 333`

**Problem**: `_pending` dictionary grows unbounded. Reviews are added but only removed in `reject()`. Approved plans remain in memory forever.

**Action**:
- [ ] Remove entries from `_pending` in `approve()` after approval
- [ ] Implement periodic cleanup of old entries (TTL-based)

### 2.6 Add Exception Handling to Front Agent Dispatch

**File**: `src/meept/agent/front.py:90-174`

**Problem**: `dispatch()` has no try-except wrapper. Any exception from planner, orchestrator, or default loop propagates uncaught, potentially crashing the bus handler.

**Action**:
- [ ] Wrap entire dispatch method in try-except
- [ ] Return error message to user on failure
- [ ] Log exception with full traceback

### 2.7 Fix asyncio.CancelledError Swallowing

**Files**: `src/meept/agent/planner.py:129-133`, `src/meept/agent/worker_factory.py:102,111`

**Problem**: Bare `except Exception` catches `asyncio.CancelledError`, preventing graceful shutdown.

**Action**:
- [ ] Add `except (asyncio.CancelledError, KeyboardInterrupt): raise` before general exception handlers in all agent files

### 2.8 Fix Tool Argument Validation Gap

**File**: `src/meept/agent/executor.py:125`

**Problem**: `arguments` dict passed directly to `tool.execute(**arguments)` without validation. Malformed arguments could crash tools.

**Action**:
- [ ] Validate arguments against tool schema before execution
- [ ] Wrap in try-except with better error messages

---

## Sprint 3 — HIGH: Memory & Data Integrity (Weeks 5-6)

### 3.1 Fix Non-Atomic Memory Consolidation

**File**: `src/meept/memory/consolidation.py:98-119`

**Problem**: Consolidation creates summaries then deletes originals in separate operations. If deletion fails after summary creation, duplicates exist. If summary fails after deletions, data is lost.

**Action**:
- [ ] Wrap consolidation in a database transaction
- [ ] Implement rollback on partial failure
- [ ] Track created summary IDs for cleanup on error

### 3.2 Fix Uninitialized `_bus` in MemoryManager

**File**: `src/meept/memory/manager.py:329-333, 353, 378`

**Problem**: `_bus` attribute is referenced in handlers but never initialized in `__init__()`. Only set in `subscribe_to_bus()`. Handlers called before subscription cause `AttributeError`.

**Action**:
- [ ] Initialize `self._bus = None` in `__init__()`
- [ ] Add guards in handler methods checking `if self._bus is None`

### 3.3 Add Commit Error Handling

**Files**: `src/meept/memory/episodic.py:187,322`, `src/meept/memory/task_memory.py:185,303`

**Problem**: `await self._db.commit()` calls have no error handling. Failed commits appear to succeed.

**Action**:
- [ ] Wrap commits in try-except with rollback
- [ ] Log and raise on commit failure

### 3.4 Fix FTS Rank Normalization Bug

**File**: `src/meept/memory/episodic.py:481-489`

**Problem**: `_normalise_fts_rank()` inverts ranking — more relevant = lower score.

**Action**:
- [ ] Fix to use `1.0 / (1.0 + abs(rank))` mapping

### 3.5 Add Job Persistence

**File**: `src/meept/scheduler/scheduler.py:225-250`

**Problem**: Dynamically added jobs are only stored in memory. Restart loses all user-scheduled jobs.

**Action**:
- [ ] Persist job definitions to database or config file
- [ ] Restore jobs on startup

### 3.6 Fix Cron-to-Interval Degradation

**File**: `src/meept/scheduler/scheduler.py:159-164`

**Problem**: When apscheduler not installed, cron triggers silently converted to 1-hour intervals. "Midnight daily" runs every hour.

**Action**:
- [ ] Reject cron jobs when apscheduler unavailable, or
- [ ] Parse basic cron expressions natively in fallback

### 3.7 Add Job Handler Timeouts

**File**: `src/meept/scheduler/jobs.py:176-178`

**Problem**: Job handlers execute with no timeout. Hung handlers block scheduler indefinitely.

**Action**:
- [ ] Wrap execution in `asyncio.wait_for()` with configurable timeout (default 300s)

### 3.8 Fix Export Pagination

**File**: `src/meept/memory/export.py:65,136`

**Problem**: Exports load all items into memory at once (limit=10,000). Large databases cause OOM.

**Action**:
- [ ] Implement streaming export with pagination
- [ ] Write to temp file, atomic rename on completion

---

## Sprint 4 — HIGH: Tools, Communication & Security Hardening (Weeks 7-8)

### 4.1 Fix SQL Injection Risks

**Files**: `src/meept/security/engine.py:442-460`, `src/meept/memory/episodic.py:316-319`, `src/meept/memory/task_memory.py:297-299`

**Problem**: Direct string interpolation in SQL queries. While parameterized in most places, several methods use f-strings for dynamic placeholder counts.

**Action**:
- [ ] Validate all inputs match `^[a-zA-Z0-9_]+$` before SQL construction
- [ ] Or use parameterized query builders consistently

### 4.2 Fix TOCTOU Vulnerabilities

**File**: `src/meept/security/engine.py:498, 582-636`

**Problem**: Path resolution race conditions — file can be swapped between resolve and check. Override check-then-use races.

**Action**:
- [ ] Use file descriptor-based checks for path operations
- [ ] Use database transactions with proper isolation for overrides

### 4.3 Add Rate Limiting

**Files**: `src/meept/security/engine.py` (all methods), `src/meept/calendar/gcal.py:186-244`

**Problem**: No rate limiting on security checks, override creation, audit queries, or Google Calendar API calls.

**Action**:
- [ ] Add rate limiting to security engine methods
- [ ] Implement exponential backoff on 429 responses for Calendar API

### 4.4 Harden Tirith Integration

**File**: `src/meept/security/tirith.py:83-84, 96-101`

**Problem**: If tirith not installed or times out, command executes — fail-open design. 2-second timeout too short.

**Action**:
- [ ] Add `require_tirith` config flag
- [ ] Increase default timeout to 10s
- [ ] Make tirith mandatory or explicitly document fail-open behavior

### 4.5 Fix Credential Storage

**File**: `src/meept/calendar/auth.py:163-171`

**Problem**: OAuth credentials including `client_secret` written to disk as plain JSON.

**Action**:
- [ ] Use OS keyring for sensitive credentials
- [ ] Or encrypt token file with key derived from user credential

### 4.6 Add Missing Input Validation in Web Routes

**File**: `src/meept/comm/web/routes.py`

**Problem**: Web routes use bus messages for async request/response but have no CSRF protection, no request size limits beyond Pydantic validation, and pending futures dict has no cleanup for abandoned requests.

**Action**:
- [ ] Add CSRF token middleware
- [ ] Add request size limits
- [ ] Add TTL-based cleanup for pending futures dict

### 4.7 Fix Git Workspace Silent Failures

**File**: `src/meept/agent/workspace.py:268-270`

**Problem**: If `git` not in PATH, workspace creation logs warning but marks as initialized. Subsequent operations fail confusingly.

**Action**:
- [ ] Raise exception during `create()` if git is unavailable
- [ ] Or disable workspace features entirely and communicate clearly

---

## Sprint 5 — MEDIUM: Test Coverage & Quality (Weeks 9-10)

### 5.1 Modules With Zero Test Coverage

The following source modules have **no test files at all**:

| Module | Risk | Priority |
|--------|------|----------|
| `src/meept/agent/loop.py` | Critical — core reasoning loop | **P0** |
| `src/meept/agent/planner.py` | High — task decomposition | P1 |
| `src/meept/agent/executor.py` | High — permission-checked execution | P1 |
| `src/meept/security/prompt_guard.py` | Critical — prompt injection defense | **P0** |
| `src/meept/security/output_monitor.py` | Critical — credential leak defense | **P0** |
| `src/meept/security/tirith.py` | High — pre-execution scanning | P1 |
| `src/meept/security/tls.py` | Medium — TLS cert generation | P2 |
| `src/meept/security/seed_rules.py` | Medium — security rule database | P2 |
| `src/meept/memory/manager.py` | High — memory orchestration | P1 |
| `src/meept/memory/consolidation.py` | High — memory lifecycle | P1 |
| `src/meept/memory/export.py` | Medium — memory export | P2 |
| `src/meept/memory/personality.py` | Medium — personality model | P2 |
| `src/meept/memory/task_memory.py` | Medium — domain-specific memory | P2 |
| `src/meept/tools/loader.py` | Medium — tool loading | P2 |
| `src/meept/tools/mcp_manager.py` | Medium — MCP server management | P2 |
| `src/meept/tools/mcp_client.py` | Medium — MCP tool execution | P2 |
| `src/meept/tools/builtin/shell.py` | High — shell command execution | P1 |
| `src/meept/tools/builtin/filesystem.py` | Medium — file operations | P2 |
| `src/meept/tools/builtin/web_fetch.py` | Medium — web fetching | P2 |
| `src/meept/tools/builtin/web_search.py` | Medium — web search | P2 |
| `src/meept/comm/server.py` | Medium — Unix socket server | P2 |
| `src/meept/comm/telegram_bot.py` | Low — telegram integration | P3 |
| `src/meept/comm/web/app.py` | Medium — web app factory | P2 |
| `src/meept/comm/web/auth.py` | High — JWT authentication | P1 |
| `src/meept/comm/web/routes.py` | Medium — API routes | P2 |
| `src/meept/llm/client.py` | High — LLM communication | P1 |
| `src/meept/llm/providers.py` | Medium — provider config | P2 |
| `src/meept/scheduler/jobs.py` | Medium — job definitions | P2 |
| `src/meept/calendar/auth.py` | Medium — OAuth flow | P2 |
| `src/meept/calendar/gcal.py` | Medium — Calendar API | P2 |
| `cli/app.py` | Low — TUI app | P3 |
| `cli/screens/*.py` | Low — TUI screens | P3 |

### 5.2 Write Security-Critical Tests (P0)

- [ ] Test prompt injection detection and blocking end-to-end
- [ ] Test credential leak detection in output
- [ ] Test fail-closed behavior when security is misconfigured
- [ ] Test boundary marker injection attempts
- [ ] Test financial action blocking (immutable deny)
- [ ] Test shell command injection patterns
- [ ] Test path traversal in filesystem tools

### 5.3 Write Agent Loop Tests (P0)

- [ ] Test conversation lifecycle (create, continue, cleanup)
- [ ] Test tool call argument validation
- [ ] Test iteration limit enforcement
- [ ] Test memory context injection
- [ ] Test error recovery in LLM calls

### 5.4 Write Integration Tests (P1)

- [ ] End-to-end: user message -> agent -> tool execution -> response
- [ ] Security pipeline: sanitize -> guard -> execute -> monitor -> respond
- [ ] Memory: store -> consolidate -> recall
- [ ] Scheduler: create job -> execute -> report

---

## Sprint 6 — MEDIUM: Architecture & Quality (Weeks 11-12)

### 6.1 Define Consistent Error Handling Strategy

**Problem**: Each file handles errors differently — some raise, some return error strings, some log and continue. No consistent error propagation.

**Action**:
- [ ] Define custom exception hierarchy: `MeeptError` -> `SecurityError`, `AgentError`, `ToolError`, etc.
- [ ] Document error propagation strategy for each layer
- [ ] Replace bare `except Exception` with specific exception types

### 6.2 Replace `Any` Type Annotations with Protocols

**Problem**: Many parameters typed as `Any` (bus, llm_client, security) defeating type checking.

**Action**:
- [ ] Define `Protocol` classes for Bus, LLMClient, SecurityManager interfaces
- [ ] Update all type annotations to use protocols
- [ ] Enable mypy strict mode incrementally

### 6.3 Add Pipeline-Level Timeout

**File**: `src/meept/scheduler/pipelines.py:122-291`

**Problem**: Individual steps have timeouts but entire pipeline has no global timeout.

**Action**:
- [ ] Add pipeline-level timeout parameter
- [ ] Wrap entire execution in `asyncio.wait_for()`

### 6.4 Fix Bus Subscription Lifecycle

**Problem**: Multiple components (Orchestrator, MemoryManager) have `subscribe_to_bus()` methods that are never called, or are called inconsistently.

**Action**:
- [ ] Standardize bus subscription in daemon startup
- [ ] Or remove unused subscription methods

### 6.5 Add Personality Profile Validation

**File**: `src/meept/memory/personality.py:202-212`

**Problem**: LLM output used directly without validation. Malformed output corrupts personality profile.

**Action**:
- [ ] Validate required section headings exist
- [ ] Backup before overwrite
- [ ] Reject empty or malformed content

### 6.6 Update Injection Patterns

**File**: `src/meept/security/sanitizer.py:53-196`

**Problem**: Missing modern injection patterns (Gemini `<think>`, Claude artifacts, function calling injection).

**Action**:
- [ ] Add 2024-2025 injection patterns
- [ ] Add detection for mid-text role marker injection (not just line beginnings)

---

## Sprint 7 — LOW: Completion & Polish (Weeks 13-16)

### 7.1 Complete Tauri Menubar App

**Status**: Barely scaffolded — minimal Rust backend, no JS frontend.

**Action**:
- [ ] Implement Rust backend with Unix socket IPC to daemon
- [ ] Build JS frontend (status display, chat, metrics)
- [ ] Wire tray icon state changes (idle/working/error)
- [ ] Test daemon communication end-to-end

### 7.2 Complete Telegram Integration

**Status**: File exists but end-to-end flow untested.

**Action**:
- [ ] Test full message->daemon->LLM->response flow
- [ ] Add rate limiting per user
- [ ] Add confirmation UI for high-risk actions

### 7.3 Complete Web Interface

**Status**: Routes exist, auth scaffolded, but integration incomplete.

**Action**:
- [ ] Test OAuth login flow
- [ ] Add WebSocket/SSE for real-time chat
- [ ] Add CORS configuration
- [ ] Frontend polish

### 7.4 Add Service Installation Automation

**Problem**: Service templates exist but no install/uninstall automation.

**Action**:
- [ ] Implement `make install-service` / `make uninstall` targets
- [ ] Add variable substitution in service templates
- [ ] Test on macOS (launchd) and Linux (systemd)

### 7.5 Clean Up Empty `__init__.py` Files

Multiple packages have empty `__init__.py` files with no exports.

**Action**:
- [ ] Add `__all__` exports to: `agent/__init__.py`, `memory/__init__.py`, `security/__init__.py`, `tools/__init__.py`, `comm/__init__.py`, `scheduler/__init__.py`, `calendar/__init__.py`, `models/__init__.py`

### 7.6 README Accuracy

**Problem**: README and `docs/implementation-plan.md` have completion percentages that may not reflect current state after this review.

**Action**:
- [ ] Update implementation-plan.md completion percentages
- [ ] Ensure README accurately describes what actually works vs. what is scaffolded

---

## Appendix A: Critical Bug Quick Reference

| # | File:Line | Severity | Issue |
|---|-----------|----------|-------|
| 1 | `loop.py:474` | CRITICAL | Permission check defaults to ALLOW on misconfiguration |
| 2 | `front.py:154-166` | CRITICAL | `plan.steps` AttributeError (list vs TaskPlan) |
| 3 | `worker_factory.py:95-135` | CRITICAL | None LLM client passed to AgentLoop |
| 4 | `engine.py:411-412` | CRITICAL | Financial blocking configurable (should be immutable) |
| 5 | `engine.py:340-348` | CRITICAL | Immutable rules only checked at CRITICAL level |
| 6 | `prompt_guard.py` | CRITICAL | Never integrated — prompt injection wide open |
| 7 | `output_monitor.py` | CRITICAL | Never integrated — credential leaks possible |
| 8 | `sanitizer.py` | CRITICAL | Only used for ClawSkills, not user input |
| 9 | `permissions.py:287-333` | CRITICAL | Confirmation workflow never resolves futures |
| 10 | `consolidation.py:98-119` | CRITICAL | Non-atomic consolidation — data loss risk |
| 11 | `manager.py:329-378` | CRITICAL | Uninitialized `_bus` attribute — AttributeError |
| 12 | `scheduler.py:159-164` | HIGH | Cron silently degraded to 1-hour interval |

## Appendix B: Security Integration Checklist

```
Agent Loop Security Pipeline (SHOULD be):

  User Input
    → InputSanitizer.sanitize()          ← NOT CONNECTED
    → PromptGuard.wrap_user_input()      ← NOT CONNECTED
    → Add to conversation history
    → LLM call
    → PromptGuard safety reminders       ← NOT CONNECTED
    → Tool calls
      → SecurityEngine.check()           ✓ Connected (mostly)
      → Tirith pre-scan (shell only)     ✓ Connected
      → ActionExecutor.execute()         ✓ Connected
      → OutputMonitor.check_output()     ← NOT CONNECTED
    → Final response
    → OutputMonitor.redact_sensitive()   ← NOT CONNECTED
    → Return to user
```

## Appendix C: Test Coverage Matrix

```
Source Module                    Has Tests?  Notes
────────────────────────────────────────────────────────
core/config.py                  ✓
core/bus.py                     ✓
core/registry.py                ✓
core/daemon.py                  ✗           Critical gap
agent/loop.py                   ✗           Critical gap
agent/planner.py                ✗           Critical gap
agent/executor.py               ✗           Critical gap
agent/front.py                  ✓
agent/orchestrator.py           ✓
agent/worker_factory.py         ✓
agent/workspace.py              ✓
agent/collaborative_planner.py  ✓
security/engine.py              ✓
security/permissions.py         ✓
security/sanitizer.py           ✓
security/prompt_guard.py        ✗           Critical gap
security/output_monitor.py      ✗           Critical gap
security/tirith.py              ✗
security/tls.py                 ✗
security/seed_rules.py          ✗
llm/budget.py                   ✓
llm/models.py                   ✓
llm/providers.py                ✓
llm/resolver.py                 ✓
llm/client.py                   ✗
skills/parser.py                ✓
skills/registry.py              ✓
skills/discovery.py             ✓
skills/models.py                ✓
skills/tool_filter.py           ✓
skills/executor.py              ✓
skills/dispatcher.py            ✓
tools/interface.py              ✓
tools/mcp_auth.py               ✓
tools/mcp_client.py             ✗
tools/mcp_manager.py            ✗
tools/loader.py                 ✗
tools/builtin/shell.py          ✗           Security-critical
tools/builtin/filesystem.py     ✗
tools/builtin/schedule_tool.py  ✓
tools/builtin/skill_tools.py    ✓
tools/builtin/web_fetch.py      ✗
tools/builtin/web_search.py     ✗
memory/manager.py               ✗
memory/episodic.py              ✓
memory/task_memory.py           ✗
memory/consolidation.py         ✗
memory/export.py                ✗
memory/personality.py           ✗
comm/protocol.py                ✓
comm/server.py                  ✗
comm/telegram_bot.py            ✗
comm/web/app.py                 ✗
comm/web/auth.py                ✗
comm/web/routes.py              ✗
scheduler/scheduler.py          ✓
scheduler/pipelines.py          ✓
scheduler/jobs.py               ✗
calendar/auth.py                ✗
calendar/gcal.py                ✗
clawskills/* (all files)        ✓           Good coverage
```
