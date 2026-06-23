# Plan Gap Remediation

**Created:** 2026-06-23
**Source:** Plan review synthesis from 11 subagent reports
**Status:** Pending implementation

---

## Executive Summary

This document consolidates all identified gaps from the comprehensive plan review conducted on 2026-06-23. Gaps are organized by source plan and priority.

**Total gaps identified:** 15 across 6 plans

| Priority | Count |
|----------|-------|
| High | 3 |
| Medium | 7 |
| Low | 5 |

---

## Plan-by-Plan Gap Analysis

### 1. Headroom Integration (`headroom-integration.md`)

**Completion:** 85% (22 of 27 tasks)

| ID | Priority | Gap | Impact | Remediation |
|----|----------|-----|--------|-------------|
| H1 | Medium | Config template (`config/meept.json5`) missing `compression` subsection under `agent` | Users copying template won't discover compression feature | Add `compression` block to `config/meept.json5` matching `AgentCompressionConfig` struct |
| H2 | Low | Stale backup file `internal/compress/log_compress.go.bak` | Dead code, repo bloat | Delete the file |
| H3 | Low | No dedicated `compression_metrics.go` in `internal/metrics/` | Plan divergence (functionally fine) | **Resolved (Option B):** Updated `headroom-integration.md` Phase 7 checklist with implementation note documenting that compression metrics are intentionally recorded inline in `collector.go` (`RecordCompression`, `recordCompression`) rather than a dedicated file |
| H4 | Low | No parity fixture tests for compression algorithms | Test coverage gap | Add fixture-based tests comparing compressed output to known-good baselines |

---

### 2. Automation Capability Extension (`2026-06-21-automation-capability-extension.md`)

**Completion:** 89% (17 of 19 phases)

| ID | Priority | Gap | Impact | Remediation |
|----|----------|-----|--------|-------------|
| A1 | High | No generic async hook config (`AsyncHookConfig` with `async: true` and `asyncRewake: true`) | Only session-end hook supports async; HTTP and file watcher hooks cannot be async | Extend `HTTPHookConfig` and `FileWatcherHookConfig` with `Async bool` field; wrap execution in goroutine when enabled |
| A2 | Medium | No "Do Not Disturb" mode in config or UI | Users cannot globally suppress notifications | Add `DoNotDisturb bool` to `NotificationsConfig`; add DND toggle to TUI/menubar |
| A3 | Medium | No designation audit/history log | No record of designation transitions (e.g., how many times session went `waiting_human` → `requires_approval`) | Add `DesignationHistory` table tracking `session_id`, `from_status`, `to_status`, `timestamp` |
| A4 | Medium | No HTTP hook config in schema (`HooksConfig` only has `FileWatcher`) | HTTP hooks must be wired manually at startup, not loaded from config | Add `HTTP []HTTPHookConfig` to `HooksConfig` struct |

---

### 3. Thread-Based Context Partitioning (`2026-06-20-thread-based-context-partitioning.md`)

**Completion:** 92% (11 of 12 tasks)

| ID | Priority | Gap | Impact | Remediation |
|----|----------|-----|--------|-------------|
| T1 | Low | Topic detection disabled by default (`EnableTopicDetection: false`, `MinMessagesForSummary: 20`) vs. plan spec (`true`, `5`) | Conservative defaults may delay thread creation | Update plan to reflect actual defaults, or adjust defaults to match spec |

---

### 4. Epistemic Memory Platform (`2026-06-21-epistemic-memory-platform.md`)

**Completion:** 100% (24 of 24 tasks)

| ID | Priority | Gap | Impact | Remediation |
|----|----------|-----|--------|-------------|
| E1 | Low | Flutter confirmation in `chat_provider.dart` not `tool_runner.dart` as specified | Plan divergence (functionally complete) | Update plan to reflect actual file location |
| E2 | Low | Ambient filtering in hook layer, not `AmbientExtractorConfig` as specified | Plan divergence (architecturally valid) | Update plan to reflect hook-layer filtering approach |
| E3 | Low | No dedicated `/api/v1/config/memory/epistemic` endpoint | Menubar app may need separate endpoint for config | Add endpoint to `config_service.go` if needed |

---

### 5. Agent Progress UI (`2026-06-15-agent-progress-ui.md`)

**Completion:** 96% (26 of 27 items)

| ID | Priority | Gap | Impact | Remediation |
|----|----------|-----|--------|-------------|
| P1 | Low | No performance/load testing | Rate limiting exists but no formal load testing performed | Add load test: simulate 100 concurrent WebSocket clients, measure event delivery latency |
| P2 | Low | Contradiction in plan text: line 140 says rate limiting IS implemented, line 235 says "optional and out of scope" | Documentation inconsistency | Update plan review checklist line 235 to match implementation |
| P3 | Low | SSE sends two `agent_progress` event types (legacy + synthesized) potentially causing duplicates | TUI/SSE clients may see duplicate progress events | Document this as intentional, or deduplicate in `handleChatStream` |

---

### 6. User Instructions (`2026-06-21-user-instructions-implementation.md`)

**Completion:** Unknown (subagent failed due to context limit)

| ID | Priority | Gap | Impact | Remediation |
|----|----------|-----|--------|-------------|
| U1 | High | Plan verification incomplete | Unknown if implementation matches spec | Re-run verification with smaller chunks or manual review |

---

### 7. Headroom Findings (`headroom-integration-findings.md`)

**Completion:** 40% actionable items addressed

| ID | Priority | Gap | Impact | Remediation |
|----|----------|-----|--------|-------------|
| HF1 | Medium | No deferred implementation plan file | CLAUDE.md protocol requires `*-deferred-implementation.md` for remaining items | Create `docs/plans/headroom-integration-deferred-implementation.md` documenting TUI config section + integration tests |

---

## Prioritized Remediation Plan

### Sprint 1: High Priority (Estimated: 1-2 days)

| Task | Files to Modify | Owner |
|------|-----------------|-------|
| A1: Generic async hook config | `internal/agent/http_hooks.go`, `internal/agent/file_watcher.go`, `internal/config/schema.go` | TBD |
| U1: Complete user instructions verification | Manual review or subagent with chunking | TBD |
| HF1: Create headroom deferred implementation plan | `docs/plans/headroom-integration-deferred-implementation.md` | TBD |

### Sprint 2: Medium Priority (Estimated: 2-3 days)

| Task | Files to Modify | Owner |
|------|-----------------|-------|
| A2: Do Not Disturb mode | `internal/config/schema.go`, menubar UI, TUI | TBD |
| A3: Designation audit log | `internal/session/`, new table migration | TBD |
| A4: HTTP hook config in schema | `internal/config/schema.go`, `internal/daemon/` | TBD |
| H1: Compression config template | `config/meept.json5` | TBD |

### Sprint 3: Low Priority (Estimated: 1 day)

| Task | Files to Modify | Owner |
|------|-----------------|-------|
| H2: Delete stale backup | `internal/compress/log_compress.go.bak` | TBD |
| P1: Performance load test | New test file | TBD |
| P2: Fix plan contradiction | Update `docs/plans/2026-06-15-agent-progress-ui.md` | TBD |
| T1: Align topic detection defaults | `internal/session/thread.go` or plan doc | TBD |
| E1-E3: Document plan divergences | Update plan doc | TBD |

---

## Verification Checklist

After remediation:

- [ ] `go build ./...` passes
- [ ] `go test ./...` passes
- [ ] All new tests added for new functionality
- [ ] Configuration templates updated
- [ ] Documentation reflects changes
- [ ] Run `make docs-generate` if schema changed

---

## Tracking

**Linked documents:**
- Source review report: N/A (this session)
- Parent plans: See individual plan files in `docs/superpowers/plans/` and `docs/plans/`

**Related CLAUDE.md sections:**
- Deferred Item Resolution Protocol
- Feature Documentation Requirements
