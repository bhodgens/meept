# Reviewer Agents Implementation Plan

## Overview

Automatic review system that validates agent outputs for correctness and completeness, returning corrections to the orchestrator or executing agent for fixes.

## Current State Analysis

### Existing Infrastructure
```
Step States: pending → ready → scheduled → running → completed/failed/skipped
Task States:  pending → planning → executing → testing → completed/failed/cancelled

Agent Roles: dispatcher, executor (coder, debugger, planner, analyst, committer, scheduler)
Reviewer Role: Already defined in spec.go but not implemented

Existing Review Pattern:
- CollaborativePlanner (collaborative.go) has plan review for users
- LLM analysis pass for plan quality
- User approve/reject/revise workflow
```

### Key Insight
**The "testing" task state already exists but is unused!** This suggests the original design intended a review/verification phase.

---

## Recommended Approach: Review as Automatic Post-Step Phase

### Why This Approach

| Criterion | Review as State | Review as Separate Step | Review as Agent Role | **Hybrid Phase** ✓ |
|-----------|----------------|------------------------|---------------------|-------------------|
| Clean state machine | ✅ | ❌ (doubles steps) | ✅ | ✅ |
| Selective review | ⚠️ (all or nothing) | ✅ | ✅ | ✅ |
| Revision cycle | ⚠️ (complex) | ✅ | ✅ | ✅ |
| TUI visibility | ✅ | ⚠️ (clutter) | ✅ | ✅ |
| Minimal changes | ⚠️ | ❌ | ⚠️ | ✅ |
| Flexible reviewer types | ❌ | ✅ | ✅ | ✅ |

---

## Architecture Design

### 1. Step State Extension

```go
// Add to task/step.go
const (
    StepPending    StepState = "pending"
    StepReady      StepState = "ready"
    StepScheduled  StepState = "scheduled"
    StepRunning    StepState = "running"
    StepReviewing  StepState = "reviewing"      // NEW
    StepApproved   StepState = "approved"       // NEW (review passed)
    StepRejected   StepState = "rejected"       // NEW (needs revision)
    StepCompleted  StepState = "completed"
    StepFailed     StepState = "failed"
    StepSkipped    StepState = "skipped"
)
```

### 2. Reviewer Agent Specs

```go
// In agent/spec.go - add reviewer agents

func CodeReviewerSpec() *AgentSpec {
    return &AgentSpec{
        ID:      "code-reviewer",
        Name:    "Code Reviewer Agent",
        Role:    RoleReviewer,
        Purpose: `You review code changes for correctness, style, security, and completeness.
Check for: bugs, logic errors, security vulnerabilities, proper error handling,
documentation, and adherence to best practices. Provide specific, actionable feedback.`,
        AdditionalTools: []string{
            "file_read",      // Review the code
            "memory_search",  // Check context
        },
        Constraints: AgentConstraints{
            MaxIterations:    3,
            Timeout:          2 * time.Minute,
            MaxTokensPerTurn: 2048,
        },
    }
}

func TestReviewerSpec() *AgentSpec {
    return &AgentSpec{
        ID:      "test-reviewer",
        Name:    "Test Reviewer Agent",
        Role:    RoleReviewer,
        Purpose: `You verify that work is complete and correct by running tests,
checking outputs, and validating results. You are pragmatic: if the work
looks good and tests pass, approve it quickly.`,
        AdditionalTools: []string{
            "shell_execute",  // Run tests
            "file_read",
        },
        Constraints: AgentConstraints{
            MaxIterations: 5,
            Timeout:          3 * time.Minute,
        },
    }
}

// Add: DebugReviewer, AnalystReviewer, PlannerReviewer
```

### 3. Review Configuration

```go
// New file: internal/agent/review.go

// ReviewPolicy determines which steps require review
type ReviewPolicy struct {
    // Tool hints that ALWAYS require review
    RequireReview []string

    // Tool hints that NEVER require review (trusted operations)
    SkipReview []string

    // Agent-specific reviewer mappings
    // e.g., coder → code-reviewer, debugger → debug-reviewer
    ReviewerMapping map[string]string

    // Maximum revision cycles before requiring human intervention
    MaxRevisionCycles int
}

// DefaultReviewPolicy returns sensible defaults
func DefaultReviewPolicy() *ReviewPolicy {
    return &ReviewPolicy{
        RequireReview: []string{"code", "refactor", "debug", "git"},
        SkipReview:    []string{"chat", "report", "recall", "search", "analyze"},
        ReviewerMapping: map[string]string{
            "coder":     "code-reviewer",
            "debugger":  "debug-reviewer",
            "planner":   "plan-reviewer",
            "analyst":   "analyst-reviewer",
            "committer": "code-reviewer", // Git commits need code review
        },
        MaxRevisionCycles: 3,
    }
}
```

### 4. Review Manager

```go
// New file: internal/agent/review_manager.go

type ReviewManager struct {
    registry      *AgentRegistry
    stepStore     *task.StepStore
    taskStore     *task.Store
    policy        *ReviewPolicy
    bus           *bus.MessageBus
    logger        *slog.Logger
}

// ReviewStep initiates review of a completed step
func (rm *ReviewManager) ReviewStep(ctx context.Context, step *task.TaskStep) (*ReviewResult, error) {
    // 1. Check if review is needed based on policy
    if !rm.needsReview(step) {
        // Auto-approve
        return &ReviewResult{Status: ReviewApproved}, nil
    }

    // 2. Select reviewer agent
    reviewerID := rm.selectReviewer(step)

    // 3. Build review prompt with step result
    prompt := rm.buildReviewPrompt(step)

    // 4. Run reviewer agent
    reviewerLoop, err := rm.registry.Get(reviewerID)
    result, err := reviewerLoop.RunOnce(ctx, prompt, step.ID)

    // 5. Parse review decision
    reviewResult := rm.parseReviewResult(result)

    // 6. Update step state based on review
    switch reviewResult.Status {
    case ReviewApproved:
        rm.stepStore.SetState(step.ID, task.StepApproved)
    case ReviewRejected:
        rm.stepStore.SetState(step.ID, task.StepRejected)
        rm.createRevisionStep(step, reviewResult.Feedback)
    case ReviewNeedsInfo:
        rm.stepStore.SetState(step.ID, task.StepReviewing) // Stay in review
    }

    return reviewResult, nil
}

type ReviewStatus string
const (
    ReviewApproved   ReviewStatus = "approved"
    ReviewRejected   ReviewStatus = "rejected"
    ReviewNeedsInfo  ReviewStatus = "needs_info"
)

type ReviewResult struct {
    Status    ReviewStatus
    Feedback  string
    Issues    []string
    Confidence float64
}
```

### 5. Tactical Scheduler Integration

```go
// Modify internal/agent/tactical.go

// OnJobCompleted EXTENDED to handle review
func (ts *TacticalScheduler) OnJobCompleted(ctx context.Context, jobID string, result json.RawMessage) error {
    step, err := ts.stepStore.GetByJobID(jobID)
    // ... existing completion logic ...

    // NEW: Check if step requires review
    if ts.reviewManager != nil && ts.reviewManager.NeedsReview(step) {
        step.State = task.StepReviewing
        ts.stepStore.SetState(step.ID, task.StepReviewing)

        // Publish review request
        ts.publishEvent("step.review_requested", map[string]any{
            "step_id": step.ID,
            "task_id": step.TaskID,
        })

        // Don't promote dependent steps yet - wait for review approval
        return nil
    }

    // Original: mark completed and promote dependent steps
    // ... existing logic ...
}

// NEW: Handle review completion
func (ts *TacticalScheduler) OnReviewCompleted(ctx context.Context, stepID string, result *ReviewResult) error {
    step, err := ts.stepStore.GetByID(stepID)

    switch result.Status {
    case ReviewApproved:
        step.State = task.StepCompleted
        ts.stepStore.Update(step)
        // Now promote dependent steps
        ts.stepStore.PromoteReadySteps(step.TaskID)

    case ReviewRejected:
        step.State = step.StepRejected
        ts.stepStore.SetResult(step.ID, result.Feedback)
        // Create revision job
        ts.createRevisionJob(step, result)
    }

    return nil
}
```

### 6. Revision Cycle

```go
// Create a new step that revises the rejected step
func (ts *TacticalScheduler) createRevisionStep(original *task.TaskStep, review *ReviewResult) error {
    revisionPrompt := fmt.Sprintf(
        "REVISE: %s\n\nOriginal work: %s\n\nReview feedback: %s\n\nFix the issues and resubmit.",
        original.Description,
        original.Result,
        review.Feedback,
    )

    revision := task.NewTaskStep(original.TaskID, revisionPrompt, original.Sequence+1000)
    revision.ToolHint = original.ToolHint
    revision.DependsOn = []string{original.ID} // Wait for original (with rejected status)

    ts.stepStore.Create(revision)

    // Schedule the revision
    ts.scheduleStep(context.Background(), revision)

    return nil
}
```

---

## Task State Integration (The "Testing" Phase)

```go
// Leverage the existing "testing" task state

// When all steps are completed/approved, enter testing phase
func (ts *TacticalScheduler) OnAllStepsCompleted(ctx context.Context, taskID string) error {
    t, _ := ts.taskStore.GetByID(taskID)

    // Check if task has executable work (not just analysis)
    if ts.hasExecutableWork(t) {
        t.SetState(task.StateTesting)
        ts.taskStore.Update(t)

        // Create final review step
        finalReview := task.NewTaskStep(taskID,
            "Review all completed work and verify task is complete",
            9999)
        finalReview.ToolHint = "test" // Triggers test-reviewer
        ts.stepStore.Create(finalReview)
        ts.scheduleStep(ctx, finalReview)
    } else {
        // Analysis-only tasks can skip testing
        t.SetState(task.StateCompleted)
    }

    return nil
}
```

---

## TUI Integration

```go
// internal/tui/models/tasks.go - update state icons

func (m *TasksModel) getStateIcon(state string) string {
    switch state {
    case "pending":
        return "○ pend"
    case "planning":
        return "◐ plan"
    case "executing":
        return "● exec"
    case "testing":      // NOW USED
        return "◑ test"  // Half-filled circle = reviewing
    case "completed":
        return "✓ done"
    case "failed":
        return "✗ fail"
    case "cancelled":
        return "⊘ stop"
    // NEW step states shown in step detail
    case "reviewing":
        return "🔍 rev"  // Magnifying glass
    case "approved":
        return "✔ ok"
    case "rejected":
        return "✎ fix"  // Needs revision
    }
}

// Step detail modal shows review feedback
func (m *TasksModel) renderStepDetail(step *types.TaskStep) string {
    if step.State == "rejected" {
        return fmt.Sprintf(
            "REJECTED: %s\n\nRevision needed. Press 'r' to retry with feedback.",
            step.Result, // Contains review feedback
        )
    }
}
```

---

## Configuration

```go
// In ~/.meept/meept.toml

[review]
enabled = true
# Which agent types require review
require_review = ["coder", "debugger", "planner"]
# Which can skip review
skip_review = ["analyst", "chat"]
# How many revision cycles before human intervention
max_revision_cycles = 3
# Auto-approve low-risk changes (e.g., comments, docs)
auto_approve_patterns = ["*.md", "LICENSE", "*.txt"]

[reviewers]
code = "claude-3-5-sonnet"  # Use capable model for reviews
test = "claude-3-haiku"      # Faster for test execution
```

---

## Implementation Phases

### Phase 1: Core Review Infrastructure (Week 1)
1. Add step states (`StepReviewing`, `StepApproved`, `StepRejected`)
2. Create `ReviewManager` with policy-based review decisions
3. Add reviewer agent specs (`code-reviewer`, `test-reviewer`)
4. Modify `TacticalScheduler.OnJobCompleted` to trigger review

### Phase 2: Reviewer Agents (Week 2)
1. Implement reviewer system prompts
2. Add reviewer-specific tools (minimal, focused)
3. Implement review result parsing
4. Add revision cycle logic

### Phase 3: Task-Level Testing Phase (Week 2)
1. Activate existing `StateTesting` task state
2. Create final review step when task completes
3. Add task completion criteria

### Phase 4: TUI & Visibility (Week 3)
1. Add review state icons
2. Show review feedback in step details
3. Add revision count in task view
4. Human override/escalation UI

### Phase 5: Polish (Week 4)
1. Configuration file support
2. Per-project review policies
3. Metrics (review pass rate, revision cycles)
4. Auto-approval patterns

---

## Files to Create/Modify

### New Files
- `internal/agent/review.go` - ReviewPolicy and related types
- `internal/agent/review_manager.go` - ReviewManager orchestrator
- `internal/agent/prompts/reviewer.go` - Reviewer system prompts

### Modified Files
- `internal/task/step.go` - Add StepReviewing, StepApproved, StepRejected states
- `internal/agent/spec.go` - Add reviewer agent specs
- `internal/agent/tactical.go` - Integrate review into job completion
- `internal/agent/orchestrator.go` - Subscribe to review events
- `internal/tui/models/tasks.go` - Add review state icons
- `config/meept.toml` - Add [review] configuration section

---

## Open Questions

1. **Auto-approve threshold**: Should simple/low-risk changes (e.g., comment edits) skip review automatically?

2. **Human escalation**: After N revision cycles, should the system pause and wait for human input?

3. **Review scope**: Should reviewers see only the step output, or full task context?

4. **Parallel reviews**: Should multiple reviewers review the same step (e.g., code + security)?

5. **Review cache**: Should identical changes skip re-review (like `git blame` history)?
