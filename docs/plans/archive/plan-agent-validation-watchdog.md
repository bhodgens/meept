# Agent Validation, Context Management & Watchdog Enhancement Plan

**Date**: 2026-04-13
**Status**: Implementation In Progress

## Context

This document describes improvements to the Meept agent system across three key areas:

1. **Task Completion Validation**: Agents currently mark work complete when iterations finish or LLM stops using tools. There's no systematic validation that ALL assigned work (per the task/step) was actually completed before reporting done.

2. **Context Budget Management**: The system has context truncation but lacks: (a) configurable "hard" limits that trigger context drop+reattempt, (b) a "soft" wrap-up suggestion threshold, and (c) hallucination detection integrated into error handling.

3. **Agent Monitoring & Escalation**: No watchdog mechanism exists to detect stuck agents or abort connections after timeouts. Failed tasks currently bubble up errors but don't trigger automatic re-planning or structured recovery.

4. **Structured Final Reports**: The final report format needs enhancement to include categorized recommendations from all sub-agents, with weighted scores. Progress should be sidebar-only until user intervention is required.

---

## Implementation Status

### Phase 1: Task Completion Validator - CONFIG COMPLETE

**Goal**: Add a validation step before agents report completion to verify all assigned work is done.

**Configuration Added** (`internal/config/schema.go`):
```toml
[agent.validation]
enabled = true
require_validation = ["code", "refactor", "debug", "git", "fix", "commit"]
skip_validation = ["chat", "report", "recall", "search", "analyze", "platform"]
skip_validation_agents = ["chat", "analyst"]
max_validation_loops = 3
```

**ValidationPolicy Added** (`internal/agent/review.go`):
- `ValidationPolicy` type with `Enabled`, `RequireValidation`, `SkipValidation`, `MaxValidationLoops`, `SkipValidationAgents`
- `NeedsValidation(step)` method to determine if validation is required
- `ExceedsMaxValidationLoops(validationLoops)` method

**Files Modified**:
- `internal/config/schema.go` - Added `ValidationConfig` type and defaults
- `internal/agent/review.go` - Added `ValidationPolicy` type and methods
- `internal/task/step.go` - Added `Recommendations []CategorizedRecommendation` field

**Files Still Needed**:
- `internal/agent/review_manager.go` - Add `ValidateCompletion()` method (TODO)
- `internal/agent/tactical.go` - Integrate validation into `OnJobCompleted()` (TODO)

---

### Phase 2: Context Budget Thresholds & Hallucination Detection - CONFIG + STRUCTS COMPLETE

**Goal**: Implement configurable soft/hard context limits and hallucination detection.

**Configuration Added** (`internal/config/schema.go`):
```toml
[llm.context_firewall]
wrap_up_threshold = 0.50        # "soft" limit - inject wrap-up suggestion
hard_limit = 0.80               # "hard" limit - drop context and reattempt
drop_context_on_hard_limit = true
```

**Files Created**:
- `internal/agent/hallucination.go` - Complete implementation of `HallucinationDetector`:
  - `HallucinationConfig` with sensitivity settings
  - Detection for: confident claims, fabricated references, contradictions, impossible responses
  - `Analyze(output, conversation)` method returning `HallucinationIndicators`
  - `RecordHistory(content)` for contradiction tracking
  - `RegisterKnownSymbol(name, isFile)` for fact-checking

**Files Still Needed**:
- `internal/llm/context_firewall.go` - Add threshold logic to `processMessages()` (TODO)
- `internal/agent/loop.go` - Integrate hallucination detector into detection config (TODO)

---

### Phase 3: Agent Watchdog - IMPLEMENTATION COMPLETE

**Goal**: Detect stuck agents and abort after configurable timeout.

**Configuration Added** (`internal/config/schema.go`):
```toml
[agent.watchdog]
enabled = true
timeout_minutes = 10
heartbeat_interval_sec = 30
max_iterations = 50
stuck_iteration_count = 5
```

**Files Created**:
- `internal/agent/watchdog.go` - Complete implementation:
  - `Watchdog` type with worker state tracking
  - `WorkerState` struct tracking: `StartTime`, `LastHeartbeat`, `Iteration`, `Stage`, `IsStuck`
  - `RegisterWorker()`, `UpdateHeartbeat()`, `UnregisterWorker()` methods
  - Background monitoring goroutine with configurable checks
  - Alert types: `AlertTimeout`, `AlertMaxIter`, `AlertStuck`, `AlertHeartbeat`
  - `CaptureReport()` for partial work state on abort

**Files Still Needed**:
- `internal/agent/loop.go` - Integrate watchdog registration/heartbeat (TODO)

---

### Phase 4: Escalation & Re-planning - IMPLEMENTATION COMPLETE

**Goal**: Bubble up failures to dispatcher for automatic re-planning into smaller tasks.

**Files Created**:
- `internal/agent/escalation.go` - Complete implementation:
  - `EscalationManager` type with escalation chain tracking
  - `FailureContext` struct capturing failure details
  - `Escalate()` method for triggering re-planning
  - `EscalateForValidation()` for validation failures
  - `triggerReplan()` calling strategic planner
  - `EscalationEvent` for bus publishing
  - `notifyHumanIntervention()` for max escalation levels

**Files Still Needed**:
- `internal/agent/tactical.go` - Integrate escalation into `OnJobFailed()` (TODO)
- `internal/agent/strategic.go` - Add `ReplanFailedTask()` method (TODO)
- `pkg/models/types.go` - Consider adding `EscalationEvent` type if bus integration needed (TODO)

---

### Phase 5: Structured Final Reports - PARTIAL COMPLETE

**Goal**: Aggregate categorized recommendations from all sub-agents, display progress in sidebar only.

**Files Created/Modified**:
- `internal/agent/report.go` - Added types:
  - `CategorizedRecommendation` struct with Category, Priority, Description, AgentID, Confidence
  - `AggregatedTaskReport` struct with Summary, StepsCompleted, Recommendations, ExecutionTime
- `internal/task/step.go` - Added `Recommendations []CategorizedRecommendation` field

**Files Still Needed**:
- `internal/agent/report.go` - Add `TaskReportAggregator` implementation (TODO)
- `internal/agent/handler.go` - Modify `handleTaskCompleted()` to aggregate recommendations (TODO)
- `internal/agent/review_manager.go` - Add `ExtractRecommendations()` during review (TODO)

---

### Phase 6: Preserve Instructions During Pruning - PARTIAL COMPLETE

**Goal**: Ensure validation/escalation instructions aren't lost during context truncation.

**Files Created**:
- `internal/agent/conversation.go` - Added:
  - `anchorMessages map[string]bool` field to Conversation struct
  - `AddAnchorMessage(role, content)` method (partial)

**Files Still Needed**:
- `internal/agent/conversation.go` - Complete anchor implementation:
  - Modify `TruncateByTokens()` to skip anchor messages (TODO)
  - Modify `TruncateByImportance()` to treat anchors as `ImportanceCritical` (TODO)
  - Modify `GetWindowedMessages()` to always include anchors (TODO)
- `internal/agent/loop.go` - Add validation instructions as anchors at step start (TODO)

---

## Configuration Summary

All configuration has been added to `internal/config/schema.go`:

### Validation Config
```go
type ValidationConfig struct {
    Enabled              bool
    RequireValidation    []string
    SkipValidation       []string
    SkipValidationAgents []string
    MaxValidationLoops   int
}

// Default:
Validation: ValidationConfig{
    Enabled:              true,
    RequireValidation:    []string{"code", "refactor", "debug", "git", "fix", "commit"},
    SkipValidation:       []string{"chat", "report", "recall", "search", "analyze", "platform"},
    SkipValidationAgents: []string{"chat", "analyst"},
    MaxValidationLoops:   3,
}
```

### Context Firewall Thresholds
```go
type LLMContextFirewallConfig struct {
    WrapUpThreshold        float64 // default 0.50
    HardLimit              float64 // default 0.80
    DropContextOnHardLimit bool    // default true
}
```

### Watchdog Config
```go
type WatchdogConfig struct {
    Enabled              bool
    TimeoutMinutes       int     // default 10
    HeartbeatIntervalSec int     // default 30
    MaxIterations        int     // default 50
    StuckIterationCount  int     // default 5
}

// Default:
Watchdog: WatchdogConfig{
    Enabled:              true,
    TimeoutMinutes:       10,
    HeartbeatIntervalSec: 30,
    MaxIterations:        50,
    StuckIterationCount:  5,
}
```

---

## New Types Summary

### internal/agent/hallucination.go
| Type | Purpose |
|------|---------|
| `HallucinationConfig` | Configuration for hallucination detection sensitivity |
| `HallucinationDetector` | Detects hallucination patterns in LLM output |
| `HallucinationIndicators` | Results of hallucination analysis |

### internal/agent/watchdog.go
| Type | Purpose |
|------|---------|
| `WatchdogConfig` | Configuration for agent monitoring |
| `WorkerStage` | Current stage: thinking, executing, validating, reviewing |
| `WorkerState` | State tracking for monitored workers |
| `Watchdog` | Main watchdog monitor |
| `WatchdogAlertType` | Alert types: timeout, max_iterations, stuck, heartbeat |
| `WatchdogAlert` | Alert details |
| `ReportCapture` | Partial work snapshot on abort |

### internal/agent/escalation.go
| Type | Purpose |
|------|---------|
| `EscalationConfig` | Configuration for escalation handling |
| `FailureContext` | Captures failure context for re-planning |
| `EscalationManager` | Handles task escalation and re-planning |
| `EscalationEvent` | Event published to bus on escalation |

### internal/agent/report.go (Added)
| Type | Purpose |
|------|---------|
| `CategorizedRecommendation` | Structured recommendation with category/priority |
| `AggregatedTaskReport` | Final task report with recommendations |

### internal/task/step.go (Added)
| Type | Purpose |
|------|---------|
| `CategorizedRecommendation` | Local copy for step storage |

---

## Integration TODOs

### High Priority (Required for Core Functionality)

1. **Validation Integration**
   - [ ] Add `ValidateCompletion(ctx, step, task)` to `ReviewManager`
   - [ ] Wire into `TacticalScheduler.OnJobCompleted()` before marking complete
   - [ ] Add validation retry loop with max loops check
   - [ ] On validation failure: loop back or escalate

2. **Context Firewall Thresholds**
   - [ ] Add `WrapUpThreshold`, `HardLimit` fields to `ContextFirewall`
   - [ ] Modify `processMessages()` to check thresholds
   - [ ] At wrap-up: inject "please wrap up" instruction
   - [ ] At hard limit: clear context, keep system + last 2 messages

3. **Watchdog Integration**
   - [ ] Initialize watchdog in `AgentLoop` setup
   - [ ] Call `RegisterWorker()` at start of `RunOnce()`
   - [ ] Call `UpdateHeartbeat()` each iteration
   - [ ] Check abort channel in iteration loop
   - [ ] Call `UnregisterWorker()` on completion

4. **Escalation Integration**
   - [ ] Initialize escalation manager
   - [ ] Wire into `TacticalScheduler.OnJobFailed()`
   - [ ] Check if failure is retryable vs. escalate-worthy
   - [ ] Add `ReplanFailedTask()` to `StrategicPlanner`

### Medium Priority (Quality of Life)

5. **Report Aggregation**
   - [ ] Create `TaskReportAggregator` type
   - [ ] Add `ExtractRecommendations()` to review process
   - [ ] Modify `handleTaskCompleted()` to call aggregator
   - [ ] Format recommendations for display

6. **Anchor Messages**
   - [ ] Modify `TruncateByTokens()` to skip anchors
   - [ ] Modify `TruncateByImportance()` to treat anchors as critical
   - [ ] Modify `GetWindowedMessages()` to always include anchors
   - [ ] Add validation instructions as anchors in `RunOnce()`

---

## Testing TODOs

### Unit Tests
- [ ] `review_manager_test.go`: Test `ValidateCompletion()` with mock steps
- [ ] `hallucination_test.go`: Test pattern detection accuracy
- [ ] `watchdog_test.go`: Test timeout triggering and abort
- [ ] `escalation_test.go`: Test escalation chain and re-planning
- [ ] `report_test.go`: Test recommendation extraction and aggregation

### Integration Tests
- [ ] End-to-end task with validation failure → retry loop
- [ ] Context budget exhaustion → wrap-up and reattempt
- [ ] Agent stuck simulation → watchdog abort
- [ ] Failed task escalation → re-plan into smaller tasks

### Manual Testing
```bash
# Build and run daemon
make go-daemon

# Test validation via TUI
agent-tui ./bin/meept chat

# Send task with incomplete work:
"Fix the bug in internal/agent/loop.go line 500"

# Verify: agent validates completion before reporting done

# Test watchdog (simulate stuck agent):
# - Add artificial delay in tool execution
# - Verify timeout + abort
```

---

## Acceptance Criteria

When all TODOs are complete, the following should be true:

- [ ] Agents validate 100% of assigned work before marking complete
- [ ] Context wrap-up at 50%, hard reset at 80% (configurable)
- [ ] Hallucination detection triggers recovery (2+ indicators)
- [ ] Watchdog aborts after 10 minutes (configurable)
- [ ] Failed tasks escalate to re-planning (max 3 levels)
- [ ] Final reports include categorized recommendations
- [ ] Progress updates are sidebar-only until completion

---

## Open Questions

The following questions were identified during planning:

1. **Validation scope**: Should the completion validator check (a) only the step description ("fix the bug"), or (b) also cross-reference with the original task intent from the dispatcher? Option (b) is more thorough but adds complexity.

2. **Hallucination detection sensitivity**: The detector could produce false positives. Should this default to "disabled" with explicit enable, or "enabled with low sensitivity" to avoid blocking legitimate work?
   - **Decision**: Default to "low" sensitivity, enabled by default

3. **Watchdog timeout default**: I proposed 10 minutes. For complex coding tasks this might be short. Would you prefer a single global timeout, or per-agent-type timeouts (e.g., coder=15min, chat=3min, debugger=10min)?
   - **Decision**: Single global timeout (simpler), can be made per-agent in future

4. **Escalation chain**: I proposed max 3 re-planning levels before user notification. Is this appropriate, or should it be configurable per-task priority?
   - **Decision**: Configurable via `MaxEscalationLevels` (default: 3)

5. **Recommendations format**: Should recommendations be:
   - (a) Simple text notes attached to each step
   - (b) Structured JSON with action items, priority, and optional code snippets
   - (c) Separate "follow-up tasks" automatically created in the queue
   - **Decision**: (b) Structured JSON with optional follow-up task creation

6. **Sidebar-only progress**: The daemon currently publishes progress to the message bus. For true "sidebar only" behavior, should progress events have a `silent` flag that UI can respect, or should we suppress chat notifications at the `ChatHandler` level?
   - **Decision**: Add `Silent` flag to progress events, let UI respect it

---

## File Reference

| File | Change Type | Status |
|------|-------------|--------|
| `internal/config/schema.go` | Modify | DONE - All config added |
| `internal/agent/review.go` | Modify | DONE - ValidationPolicy added |
| `internal/agent/hallucination.go` | NEW | DONE |
| `internal/agent/watchdog.go` | NEW | DONE |
| `internal/agent/escalation.go` | NEW | DONE |
| `internal/agent/report.go` | Modify | PARTIAL - Types added, aggregator needed |
| `internal/agent/conversation.go` | Modify | PARTIAL - Anchor field added |
| `internal/task/step.go` | Modify | DONE - Recommendations field added |
| `internal/agent/review_manager.go` | Modify | TODO - ValidateCompletion() |
| `internal/agent/tactical.go` | Modify | TODO - Wire validation/escalation |
| `internal/agent/loop.go` | Modify | TODO - Wire watchdog/hallucination |
| `internal/agent/strategic.go` | Modify | TODO - ReplanFailedTask() |
| `internal/llm/context_firewall.go` | Modify | TODO - Threshold logic |
