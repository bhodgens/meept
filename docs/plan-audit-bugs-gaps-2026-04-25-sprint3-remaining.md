# Sprint 3 Remaining: Medium Severity Fixes

**Source:** Original Sprint 3 plan (32 issues)
**Completed:** 7 issues (SEC-9, SEC-10, SEC-11, LLM-12, LLM-13, TOOLS-13, TOOLS-14)
**Remaining:** 25 issues

---

## Remaining Issues (25)

### Core (4 issues)
- CORE-5: `internal/config/presets.go:145-164` - ApplyPreset ignores TopP, FrequencyPenalty, PresencePenalty
- CORE-6: `internal/bus/handler.go:55-61` - SubscriptionHandler may skip unsubscribe
- CORE-7: `internal/registry/registry.go:82-103` - StopAll holds RLock during Stop() - Already fixed, add comment
- CORE-8: `internal/daemon/launchd.go:267` - int(time.Hour) fragile - Already fixed, add comment

### Agent (7 issues)
- AGENT-17: `internal/agent/workspace.go` - Tag parsing breaks for labels with dashes
- AGENT-18: `internal/agent/collaborative.go` - CollaborativePlanner not wired (deferred)
- AGENT-19: `internal/agent/loop.go` - progressInterval field never read
- AGENT-20: `internal/agent/session_tracker.go` - PersistIdleSessions swallows errors
- AGENT-21: `internal/agent/registry.go` - Concrete *llm.Client (deferred)
- AGENT-22: `internal/agent/orchestrator.go` - DONE/FAIL prefixes in logs
- AGENT-23: `internal/agent/review_manager.go` - stepStore.Update error ignored

### Memory (7 issues)
- MEM-11: `internal/memory/consolidation.go:133-144` - runAccessBasedExpiration ignores errors
- MEM-12: `internal/memory/consolidation.go:258-291` - summarizeByDate zero-value strings
- MEM-13: `internal/memory/manager.go:1139` - getCurrentVersion uses context.Background()
- MEM-14: `internal/memory/artifact_manager.go` - GetCacheStats inner lock under RLock (file may not exist)
- MEM-15: `internal/memory/manager.go:1362-1378` - Delete returns nil when 0 rows deleted
- MEM-16: `internal/memory/manager.go:1006-1025` - GetRelatedMemories FTS on UUID
- MEM-17: `internal/memory/consolidation.go:296-307` - MergeRelations groups by date only (deferred)

### CLI (1 issue)
- CLI-8: `cmd/meept/selfimprove.go:107-159` - analyze/generate-fixes/validate dump raw JSON

---

## Implementation Plan

Implement in small batches (4-6 issues) with immediate commits.
