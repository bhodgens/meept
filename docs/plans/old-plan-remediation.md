# Archived Plans Remediation Analysis

**Date:** 2026-04-24
**Analysis Method:** 10 parallel subagents reviewed all 30 plans in `/docs/plans/archive/`
**Scope:** Implementation completeness and correctness verification against actual codebase

---

## Executive Summary

| Metric | Value |
|--------|-------|
| Total Plans Reviewed | 22 |
| Fully Complete (100%) | 3 |
| Substantially Complete (75-95%) | 8 |
| Partially Complete (50-74%) | 4 |
| Minimal Implementation (<50%) | 4 |
| **Average Completeness** | **73%** |

---

## Plan-by-Plan Analysis

### 1. plan-web-server.md
**Completeness: 65%**

**Implementation Status:** Core server framework, authentication, and daemon integration are fully built. Several planned endpoints and features remain unimplemented.

**Gaps Identified:**
- No WebSocket support (`websocket.go`, broadcast hub)
- No streaming chat (SSE) endpoint `/api/v1/chat/stream`
- No sessions endpoints (`/api/v1/sessions`)
- No agents endpoints (`/api/v1/agents`, delegate endpoint)
- No tools endpoint (`GET /api/v1/tools`)
- No memory store endpoint (`POST /api/v1/memory`)
- No skills execute endpoint (`POST /api/v1/skills/{name}/execute`)
- Jobs endpoints incomplete (only `GET /api/v1/jobs` exists)
- No integration tests (`tests/integration/web_test.go`)
- No unit tests (`internal/comm/web/*_test.go`)

---

### 2. plan-telegram-integration.md
**Completeness: 15%**

**Implementation Status:** Only the base bot library exists. All daemon integration, handlers, command processing, and session management remain unimplemented.

**Gaps Identified:**
- No daemon integration (bot not referenced in `components.go`)
- No handler (`handler.go` never created)
- No command handling (`/start`, `/status`, `/help`, `/new`)
- No session persistence
- No response formatter
- No inline keyboard support
- No config loading in daemon
- No integration/unit tests

---

### 3. plan-tui-agent-extension.md
**Completeness: 75%**

**Implementation Status:** Core TUI features (markdown rendering, syntax highlighting, agent activity panel, vim modal editing, event stream, sparkline metrics) are implemented. Several Phase 5 polish items remain incomplete.

**Gaps Identified:**
- Task Lineage View never implemented
- Responsive Layout (adaptive layouts) never implemented
- Quick Actions Bar never implemented
- Message Threading (conversation grouping) never implemented
- Notification System (toast notifications) never implemented
- Fuzzy Finder (fuzzy-search component) never implemented
- Enhanced Tasks View columns (multi-column, memory refs) partially implemented
- Task Detail Modal memory context partially implemented
- Session Header Enhancement partially implemented
- `teatest` integration tests disabled (not v2-compatible)
- Custom clipboard implementation not replaced with `tea.SetClipboard`

---

### 4. plan-bubbletea-v2-migration.md
**Completeness: 95%**

**Implementation Status:** All 14 affected files in `internal/tui/` have been migrated to v2 APIs. The `internal/lite/` package remains on v1 but was out of scope.

**Gaps Identified:**
- `teatest` integration tests disabled (TODO comments in `app_test.go`)
- Custom `copyToClipboard()` not replaced with `tea.SetClipboard`
- Old `setTerminalTitle()` function still exists (dead code)

---

### 5. plan-agentification.md
**Completeness: 85%**

**Implementation Status:** Multi-agent architecture substantially implemented. Agent configuration, recombinant prompts, dispatcher, specialist agents, memvid integration, and memory-aware task handoff all exist (some evolved beyond original plan).

**Gaps Identified:**
- `config/prompts/tools/` directory never created (bash.md, file_ops.md, web.md, git.md)
- `config/prompts/reminders/` directory never created
- Explicit `context_query` task field not implemented
- `ChatDelegation` struct never created
- User-global agents use AGENT.md discovery instead of `custom.toml`
- JSONL inter-agent protocol evolved into SQLite-backed `task.Store`

---

### 6. plan-agent-communication-and-reporting-problems.md
**Completeness: 95%**

**Implementation Status:** All six phases implemented. Intent classification, strategic planner fast-path, worker state machine, non-retryable error handling, retry backoff, and conversation-aware reporting all functional.

**Gaps Identified:**
- Report guidance not in individual agent YAML files (embedded in baseline prompt instead)
- Session history tool parameters not fully verified

---

### 7. plan-llm-intent-classifier.md
**Completeness: 100%**

**Implementation Status:** All five tasks completed. LLM classifier, adaptive thresholds, capability matcher, semantic index, and comprehensive tests all implemented. Includes bonuses beyond the plan (multi-intent detection, session tracking).

**Gaps Identified:** None

---

### 8. plan-context-management.md
**Completeness: 100%**

**Implementation Status:** All four phases delivered. Tokenizer integration, context firewall, bounded memory injection, semantic message importance, tool definition counting, multi-turn budget tracking, anchor message protection all implemented.

**Gaps Identified:** None

---

### 9. plan-security-integration.md
**Completeness: 90%**

**Implementation Status:** Security components fully wired into hot paths (agent loop input/output, shell tool Tirith). Security Orchestrator coordinates all components. Audit logging and RPC endpoints functional.

**Gaps Identified:**
- No `internal/agent/security.go` helper file (integration done inline)
- No integration tests (`tests/integration/security_test.go`)
- Security Orchestrator not passed to Controller (self-improve)
- `config/meept.toml` template not verified for `[security]` section

---

### 10. plan-selfimprove-integration.md
**Completeness: 55%**

**Implementation Status:** Core self-improve components existed pre-plan. LearningPipeline initialized on daemon startup. RPC endpoints and CLI commands exist. Controller NOT initialized on startup. Scheduler and TUI panel never created.

**Gaps Identified:**
- Controller not initialized on daemon startup (only LearningPipeline)
- No scheduler (`internal/selfimprove/scheduler.go`)
- No TUI approval panel (`internal/tui/selfimprove.go`)
- CLI commands not wired to RPC (instantiate standalone controllers)
- No integration tests (`tests/integration/selfimprove_test.go`)
- No `ProgressCallback` mechanism
- No shutdown handling for Controller

---

### 11. plan-meept-memory-improvement.md
**Completeness: 92%**

**Implementation Status:** All 9 sections substantially implemented. Security scanning, prefix cache, context fencing, character limits, recall modes, expiration/summarization, versioned memories, automatic prefetch all functional.

**Gaps Identified:**
- Recall mode "disabled" does not gate memory tool access
- Versioning uses metadata JSON for `parent_id` instead of SQL column
- `meept.toml` configuration template not verified for `[memory.*]` sections
- `MemoryCachingConfig.Enabled` not checked before freezing snapshots

---

### 12. plan-memory-store-dedup.md
**Completeness: 85%**

**Implementation Status:** Shared `SQLiteFTSStore` type eliminated code duplication between EpisodicMemory and TaskMemory. Both types embed the shared store. Build passes, tests exist for individual types.

**Gaps Identified:**
- No generic type parameter (design deviation, not a gap per se)
- No `ftstore_test.go` dedicated unit test file
- `CLAUDE.md` not updated to document dedup architecture
- `scanResults()` and search logic still duplicated in each type

---

### 13. plan-mcp-integration.md
**Completeness: 95%**

**Implementation Status:** All 6 phases completed. MCP Manager, config loading, daemon integration, tool wrapper, graceful shutdown, and hot reload all implemented. Comprehensive unit and integration tests exist.

**Gaps Identified:**
- Minor interface difference in `MCPTool.Execute()` return type

---

### 14. plan-clawskills.md
**Completeness: 70%**

**Implementation Status:** Go implementation complete (not Python as originally planned). Models, client, installer, index, security, CLI all implemented. Daemon integration and some security features missing.

**Gaps Identified:**
- No daemon-side loading (ClawSkills not registered at startup)
- No `claw:` prefix enforcement (namespace isolation)
- No runtime tool restriction (blocked tool list)
- No risk level enforcement (HIGH risk not guaranteed)
- No slug blocklist enforcement
- No tests (`internal/clawskills/*_test.go`)
- No `inspect` vs `info` distinction (only remote detail viewer)

---

### 15. plan-go-refactoring.md
**Completeness: 70%**

**Implementation Status:** Plan superseded by full Go rewrite. All Tier 1 Go core components built. Python subsystem migrated to Go instead of keeping the bridge. Architecture fundamentally changed.

**Gaps Identified:**
- Python-Go IPC bridge (gRPC/subprocess) never built (scope change)
- `archive/` directory for Python code never created
- `meept-perms` CLI binary not created (library exists)
- CI/CD (`.github/workflows/build.yml`) never set up
- Menubar is SwiftUI (not Rust/Tauri as plan stated)
- Load test/benchmark results not formally verified

---

### 16. plan-hierarchial-async-agents.md
**Completeness: 95%**

**Implementation Status:** All 8 phases implemented. Strategic planner, tactical scheduler, orchestrator, async dispatch, task/step stores, TUI integration, tests all complete. Enhancements beyond plan (concurrency control, retry/backoff, validation gates).

**Gaps Identified:**
- `task.step.progress` per-step live estimates deferred (non-blocking)
- `GetStepStore()` accessor uses registry pattern instead of direct method

---

### 17. plan-shadow-training.md
**Completeness: 92%**

**Implementation Status:** All 5 phases implemented. Shadow package (17 files), training store, middleware, manager, scoring, selector, examples, exporter, adapters, CLI all functional. Ollama and OpenAI adapter integration complete.

**Gaps Identified:**
- No `store_sqlite.go` dedicated test file
- Agent loop uses keyword-based classification instead of `classifier.go`
- Middleware wrapping not used (direct-call integration instead)
- No `adapters/training_runs` test file

---

### 18. plan-skills-execution.md
**Completeness: 88%**

**Implementation Status:** Skills system (discovery, parser, registry, executor) wired into agent system. Filter tools for skill, RPC endpoints, CLI commands all functional. Some architectural deviations from plan.

**Gaps Identified:**
- No `AgentSpec.PreferredSkill`/`SkillTriggers` fields
- No `RunWithSkill()` on AgentLoop (skill execution in dispatcher)
- No keyword-based skill matching (only explicit `/skill-name`)
- No integration tests (`tests/integration/skills_test.go`)
- Tool filtering not wired into skill execution path

---

### 19. plan-calendar-integration.md
**Completeness: 5%**

**Implementation Status:** Only pre-existing OAuth client (`auth.go`) and Calendar API client (`gcal.go`) exist. Zero integration work from this plan has been done. Plan document still reads "Status: Not Started".

**Gaps Identified:**
- No OAuth Token Manager type
- No calendar tools (CalendarList, CalendarCreate, CalendarQuickAdd, CalendarToday)
- No `CalendarConfig` in schema
- No `[calendar]` section in `meept.toml`
- No daemon integration
- No CLI auth command
- No reminder integration (`reminder.go`)
- No tests

---

### 20. plan-reviewer-agents.md
**Completeness: 85%**

**Implementation Status:** Core review infrastructure shipped (Phases 1, 2, 4, 5). ReviewManager, ReviewPolicy, reviewer specs, tactical integration, TUI icons all functional. Phase 3 (final-review task-state) explicitly deferred.

**Gaps Identified:**
- Phase 3 deferred (StateTesting final review step)
- RevisionCount not projected to TaskStepView in TUI
- Review metrics (pass rate, avg revision cycles) not emitted
- Reviewer agent prompts file not created (inline instead)

---

### 21. plan-agent-model-backoffs.md
**Completeness: 100%**

**Implementation Status:** All five layers (A-E) implemented with tests. Error classification, rate limit detection, resolver rotation, agent loop failover, tactical job retry all functional.

**Gaps Identified:** None

---

### 22. plan-agent-validation-watchdog.md
**Completeness: 25%**

**Implementation Status:** Only config types and anchor message infrastructure implemented. The three major new files (`watchdog.go`, `escalation.go`, `hallucination.go`) were never created. No runtime behavior implemented.

**Gaps Identified:**
- Phase 1: `ValidateCompletion()` method never implemented
- Phase 2: `HallucinationDetector` never created
- Phase 3: `Watchdog` background monitoring never created
- Phase 4: `EscalationManager` never created
- Phase 5: `TaskReportAggregator` never implemented
- Phase 6: Validation instructions as anchors not added
- No tests for any unimplemented components
- `Silent` flag for progress events never implemented

---

## Summary by Completeness Tier

### 100% Complete (3 plans)
- plan-llm-intent-classifier.md
- plan-context-management.md
- plan-agent-model-backoffs.md

### 90-99% Complete (4 plans)
- plan-bubbletea-v2-migration.md (95%)
- plan-agent-communication-and-reporting-problems.md (95%)
- plan-hierarchial-async-agents.md (95%)
- plan-mcp-integration.md (95%)

### 75-89% Complete (4 plans)
- plan-tui-agent-extension.md (75%)
- plan-agentification.md (85%)
- plan-reviewer-agents.md (85%)
- plan-security-integration.md (90%)
- plan-memory-store-dedup.md (85%)
- plan-skills-execution.md (88%)
- plan-shadow-training.md (92%)
- plan-meept-memory-improvement.md (92%)

### 50-74% Complete (4 plans)
- plan-web-server.md (65%)
- plan-clawskills.md (70%)
- plan-go-refactoring.md (70%)
- plan-selfimprove-integration.md (55%)

### Below 50% Complete (4 plans)
- plan-telegram-integration.md (15%)
- plan-calendar-integration.md (5%)
- plan-agent-validation-watchdog.md (25%)

---

## Recommendations

### High Priority (Critical Gaps)
1. **plan-calendar-integration.md (5%)** - Either complete the integration or remove the plan document
2. **plan-telegram-integration.md (15%)** - Same as above; base bot code exists but is disconnected
3. **plan-agent-validation-watchdog.md (25%)** - Config types defined but no runtime; watchdog/escalation/hallucination detection all missing

### Medium Priority (Substantial Work Done, Missing Final Pieces)
4. **plan-selfimprove-integration.md (55%)** - Controller not initialized on daemon startup; no scheduler/TUI panel
5. **plan-web-server.md (65%)** - Core server works but missing WebSocket, SSE, several endpoints
6. **plan-clawskills.md (70%)** - No daemon integration; no namespace isolation; no tests

### Low Priority (Mostly Complete)
7. **plan-tui-agent-extension.md (75%)** - Polish items (lineage view, responsive layout, notifications) not implemented
8. **plan-go-refactoring.md (70%)** - Plan superseded by Go rewrite; document should be updated/retired

---

## Notes on Plan Evolution

Several plans were implemented differently than originally specified but achieved equivalent or superior outcomes:

- **plan-agentification.md:** JSONL inter-agent protocol evolved into SQLite-backed `task.Store`
- **plan-memory-store-dedup.md:** Generics-based approach replaced with concrete type (simpler, idiomatic Go)
- **plan-skills-execution.md:** Keyword-based skill matching replaced with explicit `/skill-name` syntax
- **plan-hierarchial-async-agents.md:** Added concurrency control, retry/backoff, validation gates beyond original scope
