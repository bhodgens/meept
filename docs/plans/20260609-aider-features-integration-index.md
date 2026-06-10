# Aider Features Integration - Plan Index

**Created:** 2026-06-09
**Status:** Plans Ready for Review

This document indexes all implementation plans created from the aider-ai/aider repository feature analysis.

---

## Plans Overview

| Plan | Priority | Effort | Status |
|------|----------|--------|--------|
| [RepoMap with PageRank](#repomap) | High | 2-3 weeks | Pending |
| [Auto Lint/Test Reflection](#auto-linttest) | Critical | 2-3 weeks | Pending |
| [Multiple Edit Formats](#edit-formats) | Medium | 1-2 weeks | Pending |
| [Analytics System](#analytics) | Medium | 1-2 weeks | Pending |
| [Desktop Notifications](#notifications) | Medium | 3-5 days | Pending |

---

## Detailed Plans

### RepoMap {#repomap}

**File:** `docs/plans/repomap-pagerank-implementation.md`

**Summary:**
Implement a repository mapping system that provides LLMs with structural awareness of the entire codebase using graph-based Personalized PageRank ranking.

**Key Components:**
- Tag extraction via tree-sitter (definitions + references)
- Weighted directed graph construction
- Personalized PageRank for relevance ranking
- Token-budget fitting via binary search
- Three-layer caching (SQLite disk cache + in-memory)

**Benefits for Meept:**
- Agents gain repository-wide awareness
- Better file navigation for multi-agent coordination
- Token-efficient context injection
- Improved performance on large codebases

**Integration Points:**
- `internal/code/ast/` - Tree-sitter parser
- `internal/agent/orchestrator.go` - Context injection
- New: `internal/repomap/` package

---

### Auto Lint/Test {#auto-linttest}

**File:** `docs/plans/auto-lint-test-reflection-implementation.md`

**Summary:**
Implement automated quality-assurance feedback loop with automatic linting, testing, and LLM-driven fix attempts (reflection loop).

**Key Components:**
- Linter registry with language-specific chains
- Tree-sitter syntax validation
- Test runner integration (Go, Python, JS)
- Reflection engine (up to 3 iterations)
- Error formatting with tree context

**Benefits for Meept:**
- **Critical for Debugger agent** - systematic failure analysis
- Reduces human review required
- Catches errors before commit
- Self-correcting code generation

**Integration Points:**
- `internal/agent/orchestrator.go` - Reflection loop
- `internal/agent/debugger.go` - Debug workflow
- `internal/lint/` package (new)
- `internal/metrics/store.go` - Lint/test metrics

---

### Multiple Edit Formats {#edit-formats}

**File:** `docs/plans/20260609-multiple-edit-formats-explanation.md`

**Summary:**
Implement **input format adapters** that parse various LLM output styles (SEARCH/REPLACE, unified diff, whole file) and convert them to Meept's existing internal `editOp` format with `LINE:HASH` anchors.

**Key Insight:** Meept already has a sophisticated patch system (`internal/tools/builtin/file_edit.go`) with:
- `LINE:HASH` anchored patches
- Stale anchor recovery (3-tier: exact → hash → fuzzy)
- Block operations (`replace_block`, `delete_block`)
- Preview/accept workflow

**Adapters to Build:**
1. **SearchReplaceAdapter** - Parse Claude-style SEARCH/REPLACE blocks
2. **UnifiedDiffAdapter** - Parse standard diff format (code models)
3. **WholeFileAdapter** - Parse complete file output (local models)
4. **JSONAdapter** - Native Meept format (pass-through)
5. **ArchitectAdapter** - Coordinate plan-then-execute workflow

**Benefits for Meept:**
- Let each model use its native output format
- Leverages existing `file_edit` infrastructure
- Better model compatibility without rewriting core logic
- Token efficiency via format selection per model

**Integration Points:**
- New: `internal/tools/builtin/adapters/` package
- `internal/llm/client.go` - Route responses through adapters
- `internal/agent/orchestrator.go` - Adapter selection logic

---

### Analytics System {#analytics}

**File:** `docs/plans/analytics-system-implementation.md`

**Summary:**
Extend Meept's existing metrics system with agent performance analytics, inspired by aider's benchmarking capabilities. **Fully local implementation** (no PostHog or external services).

**Metrics to Track:**
- Pass rate, well-formed %, syntax errors
- Lazy responses, context exhaustion
- Task timeouts, user interventions
- Time/cost per task
- Reflection success rate

**Key Components:**
- Response quality analyzer
- Extended metrics database schema
- CLI analytics commands
- Benchmark framework

**Benefits for Meept:**
- Data-driven agent optimization
- Model performance comparison
- Quality trend monitoring
- Cost tracking

**Integration Points:**
- `internal/metrics/collector.go` - Metrics collection
- `internal/metrics/analyzer.go` (new) - Quality analysis
- `cmd/meept/analytics.go` (new) - CLI commands
- `internal/benchmark/` (new) - Benchmark framework

---

### Desktop Notifications {#notifications}

**File:** `docs/plans/menubar-desktop-notifications-implementation.md`

**Summary:**
Implement desktop notifications for the Meept MenuBar app to alert users when LLM responses are ready, tasks complete, or errors occur.

**Key Components:**
- Daemon-side event emitter
- WebSocket real-time notifications
- HTTP polling fallback
- macOS native notifications (NSUserNotificationCenter)
- MenuBar notification center view

**Notification Types:**
- Task completed (success)
- Task failed (error)
- Long-running task (info)
- Confirmation needed (warning)
- Security block (error)

**Benefits for Meept:**
- Improved UX for MenuBar users
- Asynchronous task monitoring
- Better user awareness of background operations

**Integration Points:**
- `internal/daemon/events.go` (new) - Event system
- `internal/comm/http/notification_handlers.go` (new) - HTTP endpoints
- `MeeptMenuBar/Services/NotificationManager.swift` (new)
- `MeeptMenuBar/Views/NotificationCenterMenuView.swift` (new)

---

## Implementation Priority

### Phase 1: Foundation (Weeks 1-4)
1. **Auto Lint/Test** - Critical for debugger agent reliability
2. **Analytics System** - Needed to measure impact of other features

### Phase 2: Core Capabilities (Weeks 5-8)
3. **RepoMap** - Major capability enhancement
4. **Multiple Edit Formats** - Model compatibility improvements

### Phase 3: UX Enhancements (Weeks 9-10)
5. **Desktop Notifications** - MenuBar app polish

---

## Cross-Plan Dependencies

```
Auto Lint/Test ──────────────┬──────────────► Analytics
                              │
RepoMap ──────────────────────┤
                              │
Edit Formats ────────────────┴──────────────► (no dependencies)

Desktop Notifications ───────────────────────► (standalone)
```

**Shared Infrastructure:**
- Auto Lint/Test and Analytics both extend metrics schema
- RepoMap uses existing tree-sitter infrastructure
- Edit Formats requires LLM response parser updates

---

## Estimated Total Effort

| Phase | Duration | Features |
|-------|----------|----------|
| Phase 1 | 4 weeks | Auto Lint/Test, Analytics |
| Phase 2 | 4 weeks | RepoMap, Edit Formats |
| Phase 3 | 1 week | Desktop Notifications |
| **Total** | **9 weeks** | All 5 features |

---

## Success Metrics

After implementing all features:

1. **Code Quality**: >70% of lint/test failures auto-fixed via reflection
2. **Agent Awareness**: Agents use RepoMap for >50% of file operations
3. **Model Flexibility**: Support for 5+ edit formats across different models
4. **Observability**: All agent actions tracked with quality metrics
5. **User Satisfaction**: Desktop notifications reduce context-switching overhead

---

## Risk Assessment

| Risk | Mitigation |
|------|------------|
| Tree-sitter parsing performance | Aggressive caching, incremental updates |
| Reflection loop infinite cycles | Max iteration limit, failure detection |
| Token budget exceeded in RepoMap | Binary search with strict bounds |
| Analytics overhead >5% | Async collection, batched writes |
| Notification spam | Configurable filters, rate limiting |

---

## Next Steps

1. **Review all plans** with team
2. **Prioritize** based on current pain points
3. **Create tasks** for Phase 1 implementation
4. **Set up tracking** in project management system
5. **Begin implementation** with Auto Lint/Test foundation

---

## Related Documentation

- Original Feature Analysis: (this conversation's output)
- Aider Repository: https://github.com/aider-ai/aider
- Meept Architecture: `docs/concepts/architecture.md`
- Current Metrics: `docs/reference/metrics.md`
