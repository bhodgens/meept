# Sprint 3: Medium Severity Fixes

**Source:** `docs/audit-bugs-gaps-2026-04-25-remediation.md`

**Goal:** Implement all Medium severity fixes (32 issues).

---

## Issues to Fix (32 total)

### Core (4 issues)
- CORE-5: `internal/config/presets.go:145-164` - ApplyPreset ignores TopP, FrequencyPenalty, PresencePenalty
- CORE-6: `internal/bus/handler.go:55-61` - SubscriptionHandler may skip unsubscribe if bus closes first
- CORE-7: `internal/registry/registry.go:82-103` - StopAll holds RLock while calling component Stop()
- CORE-8: `internal/daemon/launchd.go:267` - int(time.Hour) multiplication fragile

### Agent (7 issues)
- AGENT-17: `internal/agent/workspace.go` - Tag parsing breaks for labels containing dashes
- AGENT-18: `internal/agent/collaborative.go` - CollaborativePlanner not wired into any production path
- AGENT-19: `internal/agent/loop.go` - progressInterval field never read
- AGENT-20: `internal/agent/session_tracker.go` - PersistIdleSessions silently swallows errors
- AGENT-21: `internal/agent/registry.go` - AgentRegistry holds concrete *llm.Client, not interface
- AGENT-22: `internal/agent/orchestrator.go` - Debug log prefixes "DONE"/"FAIL" in production messages
- AGENT-23: `internal/agent/review_manager.go` - stepStore.Update error ignored after validation failure

### Security (4 issues)
- SEC-9: `internal/security/taint/patterns.go:105-107` - ; and | detection has massive false positive rate
- SEC-10: `internal/security/engine.go:473-478` - Additional resource leak path in checkPath
- SEC-11: `internal/security/taint/patterns.go:253-286` - SanitizeShellCommand gives false sense of security

### LLM (5 issues)
- LLM-12: `internal/llm/context_firewall.go:267-272` - Stage 2 "summarize" just truncates - no actual LLM summarization
- LLM-13: `internal/llm/provider_manager.go:551-583` - Health checks consume real budget with live API requests

### Memory (7 issues)
- MEM-11: `internal/memory/consolidation.go:133-144` - runAccessBasedExpiration ignores Store/Delete errors
- MEM-12: `internal/memory/consolidation.go:258-291` - summarizeByDate leaves zero-value strings in IDs slice
- MEM-13: `internal/memory/manager.go:1139` - getCurrentVersion uses context.Background()
- MEM-14: `internal/memory/artifact_manager.go:121-133` - GetCacheStats acquires inner lock under outer RLock
- MEM-15: `internal/memory/manager.go:1362-1378` - Delete returns nil even when 0 rows deleted
- MEM-16: `internal/memory/manager.go:1006-1025` - GetRelatedMemories SQLite path uses FTS on UUID (non-functional)
- MEM-17: `internal/memory/consolidation.go:296-307` - MergeRelated only groups by date, not semantic similarity

### Tools (2 issues)
- TOOLS-13: `internal/comm/http/config_service.go:43-57` - expandPath is dead code and fragile
- TOOLS-14: `internal/calendar/auth.go:254` - Uses fmt.Printf instead of slog

### CLI (3 issues)
- CLI-8: `cmd/meept/selfimprove.go:107-159` - analyze/generate-fixes/validate dump raw JSON
- CLI-9: `cmd/meept/status.go:52` - PID parsing lacks strings.TrimSpace

---

## Implementation Order

1. Quick fixes first (single line changes)
2. Group by package for efficiency
3. Verify each fix compiles before moving to next

## Verification Criteria

Each fix must:
1. Compile without errors
2. Pass existing tests in the package
3. Include inline comment referencing issue ID (e.g., `// CORE-5 FIX:`)
