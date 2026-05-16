# Complete Feature Integration Plan

**Date:** 2026-05-16
**Status:** In Progress
**Priority:** Critical
**Objective:** Wire all unwired features and close implementation gaps identified in README

---

## Executive Summary

Three features are marked as "Partial" in README.md:
1. **Skills System** - ~80% complete, needs CLI commands
2. **Security Engine** - ~50% complete, needs harness bug fixes
3. **Collaborative Planning** - 0% integration, needs wiring

This plan addresses all gaps with parallel execution tracks.

---

## Track 1: Skills System - Final Integration (15%)

### Gap Analysis
- Core discovery, registry, executor: **Complete**
- Dispatcher integration: **Complete**
- Tool filtering: **Complete**
- Automatic skill injection: **Complete**
- **Missing:** CLI subcommands (Phase 7)

### Tasks

#### 1.1 CLI Skills Subcommand
**File:** `cmd/meept/skills.go` (create)

```go
// Commands to implement:
// - meept skills list              # List available skills
// - meept skills run <name> <input> # Execute a skill
// - meept skills info <name>       # Show skill details
// - meept skills search <query>    # Search skills by keyword
```

**Integration:**
- Register in `cmd/meept/main.go`
- Wire to RPC skills service

#### 1.2 RPC Endpoint Verification
**File:** `internal/rpc/skills.go` (verify/existing)

Verify these methods are registered and functional:
- `skills.list`
- `skills.execute`
- `skills.info`

---

## Track 2: Security Engine - Harness Bug Fixes (50% → 100%)

### Gap Analysis (from docs/auto-analysis/0012-security-engine-harness-bugs.md)

| Bug | Severity | Status |
|-----|----------|--------|
| #1: SecurityBeforeToolCall only handles `shell` | CRITICAL | Not fixed |
| #2: No Tirith scan logging | HIGH | Not fixed |
| #3: Input sanitizer not invoked | HIGH | Partial (hook exists) |
| #4: Execution semaphore blocks testing | MEDIUM | Test infra |
| #5: Risk level logged for blocked tools | MEDIUM | Not fixed |

### Tasks

#### 2.1 Extend SecurityBeforeToolCall to All Tools
**File:** `internal/agent/security_hooks.go`

Extend `BeforeToolCall()` to handle:
- `shell` - Tirith scan (existing)
- `file_read`, `file_write`, `file_delete`, `list_directory` - path permission check
- `web_fetch`, `network` - network permission check

```go
func (s *SecurityBeforeToolCall) BeforeToolCall(ctx context.Context, toolCall llm.ToolCall) BlockResult {
    switch toolCall.Function.Name {
    case "shell":
        return s.scanShellCommand(ctx, toolCall)
    case "file_read", "file_write", "file_delete", "list_directory":
        return s.checkFilePermission(ctx, toolCall)
    default:
        return BlockResult{}
    }
}
```

#### 2.2 Add Comprehensive Tirith Logging
**File:** `internal/security/orchestrator.go`

Add logging at appropriate levels:
- INFO: All commands scanned (truncated)
- INFO: Commands blocked with reason
- WARN: Tirith unavailable (graceful degradation)
- DEBUG: Scan results for allowed commands

#### 2.3 Fix Input Sanitizer Invocation
**File:** `internal/agent/security_hooks.go`

The hook exists but needs INFO-level logging:
```go
if len(result.ThreatsDetected) > 0 {
    logger.Info("Input sanitization detected threats",
        "threats", result.ThreatsDetected,
        "blocked", blocked)
}
```

#### 2.4 Fix Risk Level Logging
**File:** `internal/agent/executor.go` or security hooks

Log both tool risk level AND block reason:
```go
logger.Info("Tool blocked by security",
    "tool", toolName,
    "reason", blockReason,
    "tool_risk_level", toolRiskLevel,
    "block_category", "path_rule"|"risk_assessment")
```

#### 2.5 Wire Security Orchestrator in Daemon
**File:** `internal/daemon/components.go`

Verify/create:
- `NewOrchestrator()` call with config
- Pass to agent loop factory
- Pass to shell tool via `SetSecurityOrchestrator()`
- Register security hooks with agent loop

---

## Track 3: Collaborative Planning Integration (0% → 100%)

### Gap Analysis
- `CollaborativePlanner` class: **Complete**
- `PlanAndReview()`: **Complete**
- `Approve()/Reject()/Revise()`: **Complete**
- **Missing:** Integration into agent loop
- **Missing:** User-facing approval UI

### Tasks

#### 3.1 Wire CollaborativePlanner in Daemon
**File:** `internal/daemon/components.go`

```go
// Create CollaborativePlanner during component wiring
collaborativePlanner := agent.NewCollaborativePlanner(
    planner,
    llmClient,
    workspaceManager,
    logger,
)
```

#### 3.2 Integrate into Agent Loop
**File:** `internal/agent/loop.go`

Add collaborative planning check in `Run()` or `RunOnce()`:
```go
// Check if this is a programming task needing collaborative review
if c.collaborativePlanner != nil && c.collaborativePlanner.IsProgrammingTask(userMessage) {
    // Check for pending review
    if c.collaborativePlanner.HasPendingReview(conversationID) {
        // Handle approval/rejection/revision response
        classification := c.collaborativePlanner.ClassifyResponse(userMessage)
        switch classification {
        case "approve":
            plan, err := c.collaborativePlanner.Approve(ctx, conversationID)
            // Execute approved plan
        case "reject":
            err := c.collaborativePlanner.Reject(ctx, conversationID, userMessage)
        case "revise":
            review, err := c.collaborativePlanner.Revise(ctx, conversationID, userMessage)
        }
    } else {
        // Create new plan for review
        review, err := c.collaborativePlanner.PlanAndReview(ctx, userMessage, conversationID)
        // Return review to user for approval
        return &Response{Content: review.FormattedSummary, RequiresApproval: true}
    }
}
```

#### 3.3 Add Approval Response Type
**File:** `internal/agent/loop.go` or `pkg/models/`

```go
type Response struct {
    Content           string
    Model             string
    TokensUsed        int
    SkillUsed         string
    RequiresApproval  bool  // NEW: indicates pending plan approval
    TaskID            string // NEW: for tracking
}
```

#### 3.4 RPC Endpoint for Approval
**File:** `internal/rpc/proxy.go` or new handler

```go
p.Handle("task.approve", p.handleTaskApprove)
p.Handle("task.reject", p.handleTaskReject)
p.Handle("task.revise", p.handleTaskRevise)
p.Handle("task.pending", p.handleTaskPending) // Check for pending plans
```

#### 3.5 CLI Support for Approval
**File:** `cmd/meept/chat.go`

When response has `RequiresApproval: true`:
- Display plan summary
- Prompt: `[Approve/Reject/Revise]: `
- Route response to appropriate handler

---

## Success Criteria

| Feature | Before | After |
|---------|--------|-------|
| Skills CLI | Not exists | `meept skills list/run/info/search` |
| Security hooks | Shell only | All security-sensitive tools |
| Security logging | Silent | Comprehensive audit trail |
| Collaborative planning | Not wired | Full approval workflow |

---

## Execution Strategy

**Phase 1 (Subagent Parallel):**
- Subagent 1: Skills CLI (Track 1.1)
- Subagent 2: Security harness bugs (Track 2.1-2.4)
- Subagent 3: Collaborative planning wiring (Track 3.1-3.3)

**Phase 2 (Verification):**
- Subagent 4: Test skills integration
- Subagent 5: Test security engine
- Subagent 6: Test collaborative planning

**Phase 3 (Documentation):**
- Update README.md feature status table
- Update CLAUDE.md with new commands
- Generate/update HTTP API docs if needed
