# Leaf 05-03: Inline Skill Feedback + Memory Staleness Caveat

## DISPATCH INSTRUCTION
Implement all tasks below. Do NOT commit. Do NOT run git add. Write code, run tests, report results only. See SHARED-CONVENTIONS.md for coding standards.

**Parent:** 05-combined-improvements/orchestrator.md
**Scope:** (A) Add inline skill feedback trigger (every N user messages, lightweight classification, feeds into existing selfimprove pipeline). (B) Add memory staleness caveat at retrieval time.
**Dependencies:** None
**Estimated Context:** ~55K

## Interface Contract

This leaf exposes:
- `InlineFeedbackTrigger` hook (PrepareNextTurnHook) that fires every N messages
- Integration with existing `selfimprove.LearningPipeline` for validation/application
- `MemoryFreshnessText(ageDays int) string` in memory retrieval formatting

## Tasks

### Part A: Inline Skill Feedback

### Task 1: Create InlineFeedbackTrigger

**File:** `internal/agent/inline_feedback.go` (new)

```go
package agent

import (
    "context"
    "sync"
)

const (
    // DefaultFeedbackInterval is the number of user messages between
    // inline skill feedback checks.
    DefaultFeedbackInterval = 5

    // feedbackClassifyPrompt is the lightweight classification prompt
    // sent to a small/fast model to detect skill improvement opportunities.
    feedbackClassifyPrompt = `Analyze the recent conversation for skill improvement opportunities.

Look for:
1. Requests to add, change, or remove steps in a procedure
2. User preferences or corrections ("actually, I prefer X", "don't do Y")
3. Corrections to approach ("that's wrong, use Z instead")
4. Repeated patterns the user had to explain more than once

Ignore:
- Routine conversation that doesn't generalize
- One-off questions or answers
- Task-specific details that won't recur

If you find an improvement opportunity, respond with:
IMPROVEMENT: [one-line description]
SKILL: [which skill or procedure it affects]
CHANGE: [what should change]

If nothing generalizable, respond with:
NO_IMPROVEMENT`
)

// InlineFeedbackTrigger is a PrepareNextTurnHook that periodically checks
// for skill improvement opportunities during conversation.
type InlineFeedbackTrigger struct {
    mu            sync.Mutex
    messageCount  int
    interval      int
    activeSkill   string // name of currently active skill, if any
    pipeline      FeedbackPipeline // interface to selfimprove
    classifyFn    ClassifyFunc     // LLM classification function
}

// FeedbackPipeline is the interface to the selfimprove learning pipeline.
// The inline trigger proposes; the pipeline validates and applies.
type FeedbackPipeline interface {
    // SubmitProposal queues a skill improvement proposal for validation.
    // The pipeline's Detect→Analyze→Generate→Validate→Apply cycle handles it.
    SubmitProposal(ctx context.Context, proposal SkillProposal) error
}

// SkillProposal is a detected improvement opportunity.
type SkillProposal struct {
    Description string
    SkillName   string
    Change      string
    Source      string // "inline_feedback"
}

// ClassifyFunc sends the classification prompt to a fast model and returns
// the raw response text.
type ClassifyFunc func(ctx context.Context, prompt string, recentMessages []string) (string, error)

func NewInlineFeedbackTrigger(interval int, pipeline FeedbackPipeline, classifyFn ClassifyFunc) *InlineFeedbackTrigger {
    if interval < 1 {
        interval = DefaultFeedbackInterval
    }
    return &InlineFeedbackTrigger{
        interval:   interval,
        pipeline:   pipeline,
        classifyFn: classifyFn,
    }
}

// SetActiveSkill sets the currently active skill name.
// Feedback is only collected when a skill is active.
func (t *InlineFeedbackTrigger) SetActiveSkill(name string) {
    t.mu.Lock()
    defer t.mu.Unlock()
    t.activeSkill = name
}

func (t *InlineFeedbackTrigger) PrepareNextTurn(ctx context.Context, state *TurnState) (*TurnModification, error) {
    t.mu.Lock()
    t.messageCount++
    shouldCheck := t.messageCount >= t.interval && t.activeSkill != ""
    if shouldCheck {
        t.messageCount = 0
    }
    skill := t.activeSkill
    t.mu.Unlock()

    if !shouldCheck {
        return nil, nil
    }

    // Collect recent user messages for classification
    recentMessages := state.RecentUserMessages(t.interval)
    if len(recentMessages) == 0 {
        return nil, nil
    }

    // Classify with fast model (fire-and-forget)
    go func() {
        result, err := t.classifyFn(ctx, feedbackClassifyPrompt, recentMessages)
        if err != nil {
            slog.Debug("inline feedback classification failed", "error", err)
            return
        }

        proposal := parseFeedbackResult(result, skill)
        if proposal == nil {
            return
        }

        // Feed into existing selfimprove pipeline for validation
        if err := t.pipeline.SubmitProposal(ctx, *proposal); err != nil {
            slog.Debug("inline feedback submission failed", "error", err)
        }
    }()

    // Non-blocking: don't modify the turn
    return nil, nil
}

func parseFeedbackResult(result, skillName string) *SkillProposal {
    if strings.Contains(result, "NO_IMPROVEMENT") {
        return nil
    }
    // Parse IMPROVEMENT/SKILL/CHANGE lines
    proposal := &SkillProposal{
        SkillName: skillName,
        Source:    "inline_feedback",
    }
    for _, line := range strings.Split(result, "\n") {
        line = strings.TrimSpace(line)
        switch {
        case strings.HasPrefix(line, "IMPROVEMENT:"):
            proposal.Description = strings.TrimPrefix(line, "IMPROVEMENT:")
        case strings.HasPrefix(line, "SKILL:"):
            if s := strings.TrimSpace(strings.TrimPrefix(line, "SKILL:")); s != "" {
                proposal.SkillName = s
            }
        case strings.HasPrefix(line, "CHANGE:"):
            proposal.Change = strings.TrimPrefix(line, "CHANGE:")
        }
    }
    if proposal.Description == "" || proposal.Change == "" {
        return nil
    }
    return proposal
}
```

### Task 2: Wire into selfimprove pipeline

**File:** `internal/selfimprove/learning.go` (or wherever LearningPipeline is defined)

Read the existing pipeline. Add a `SubmitProposal` method (or adapt an existing entry point) that accepts `SkillProposal` from the inline trigger and feeds it into the Detect phase:

```go
// SubmitProposal accepts an inline feedback proposal and queues it
// for the learning pipeline's validation cycle.
func (p *LearningPipeline) SubmitProposal(ctx context.Context, proposal agent.SkillProposal) error {
    // Convert to the pipeline's internal detection format
    detection := Detection{
        Source:      proposal.Source,
        Description: proposal.Description,
        SkillName:   proposal.SkillName,
        SuggestedChange: proposal.Change,
        Confidence:  0.6, // inline feedback starts at moderate confidence
    }
    return p.enqueueDetection(ctx, detection)
}
```

The exact integration depends on the pipeline's existing entry points. Read the code to find the right place. The key: inline proposals enter the same Detect→Analyze→Generate→Validate→Apply cycle as batch detections, with sandbox validation before applying.

### Task 3: Wire trigger into agent loop

**File:** `internal/agent/handler.go` or the loop constructor

Add the InlineFeedbackTrigger as a PrepareNextTurnHook when:
1. A skill is active in the current session
2. The selfimprove pipeline is available

```go
// In the agent loop constructor or RunOnce setup:
if l.selfimprovePipeline != nil && l.activeSkill != "" {
    trigger := NewInlineFeedbackTrigger(
        DefaultFeedbackInterval,
        l.selfimprovePipeline,
        l.fastModelClassify, // uses the fast/small model for classification
    )
    trigger.SetActiveSkill(l.activeSkill)
    l.hookRegistry.RegisterPrepareNextTurn(trigger)
}
```

### Task 4: Tests for inline feedback

**File:** `internal/agent/inline_feedback_test.go` (new)

- `TestFeedbackInterval` — triggers every N messages, not before
- `TestFeedbackNoSkill` — doesn't trigger when no skill is active
- `TestParseFeedbackResult` — parses IMPROVEMENT/SKILL/CHANGE lines
- `TestParseFeedbackNoImprovement` — returns nil for NO_IMPROVEMENT
- `TestFeedbackNonBlocking` — PrepareNextTurn returns nil modification (fire-and-forget)
- `TestFeedbackPipelineSubmission` — proposal submitted to pipeline

### Part B: Memory Staleness Caveat

### Task 5: Add staleness caveat to memory retrieval

**File:** `internal/memory/episodic.go` or `internal/memory/scoped_manager.go` (wherever memories are formatted for retrieval)

Read the existing memory retrieval code. Find where memories are formatted into text for injection into the agent's context. Add an age-based caveat:

```go
// MemoryFreshnessText returns a staleness caveat for memories older than 1 day.
func MemoryFreshnessText(ageDays int) string {
    if ageDays <= 1 {
        return ""
    }
    return fmt.Sprintf(
        "This memory is %d days old. Memories are point-in-time observations, not live state — "+
            "claims about code behavior or file:line citations may be outdated. "+
            "Verify against current code before asserting as fact.",
        ageDays,
    )
}
```

In the retrieval formatting function, calculate age from `CreatedAt` or `UpdatedAt` and append the caveat:

```go
func formatMemoryForRetrieval(m *Memory) string {
    ageDays := int(time.Since(m.UpdatedAt).Hours() / 24)
    caveat := MemoryFreshnessText(ageDays)

    var b strings.Builder
    b.WriteString(m.Content)
    if caveat != "" {
        b.WriteString("\n\n[" + caveat + "]")
    }
    return b.String()
}
```

### Task 6: Tests for staleness caveat

**File:** `internal/memory/staleness_test.go` (new)

- `TestFreshnessTextRecent` — memory < 1 day old returns empty string
- `TestFreshnessTextOld` — memory 30 days old returns caveat with "30 days"
- `TestFreshnessTextBoundary` — memory exactly 1 day old returns empty
- `TestFreshnessTextTwoDays` — memory 2 days old returns caveat
- `TestFormatMemoryWithCaveat` — formatted output includes caveat for old memories
- `TestFormatMemoryWithoutCaveat` — formatted output has no caveat for fresh memories

## Self-Verification Checklist

- [ ] `go build ./internal/agent/... ./internal/memory/... ./internal/selfimprove/...` compiles
- [ ] `go test ./internal/agent/... -race -run TestFeedback` passes
- [ ] `go test ./internal/memory/... -race -run TestFreshness|TestFormat` passes
- [ ] Inline trigger fires every N messages (default 5)
- [ ] Trigger only fires when a skill is active
- [ ] Classification is fire-and-forget (doesn't block the turn)
- [ ] Proposals feed into existing selfimprove pipeline
- [ ] Staleness caveat appears for memories > 1 day old
- [ ] No mutex held across I/O (trigger uses collect-under-lock)
- [ ] No unused imports or functions

## Review Checklist (for orchestrator)

- [ ] Inline trigger is non-blocking (goroutine for classification)
- [ ] Pipeline integration uses existing validation (sandbox, circuit breaker)
- [ ] Classification prompt is lightweight (fast model appropriate)
- [ ] Staleness caveat is informational, not blocking
- [ ] Caveat text matches Claude Code's pattern ("point-in-time observations, not live state")
- [ ] Age calculated from UpdatedAt (not CreatedAt — updated is more relevant)
- [ ] No debug artifacts, no TODOs, no placeholder values
