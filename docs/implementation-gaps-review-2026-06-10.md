# Implementation Gaps Review — 2026-06-10

Comprehensive verification of all plan files created/modified since April 30, 2026, against actual codebase state. Each gap is classified by severity and includes specific action items.

---

## Summary

- **59 plan files reviewed** across 6 domains
- **45 plans (76%)** fully implemented at 100%
- **9 plans (15%)** near-complete (80-99%)
- **3 plans (5%)** partially complete (50-79%)
- **2 plans (4%)** research/index only (N/A)
- **Overall weighted completion: ~94%**

---

## High Priority Gaps

### 1. Collaborative Planning Integration — 58%

**Plan:** `docs/plans/2026-05-16-complete-feature-integration.md`
**Status:** Track 3 (Collaborative Planning) was never wired as described

The plan called for wiring `CollaborativePlanner` into the agent loop with approval/rejection RPC endpoints and CLI prompts. Instead, a separate `internal/plan/` package was built with its own HTTP endpoints (`/api/v1/plans/{id}/approve|reject|confirm|revise`) and CLI commands (`meept plans approve/reject`).

| Planned Item | Actual State | Action |
|-------------|--------------|--------|
| `CollaborativePlanner` wired in daemon | Not wired as standalone integration | **Update plan** to document architectural pivot to `internal/plan/` |
| `IsProgrammingTask`, `HasPendingReview`, `ClassifyResponse` methods | Not found | Superseded by plan package |
| `RequiresApproval` field on agent Response | Not found | Superseded by plan lifecycle |
| RPC: `task.approve`, `task.reject`, `task.pending`, `task.revise` | Not implemented | HTTP equivalents exist in plan package |
| CLI `[Approve/Reject/Revise]:` prompt | Not in chat | Replaced by `meept plans` commands |

**Action:** Update the plan document to mark Track 3 as "superseded by `internal/plan/` package" with a note pointing to the new implementation.

---

### 2. Lint Remediation — 50%

**Plan:** `docs/plans/2026-05-11-remaining-lint-remediation.md`
**Status:** Reduced from 3,031 warnings to ~827 as of 2026-05-13

| Phase | Description | Status | Remaining |
|-------|-------------|--------|-----------|
| Phase 1 | Config changes (disable `contextcheck`) | Done | — |
| Phase 2 | Mechanical fixes (staticcheck, introute, errcheck, revive, nilerr, modernize, rowserrcheck, prealloc) | Partially done | Specific per-file fixes remain |
| Phase 3 | Gosec per-site suppressions (SQL parameterization, file permissions, subprocess validation) | Not verified | SQL injection fixes in `ftstore.go`, `store_sqlite.go` need checking |
| Phase 4 | Goconst production constants (agent ID strings, log level strings, preset name extraction) | Not verified | String literal extraction incomplete |

**Action:** Run `golangci-lint run ./...` to get current warning count. Complete Phases 3-4.

---

### 3. Empty LLM Response Handling — 67%

**Plan:** `docs/plans/empty-llm-response-handling.md`
**Status:** Phase 3 explicitly marked TODO

| Phase | Description | Status |
|-------|-------------|--------|
| Phase 1 | Classifier alias with fallback models | Done |
| Phase 2 | Agent loop empty response handling with model rotation | Done |
| Phase 3 | User guidance messages for common failures | **Not implemented** |

**Gap:** No `handleClassificationError` function exists. Users receive no actionable guidance when classification fails.

**Action:** Implement user-facing error messages for: empty classifier response, model unavailable, budget exhausted during classification.

---

## Medium Priority Gaps

### 4. OpenRouter Error Handling — 80%

**Plan:** `docs/plans/openrouter-error-handling.md`

| Item | Status | Details |
|------|--------|---------|
| Rich error types + parsing | Done | `RateLimitError`, `APIError`, `ProviderErrorDetail` |
| Jitter backoff | Done | `BackoffWithJitter()` |
| Consistent RateLimitError across providers | Done | Anthropic + OpenRouter both return `RateLimitError` |
| `UserMessage()` on all error types | Done | TUI and CLI use it |
| TUI retry progress indicators | **Not done** | No retry status propagated through RPC |
| Extended `ErrorRecord` in metrics | **Not done** | `ErrorRecord` lacks `LimitType`, `RetryAttempts`, `FinalOutcome` fields |

**Action:** Add retry progress events to bus. Extend `ErrorRecord` in `metrics/store.go`.

---

### 5. Standardization Option C — 78%

**Plan:** `docs/plans/2026-06-03-standardization-option-c.md`

| Task | Description | Status | Decision Needed |
|------|-------------|--------|-----------------|
| 1.4 | Shell tokenization | Kept current impl | Accepted |
| 1.8 | Env var expansion | Kept current impl | Accepted |
| 5.3 | Flutter `retrofit` for typed HTTP client | Incompatible with Dart 3.12 — manual client used | Accepted, document it |
| 5.4 | `reconnecting_web_socket` | Not adopted; `rxdart` used instead | Needs decision |
| 7.1 | `magefiles/gui.go` for menubar/flutter builds | Not created | **Create or mark N/A** |
| 7.2 | `go:generate` for OpenAPI | Not adopted; `cmd/gendoc` still used | **Adopt or mark N/A** |
| 7.3 | `mkdocs-awesome-pages` | Not adopted; manual nav maintained | **Adopt or mark N/A** |
| 7.4 | Replace `cmd/gendoc` with `gomarkdoc` | Not done; gendoc still exists (274 lines) | **Adopt or mark N/A** |
| 8.1/8.2 | `gosec` CI integration | Annotations in 84 files but not in CI pipeline | Wire into `mage test` |

**Action:** For each "Decision Needed" item, choose: implement, mark as deferred, or mark as rejected. Update plan accordingly.

---

### 6. Full-Stack Bug Fixes — Plan Stale

**Plan:** `docs/superpowers/plans/2026-06-09-full-stack-bug-fixes.md`
**Status:** All 29 bugs fixed via 8 sprints, but **all 142 step checkboxes remain `[ ]`**

The plan appears completely unimplemented to anyone reading it, but git history confirms all fixes were committed.

**Action:** Update all checkboxes from `[ ]` to `[x]` in the plan document.

---

## Low Priority Gaps

### 7. Analytics System — 85%

**Plan:** `docs/plans/20260609-analytics-system-implementation.md`

| Item | Status |
|------|--------|
| `agent_task_outcomes` and `agent_errors` tables | Done |
| Response quality analyzer | Done |
| CLI: `analytics summary/errors/models/export` | Done |
| Benchmark framework | Done |
| Agent loop integration | Done (commit `27129f5`) |
| `model_performance` aggregation table | Not confirmed |
| `analytics` config section in `meept.json5` | Not confirmed |
| Success criteria checkboxes | All unchecked despite implementation |

**Action:** Verify config section and aggregation table. Check off success criteria.

---

### 8. Menubar Desktop Notifications — 90%

**Plan:** `docs/plans/20260609-menubar-desktop-notifications-implementation.md`

| Item | Status |
|------|--------|
| Daemon `EventEmitter` with `Subscribe`/`Unsubscribe`/`Publish` | Done |
| HTTP notification handlers (WebSocket + polling) | Done |
| Swift `NotificationManager` with UNUserNotificationCenter | Done |
| Swift `WebSocketManager` with reconnect logic | Done |
| `NotificationCenterMenuView` UI | Done |
| Agent loop hooks for long-running task notifications | Not confirmed |
| Notification config section in `meept.json5` | Not confirmed |
| Success criteria checkboxes | All unchecked |

**Action:** Verify agent loop integration and config. Check off success criteria.

---

### 9. RepoMap & PageRank — 90%

**Plan:** `docs/plans/20260609-repomap-pagerank-implementation.md`

| Item | Status |
|------|--------|
| Tag extraction (`internal/repomap/extractor.go`) | Done |
| Graph construction (`graph.go`) | Done |
| PageRank (`pagerank.go`) | Done |
| Token budget fitting (`fitting.go`) | Done |
| Context rendering (`renderer.go`) | Done |
| Caching (`cache.go`) | Done |
| Agent loop/orchestrator integration | Not confirmed |
| Config schema additions | Not confirmed |

**Action:** Verify agent loop integration. Confirm config schema.

---

### 10. Auto-Lint & Test Reflection — 90%

**Plan:** `docs/plans/20260609-auto-lint-test-reflection-implementation.md`

| Item | Status |
|------|--------|
| Linter registry (`internal/lint/registry.go`) | Done |
| Tree-sitter lint (`treelint.go`) | Done |
| Language linters (Go, Python, JS) | Done |
| Test runner (`testrunner.go`) | Done |
| Reflection engine (`internal/agent/reflection.go`) | Done |
| Agent loop integration (component 6) | Not confirmed |
| Tree context with error markers (component 7) | Not confirmed |
| Metrics integration | Not confirmed |

**Action:** Verify orchestrator integration and tree context features.

---

### 11. HTTP API Plans — Minor Gaps (92-93%)

**Plans:** `2026-05-07-http-api-for-web-clients.md`, `2026-05-16-http-api-complete-implementation.md`

| Gap | Details |
|-----|---------|
| TypeScript Client SDK | Not built; superseded by Flutter menubar app. Mark as "out of scope" in both plans. |
| Integration tests at `tests/http_api_test.go` | Tests exist at `internal/comm/http/unified_http_test.go` instead. Acceptable alternative location. |
| BranchService as standalone file | Merged into SessionService. Architectural simplification. |

**Action:** Update plans to mark TypeScript SDK as "superseded by Flutter GUI."

---

### 12. Option C Dual-Agent Conversation — 83%

**Plan:** `docs/plans/2026-05-28-option-c-dual-agent-conversation.md`

| Gap | Details |
|-----|---------|
| Dispatcher heuristic for `IntentPair` | `IntentPair` exists in `intent.go` and is checked in `dispatcher.go:1851`, but no explicit heuristic documents which user inputs should trigger pairing vs. other intent types |

**Action:** Document the `IntentPair` classification logic in `docs/concepts/multi-agent.md`.

---

### 13. Bubbletea v2 Migration — 93%

**Plan:** `docs/plans/archive/plan-bubbletea-v2-migration.md`

| Gap | Details |
|-----|---------|
| Textarea widget API | `textarea.New()` in `internal/tui/models/chat.go:243` still uses direct field assignment (`ta.Placeholder = "..."`) instead of functional options (`textarea.WithPlaceholder(...)`) |

**Action:** Either migrate to functional options or document why field assignment is retained.

---

### 14. Unused Code Remediation — 86%

**Plan:** `docs/plans/2026-05-10-unused-code-remediation.md`

| Gap | Details |
|-----|---------|
| Phase 7 sub-items | `isClickInViewportArea`, `drawX`/`drawExclamation` wiring not verified |

**Action:** Verify these specific wiring items or mark as N/A.

---

## Research/Non-Actionable

### 15. Native Scrollback — 0% (Research Only)

**Plan:** `docs/plans/meept-lite-native-scrollback.md`

This is a research document that concluded the effort (6-8 hours) wasn't worth the benefit, recommending to keep the current termbox-based implementation. **No action needed.**

---

### 16. Multiple Edit Formats — N/A (Explanation Only)

**Plan:** `docs/plans/20260609-multiple-edit-formats-explanation.md`

This is a conceptual explanation document. The referenced implementation plan (`20260609-multiple-edit-formats-implementation.md`) was never created. No `internal/tools/builtin/adapters/` package exists.

**Action:** If edit format adapters are still desired, create the implementation plan. Otherwise, document as "deferred."

---

## Action Items Summary

| # | Priority | Action | Effort |
|---|----------|--------|--------|
| 1 | High | Update `complete-feature-integration.md` to document plan package pivot | 15 min |
| 2 | High | Run `golangci-lint run ./...`, complete lint Phases 3-4 | 2-4 hrs |
| 3 | High | Implement user guidance messages for LLM failures (Phase 3 of empty-llm-response) | 1-2 hrs |
| 4 | Medium | Add retry progress events to TUI; extend ErrorRecord fields | 1-2 hrs |
| 5 | Medium | Decide on standardization items (magefiles/gui.go, gomarkdoc, mkdocs-awesome-pages, gosec CI) | 30 min decisions |
| 6 | Medium | Update all 142 checkboxes in full-stack-bug-fixes plan | 20 min |
| 7 | Low | Verify and check off success criteria in analytics, notifications, repomap, lint plans | 1 hr |
| 8 | Low | Mark TypeScript SDK as "superseded" in HTTP API plans | 10 min |
| 9 | Low | Document IntentPair classification logic | 15 min |
| 10 | Low | Verify/migrate textarea widget API in bubbletea v2 | 30 min |
