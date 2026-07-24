# Leaf 01-03: Auto-Trigger + Fix Loop Integration

## DISPATCH INSTRUCTION
Implement all tasks below. Do NOT commit. Do NOT run git add. Write code, run tests, report results only. See SHARED-CONVENTIONS.md for coding standards.

**Parent:** 01-adversarial-verification/orchestrator.md
**Scope:** Integrate verification auto-trigger into the agent loop's post-turn logic. Track file edits, spawn verifier when threshold reached, handle FAIL→fix→re-verify loop with configurable max iterations.
**Dependencies:** Leaf 01 (VerificationConfig), Leaf 02 (BuildVerifierPrompt, ParseVerdict)
**Estimated Context:** ~85K

## Interface Contract

This leaf exposes:
- `VerificationTracker` struct that counts file-modifying tool calls per turn
- Auto-trigger logic in the agent loop's post-turn hook or PrepareNextTurnHook
- Fix loop: on FAIL, re-dispatch implementer with verifier findings, loop up to MaxFixLoops
- Escalation to user after max iterations (via `ask` tool or notification)

## Tasks

### Task 1: Create VerificationTracker

**File:** `internal/agent/verification_tracker.go` (new)

```go
package agent

import "sync"

// fileModifyingTools are tools that change project state.
var fileModifyingTools = map[string]bool{
    "file_write":  true,
    "file_edit":   true,
    "file_delete": true,
    "git_commit":  true,
    "shell":       true, // conservative: shell may modify files
}

// VerificationTracker counts file-modifying operations and determines
// when adversarial verification should be triggered.
type VerificationTracker struct {
    mu           sync.Mutex
    editCount    int
    filesChanged []string
    threshold    int // from daemon config AutoTriggerThreshold
}

func NewVerificationTracker(threshold int) *VerificationTracker {
    if threshold < 1 {
        threshold = 3
    }
    return &VerificationTracker{threshold: threshold}
}

// RecordToolCall increments the edit counter for file-modifying tools.
func (t *VerificationTracker) RecordToolCall(toolName string, filePath string) {
    t.mu.Lock()
    defer t.mu.Unlock()
    if fileModifyingTools[toolName] {
        t.editCount++
        if filePath != "" {
            t.filesChanged = append(t.filesChanged, filePath)
        }
    }
}

// ShouldTrigger returns true when the edit count reaches the threshold.
func (t *VerificationTracker) ShouldTrigger() bool {
    t.mu.Lock()
    defer t.mu.Unlock()
    return t.editCount >= t.threshold
}

// Snapshot returns the current files changed and resets the counter.
func (t *VerificationTracker) Snapshot() []string {
    t.mu.Lock()
    defer t.mu.Unlock()
    files := make([]string, len(t.filesChanged))
    copy(files, t.filesChanged)
    t.editCount = 0
    t.filesChanged = nil
    return files
}

// Reset clears all state.
func (t *VerificationTracker) Reset() {
    t.mu.Lock()
    defer t.mu.Unlock()
    t.editCount = 0
    t.filesChanged = nil
}
```

### Task 2: Wire tracker into agent loop

**File:** `internal/agent/handler.go` or the main agent loop file

Read the existing agent loop. Find where tool results are processed (after each tool call completes). Add:

```go
// After each tool call result:
if l.verificationTracker != nil {
    filePath := extractFilePath(toolName, toolInput, toolResult)
    l.verificationTracker.RecordToolCall(toolName, filePath)
}
```

Add `verificationTracker *VerificationTracker` field to the agent loop struct. Initialize in the constructor when verification is enabled for the agent.

### Task 3: Implement auto-trigger as PrepareNextTurnHook

**File:** `internal/agent/verification_hook.go` (new)

```go
package agent

// VerificationAutoTrigger is a PrepareNextTurnHook that spawns adversarial
// verification when the file edit threshold is reached.
type VerificationAutoTrigger struct {
    tracker    *VerificationTracker
    config     specs.VerificationConfig
    daemonCfg  VerificationDefaults
    loop       *AgentLoop // back-reference for spawning verifier
}

func (h *VerificationAutoTrigger) PrepareNextTurn(ctx context.Context, state *TurnState) (*TurnModification, error) {
    if !h.config.Enabled || !h.config.AutoTrigger {
        return nil, nil
    }
    if !h.tracker.ShouldTrigger() {
        return nil, nil
    }

    filesChanged := h.tracker.Snapshot()

    // Build verifier prompt
    verifierPrompt := prompts.BuildVerifierPrompt(
        state.AgentRole,
        state.TaskDescription,
        filesChanged,
        state.ApproachSummary,
    )

    // Determine verifier model
    verifierModel := h.config.EffectiveModel(state.AgentModel)
    if verifierModel == "" {
        verifierModel = h.daemonCfg.DefaultModel
    }

    // Spawn verifier as a subagent with restricted tools
    result, err := h.loop.spawnVerifier(ctx, verifierPrompt, verifierModel)
    if err != nil {
        // Log but don't block the main loop
        slog.Warn("verification spawn failed", "error", err)
        return nil, nil
    }

    // Parse verdict
    verdict, checks := ParseVerdict(result)

    switch verdict {
    case VerdictPass, VerdictPartial:
        if verdict == VerdictPartial {
            slog.Info("verification partial", "checks", len(checks))
        }
        return nil, nil // continue normally

    case VerdictFail:
        // Enter fix loop
        return h.handleFail(ctx, state, result, checks)

    case VerdictUnknown:
        slog.Warn("could not parse verification verdict", "output_len", len(result))
        return nil, nil
    }
}
```

### Task 4: Implement fix loop

**File:** `internal/agent/verification_hook.go` (continued)

```go
func (h *VerificationAutoTrigger) handleFail(ctx context.Context, state *TurnState, verifierOutput string, checks []CheckResult) (*TurnModification, error) {
    maxLoops := h.config.MaxFixLoops
    if maxLoops < 1 {
        maxLoops = h.daemonCfg.MaxFixLoops
    }
    if maxLoops < 1 {
        maxLoops = 3
    }

    for i := 0; i < maxLoops; i++ {
        // Inject verifier findings into the implementer's context
        fixInstruction := fmt.Sprintf(
            "Adversarial verification FAILED (iteration %d/%d). Fix the following issues:\n\n%s\n\nAfter fixing, the verifier will re-check your work.",
            i+1, maxLoops, verifierOutput,
        )

        // Re-dispatch implementer with fix instruction
        // (This injects a message into the conversation and lets the loop continue)
        state.InjectSystemMessage(fixInstruction)

        // Wait for implementer to complete fixes
        // (The loop continues naturally; the tracker will re-trigger verification)
        return &TurnModification{
            InjectMessages: []Message{{Role: "user", Content: fixInstruction}},
            SkipTools:      false,
        }, nil
    }

    // Max loops exhausted — escalate to user
    return &TurnModification{
        InjectMessages: []Message{{
            Role: "user",
            Content: fmt.Sprintf(
                "Adversarial verification failed after %d fix attempts. Manual review needed.\n\nLast verifier output:\n%s",
                maxLoops, verifierOutput,
            ),
        }},
    }, nil
}
```

Note: The exact mechanism for "re-dispatch implementer" depends on how the agent loop handles injected messages. Read the existing loop to determine the correct integration point. The key behavior: on FAIL, the implementer gets the verifier's findings and another chance to fix. After max loops, the user is notified.

### Task 5: Wire spawnVerifier into agent loop

**File:** `internal/agent/handler.go` or the loop file

Add a `spawnVerifier` method that:
1. Creates a subagent with the verifier's restricted tool set
2. Uses the verifier model (from config)
3. Runs the verifier prompt
4. Returns the raw output string

Use the existing `subagent_pool.go` infrastructure for spawning. The verifier subagent should have:
- Tool restrictions from `config/agents/verifier.json5`
- The adversarial system prompt from `BuildVerifierPrompt()`
- A reasonable turn limit (e.g., 20 turns)

### Task 6: Add TODO verification nudge

**File:** `internal/agent/verification_hook.go` or the task completion logic

When the agent completes 3+ tasks (tracked via task tool calls) and none of them was a verification step, append a nudge to the tool result:

```go
const verificationNudge = `NOTE: You just closed out 3+ tasks and none of them was a verification step.
Before writing your final summary, verify your work: run the tests, execute the code, check the output.
You cannot self-assign PARTIAL by listing caveats — either verify or report what could not be verified.`
```

This is a lighter-weight nudge than the full auto-trigger — it reminds the agent to self-verify before the independent verifier runs.

### Task 7: Tests

**File:** `internal/agent/verification_tracker_test.go` (new)

- `TestRecordToolCall` — file_write increments, file_read doesn't
- `TestShouldTrigger` — fires at threshold, not before
- `TestSnapshot` — returns files and resets counter
- `TestReset` — clears all state

**File:** `internal/agent/verification_hook_test.go` (new)

- `TestAutoTriggerDisabled` — no trigger when config.Enabled=false
- `TestAutoTriggerBelowThreshold` — no trigger below threshold
- `TestAutoTriggerAtThreshold` — triggers at threshold
- `TestFixLoopMaxIterations` — escalates after max loops
- `TestVerificationNudge` — nudge appears after 3+ tasks without verification

## Self-Verification Checklist

- [ ] `go build ./internal/agent/...` compiles
- [ ] `go test ./internal/agent/... -race -run "TestVerification|TestAutoTrigger|TestFixLoop"` passes
- [ ] Tracker correctly counts file-modifying tools only
- [ ] Auto-trigger respects enabled/auto_trigger config
- [ ] Fix loop respects max_fix_loops from config chain (agent > daemon > default)
- [ ] Escalation message includes verifier output
- [ ] Nudge text appears after 3+ tasks without verification
- [ ] No mutex held across I/O (tracker uses collect-under-lock pattern)
- [ ] No unused imports or functions

## Review Checklist (for orchestrator)

- [ ] VerificationTracker is thread-safe (mutex on all field access)
- [ ] Auto-trigger integrates cleanly with existing hook system (PrepareNextTurnHook)
- [ ] Fix loop does NOT infinite loop (max iterations enforced)
- [ ] Escalation to user includes enough context to understand the failure
- [ ] Verifier subagent uses restricted tool set (cannot write files)
- [ ] Verifier model override works (config chain: agent > daemon > inherit)
- [ ] TODO nudge is non-blocking (appended to tool result, doesn't halt loop)
- [ ] No debug artifacts, no TODOs, no placeholder values
