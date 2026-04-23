# Plan: Implementing Determinism & Verification for Meept

**Generated:** 2026-04-23
**Author:** Claude Code
**Source Audit:** `docs/audit-determinism.md`
**Original Plan:** `docs/plan-determinism-audit.md`

---

## Context

The determinism audit revealed that Meept has excellent orchestration foundations (externalized state, explicit dependencies, review workflows) but critical gaps in **verification**. The system accepts agent self-reporting without ground-truth validation, leading to an estimated 25-30% silent failure rate.

This plan addresses the critical and high-priority gaps to bring Meept to production-grade reliability.

---

## Problem Statement

Meept cannot currently verify that:
1. File writes actually exist on disk after completion
2. Tool side-effects occurred as claimed
3. Step results match the stated requirements
4. Partial output is detected and rejected

The agent loop trusts LLM self-reporting, and review is LLM-to-LLM (reviewer agent validates executor agent—both can hallucinate).

---

## Recommended Approach: Layered Verification

Build a verification layer that:
1. **Requires structured evidence** from agents (claims + proof)
2. **Validates tool side-effects** post-execution
3. **Introduces ground-truth validators** (filesystem, API, database checks)
4. **Adds validation gates** at step and task completion

---

## Critical Files to Modify/Create

### New Files

| File | Purpose |
|------|---------|
| `internal/validator/interface.go` | Validator interface, Evidence types |
| `internal/validator/filesystem.go` | File existence, content, hash validation |
| `internal/validator/task.go` | Task-level completion validation |
| `internal/validator/manager.go` | Orchestrates validators per step |
| `pkg/models/evidence.go` | Evidence types shared across packages |

### Modified Files

| File | Changes |
|------|---------|
| `internal/tools/interface.go` | Extend `ToolResult` to include `Evidence []Evidence` |
| `internal/tools/builtin/filesystem.go` | Return evidence (file stat, hash) after write |
| `internal/tools/builtin/shell.go` | Return evidence (exit code, output hash) |
| `internal/task/step.go` | Add `Evidence []Evidence` field to `TaskStep` |
| `internal/agent/review_manager.go` | Validate evidence presence before review |
| `internal/agent/tactical.go` | Add validation gate in `OnJobCompleted` |
| `internal/agent/loop.go` | Inject evidence requirements into agent prompt |

---

## Implementation Sprints

### Sprint 1: Evidence Infrastructure (Week 1)

**Goal:** Define evidence types and integrate into step model.

#### 1.1 Define Evidence Model (`pkg/models/evidence.go`)

```go
type EvidenceType string

const (
    EvidenceFileExists   EvidenceType = "file_exists"
    EvidenceFileHash     EvidenceType = "file_hash"
    EvidenceAPIResponse  EvidenceType = "api_response"
    EvidenceDatabaseRow  EvidenceType = "db_row"
    EvidenceProcessExit  EvidenceType = "process_exit"
    EvidenceShellOutput  EvidenceType = "shell_output"
)

type Evidence struct {
    Type      EvidenceType `json:"type"`
    Subject   string       `json:"subject"`   // path, URL, query
    Value     string       `json:"value"`     // hash, row JSON, exit code
    Timestamp time.Time    `json:"timestamp"`
    Source    string       `json:"source"`    // tool name that produced it
}
```

#### 1.2 Extend TaskStep (`internal/task/step.go`)

Add field:
```go
type TaskStep struct {
    // ... existing fields ...
    Evidence      []Evidence         `json:"evidence,omitempty"`
    Claims        []string           `json:"claims,omitempty"`  // Agent claims
    Validated     bool               `json:"validated"`
    ValidationError string           `json:"validation_error,omitempty"`
}
```

#### 1.3 Extend ToolResult (`internal/tools/interface.go`)

```go
type ToolResult struct {
    Success  bool        `json:"success"`
    Result   any         `json:"result,omitempty"`
    Error    string      `json:"error,omitempty"`
    Evidence []Evidence  `json:"evidence,omitempty"`  // NEW
}
```

**Files:** `internal/tools/interface.go`, `internal/task/step.go`, `pkg/models/evidence.go`

---

### Sprint 2: Filesystem Validator (Week 1-2)

**Goal:** Validate file operation side-effects.

#### 2.1 Create Validator Interface (`internal/validator/interface.go`)

```go
type ValidationResult struct {
    Valid    bool     `json:"valid"`
    Errors   []string `json:"errors,omitempty"`
    Warnings []string `json:"warnings,omitempty"`
}

type Validator interface {
    Validate(ctx context.Context, step *TaskStep) ValidationResult
}

// StepValidator validates a step's evidence against its claims
type StepValidator struct {
    fsValidator   *FilesystemValidator
    shellValidator *ShellValidator
}

func (v *StepValidator) Validate(ctx context.Context, step *TaskStep) ValidationResult {
    var result ValidationResult

    // Validate each claim against evidence
    for _, claim := range step.Claims {
        claimResult := v.validateClaim(ctx, claim, step.Evidence)
        if !claimResult.Valid {
            result.Errors = append(result.Errors, claimResult.Error)
        }
    }

    return result
}
```

#### 2.2 Filesystem Validator (`internal/validator/filesystem.go`)

```go
type FilesystemValidator struct {
    basePath string
}

func (v *FilesystemValidator) Validate(ctx context.Context, step *TaskStep) ValidationResult {
    var result ValidationResult

    for _, ev := range step.Evidence {
        switch ev.Type {
        case EvidenceFileExists:
            if err := v.validateFileExists(ev.Subject); err != nil {
                result.Errors = append(result.Errors, fmt.Sprintf("file not found: %s", ev.Subject))
            }
        case EvidenceFileHash:
            if err := v.validateFileHash(ev.Subject, ev.Value); err != nil {
                result.Errors = append(result.Errors, fmt.Sprintf("hash mismatch: %s", ev.Subject))
            }
        }
    }

    return result
}

func (v *FilesystemValidator) validateFileExists(path string) error {
    _, err := os.Stat(path)
    return err
}

func (v *FilesystemValidator) validateFileHash(path, expectedHash string) error {
    actualHash, err := computeSHA256(path)
    if err != nil {
        return err
    }
    if actualHash != expectedHash {
        return fmt.Errorf("hash mismatch: expected %s, got %s", expectedHash, actualHash)
    }
    return nil
}
```

#### 2.3 Wire into Tool Execution

Modify `WriteFileTool.Execute()` to return evidence:

```go
func (t *WriteFileTool) Execute(ctx context.Context, args map[string]any) (any, error) {
    // ... existing write logic ...

    // Compute evidence
    info, _ := os.Stat(resolved)
    hash, _ := computeSHA256(resolved)

    result := tools.ToolResult{
        Success: true,
        Result:  fmt.Sprintf("Successfully wrote %s (%d bytes)", resolved, len(content)),
        Evidence: []tools.Evidence{
            {
                Type:    EvidenceFileExists,
                Subject: resolved,
                Value:   fmt.Sprintf("size=%d", info.Size()),
            },
            {
                Type:    EvidenceFileHash,
                Subject: resolved,
                Value:   hash,
            },
        },
    }

    return result, nil
}
```

**Files:** `internal/validator/interface.go`, `internal/validator/filesystem.go`, `internal/tools/builtin/filesystem.go`

---

### Sprint 3: Shell Validator (Week 2)

**Goal:** Validate shell command side-effects.

#### 3.1 Shell Validator (`internal/validator/shell.go`)

```go
type ShellValidator struct{}

func (v *ShellValidator) Validate(ctx context.Context, step *TaskStep) ValidationResult {
    var result ValidationResult

    for _, ev := range step.Evidence {
        switch ev.Type {
        case EvidenceShellOutput:
            // Verify output contains expected patterns
            if !strings.Contains(ev.Value, ev.Subject) {
                result.Errors = append(result.Errors,
                    fmt.Sprintf("output doesn't contain expected pattern: %s", ev.Subject))
            }
        case EvidenceProcessExit:
            if ev.Value != "0" {
                result.Errors = append(result.Errors,
                    fmt.Sprintf("process exited with non-zero code: %s", ev.Value))
            }
        }
    }

    return result
}
```

#### 3.2 Modify ShellExecuteTool

```go
func (t *ShellExecuteTool) Execute(ctx context.Context, args map[string]any) (any, error) {
    // ... existing execution ...

    result := ShellResult{
        Stdout:     stdoutStr,
        Stderr:     stderrStr,
        ReturnCode: returnCode,
    }

    // Attach evidence
    toolResult := tools.ToolResult{
        Success: returnCode == 0,
        Result:  result,
        Evidence: []tools.Evidence{
            {
                Type:    EvidenceProcessExit,
                Subject: command,
                Value:   fmt.Sprintf("%d", returnCode),
            },
            {
                Type:    EvidenceShellOutput,
                Subject: command,
                Value:   hashOutput(stdoutStr),  // Hash for compactness
            },
        },
    }

    return toolResult, nil
}
```

**Files:** `internal/validator/shell.go`, `internal/tools/builtin/shell.go`

---

### Sprint 4: Validation Gates (Week 3)

**Goal:** Block task completion without valid evidence.

#### 4.1 Validator Manager (`internal/validator/manager.go`)

```go
type ValidatorManager struct {
    validators map[string]Validator // tool_hint -> validator
    logger     *slog.Logger
}

func NewValidatorManager() *ValidatorManager {
    return &ValidatorManager{
        validators: map[string]Validator{
            "code":    NewFilesystemValidator(),
            "refactor": NewFilesystemValidator(),
            "shell":   NewShellValidator(),
        },
    }
}

func (m *ValidatorManager) ValidateStep(ctx context.Context, step *TaskStep) error {
    validator, ok := m.validators[step.ToolHint]
    if !ok {
        m.logger.Debug("No validator for tool hint", "hint", step.ToolHint)
        return nil // No validator, pass through
    }

    result := validator.Validate(ctx, step)
    if !result.Valid {
        return fmt.Errorf("validation failed: %s", strings.Join(result.Errors, ", "))
    }

    return nil
}
```

#### 4.2 Integrate into TacticalScheduler

Modify `tactical.go:OnJobCompleted()`:

```go
func (ts *TacticalScheduler) OnJobCompleted(ctx context.Context, jobID string, result json.RawMessage) error {
    // ... existing logic to find step and store result ...

    // NEW: Validation gate BEFORE marking complete
    if ts.validatorManager != nil {
        validationErr := ts.validatorManager.ValidateStep(ctx, step)
        if validationErr != nil {
            ts.logger.Error("Validation failed", "step_id", step.ID, "error", validationErr)
            // Mark as needs_info or trigger revision
            step.Validated = false
            step.ValidationError = validationErr.Error()
            ts.stepStore.Update(step)  // Persist
            return validationErr  // Don't proceed to completion
        }
        step.Validated = true
    }

    // ... existing review/completion logic ...
}
```

**Files:** `internal/validator/manager.go`, `internal/agent/tactical.go`

---

### Sprint 5: Evidence Requirements in Prompt (Week 3-4)

**Goal:** Train agents to provide evidence.

#### 5.1 Update Agent Loop Prompt

Modify `internal/agent/loop.go` to inject evidence requirements:

```go
const evidenceRequirementsPrompt = `
## Evidence Requirements

When completing tasks, you MUST provide:

1. **Claims**: Explicit statements of what was accomplished
   - "Created file X at path Y"
   - "Modified function Z in file W"

2. **Evidence**: Proof that claims are true
   - For files: `stat` output showing existence, SHA256 hash
   - For shell commands: exit code, relevant output excerpts
   - For API calls: response body or status code

Example response format:
```json
{
  "claims": ["Created config.json at ~/.meept/config.json"],
  "evidence": [
    {"type": "file_exists", "path": "/Users/caimlas/.meept/config.json", "size": 1234},
    {"type": "file_hash", "path": "/Users/caimlas/.meept/config.json", "sha256": "abc123..."}
  ]
}
```

Without evidence, task validation will fail.
`
```

**Files:** `internal/agent/loop.go`

---

### Sprint 6: Task-Level Completion Check (Week 4)

**Goal:** Validate entire task before marking complete.

#### 6.1 Add Task Validator (`internal/validator/task.go`)

```go
type TaskValidator struct {
    stepValidator *StepValidator
    taskStore     *task.Store
    stepStore     *task.StepStore
}

func (v *TaskValidator) ValidateTaskCompletion(ctx context.Context, taskID string) error {
    steps, err := v.stepStore.ListByTaskID(taskID)
    if err != nil {
        return err
    }

    var validationErrors []string
    for _, step := range steps {
        if !step.Validated && step.State.IsSuccessfullyTerminal() {
            validationErrors = append(validationErrors,
                fmt.Sprintf("step %s completed but not validated", step.ID))
        }
    }

    if len(validationErrors) > 0 {
        return fmt.Errorf("task validation incomplete: %s", strings.Join(validationErrors, ", "))
    }

    return nil
}
```

#### 6.2 Wire into Orchestrator

Modify `tactical.go:buildStepSummaries()` or completion check:

```go
// In handleJobCompleted, after AreAllCompleted check:
if allDone {
    // NEW: Validate entire task
    if ts.taskValidator != nil {
        if err := ts.taskValidator.ValidateTaskCompletion(ctx, step.TaskID); err != nil {
            ts.logger.Error("Task validation failed", "task_id", step.TaskID, "error", err)
            // Don't mark complete - create remediation step instead
            return err
        }
    }

    t.SetState(task.StateCompleted)
    // ... existing completion logic ...
}
```

**Files:** `internal/validator/task.go`, `internal/agent/tactical.go`

---

## Verification: Testing the Changes

### End-to-End Test Scenarios

1. **File Write Validation**
   ```bash
   # Run task that writes a file
   ./bin/meept chat "Create a file at /tmp/test-determinism.txt with content 'hello world'"

   # Verify task doesn't complete if file doesn't exist
   rm /tmp/test-determinism.txt  # Delete file after creation
   # Task should remain in "executing" or fail validation
   ```

2. **Partial Output Detection**
   ```bash
   # Task that claims completion without full work
   ./bin/meept chat "Create 3 files: a.txt, b.txt, c.txt"

   # If agent only creates 2 files, validation should catch it
   ls -la /path/a.txt /path/b.txt /path/c.txt  # c.txt missing
   # Task should NOT complete - validation gate blocks it
   ```

3. **Hash Verification**
   ```bash
   # Create file through agent, then verify hash
   ./bin/meept chat "Write 'test content' to /tmp/hash-test.txt"

   # Compare reported hash vs actual
   sha256sum /tmp/hash-test.txt
   # Should match the evidence reported in task logs
   ```

### Unit Tests

```bash
# Run validator tests
go test ./internal/validator/... -v

# Run tool integration tests
go test ./internal/tools/builtin/... -v

# Run agent loop tests with validation
go test ./internal/agent/... -run "Validation" -v
```

---

## Rollout Strategy

### Phase 1: Shadow Mode (Week 1-2)
- Deploy validators but don't block completion
- Log validation failures without failing tasks
- Collect metrics on failure rate

### Phase 2: Enforcement Mode (Week 3-4)
- Enable validation gates
- Tasks fail if evidence missing
- Human-in-the-loop for failures

### Phase 3: Optimization (Week 5+)
- Tune evidence requirements based on false positives
- Add more validators (database, API, etc.)
- Integrate with self-improvement loop

---

## Success Metrics

| Metric | Target |
|--------|--------|
| Silent failure rate | <5% (down from 25-30%) |
| Evidence coverage | 90%+ of steps |
| Validation gate blocks | Track count/week |
| Task completion rate | Maintain >80% (don't over-constrain) |

---

## Appendix: Architecture Diagram

```
┌─────────────────────────────────────────────────────────────────┐
│                        Agent Loop                                │
│  (internal/agent/loop.go)                                        │
│                                                                  │
│  ┌──────────────┐      ┌──────────────┐      ┌──────────────┐  │
│  │ LLM Request  │ ──>  │ Tool Execute │ ──>  │ Result +     │  │
│  │ with Prompt  │      │              │      │ Evidence     │  │
│  └──────────────┘      └──────────────┘      └──────────────┘  │
│                                                 │                │
│                                                 v                │
│                                          ┌──────────────┐      │
│                                          │ Store in     │      │
│                                          │ TaskStep     │      │
│                                          └──────────────┘      │
│                                                 │                │
└──────────────────────────────────────────────────┼────────────────┘
                                                   │
                                                   v
┌──────────────────────────────────────────────────────────────────┐
│                     TacticalScheduler                             │
│  (internal/agent/tactical.go)                                     │
│                                                                   │
│  ┌──────────────┐      ┌──────────────┐      ┌──────────────┐  │
│  │ Job          │ ──>  │ Validation   │ ──>  │ Review       │  │
│  │ Completed    │      │ Gate         │      │ or Complete  │  │
│  └──────────────┘      └──────────────┘      └──────────────┘  │
│         │                     │                                 │
│         │                     v                                 │
│         │            ┌──────────────┐                          │
│         │            │ Validator    │                          │
│         │            │ Manager      │                          │
│         │            └──────────────┘                          │
│         │                     │                                 │
└─────────┼─────────────────────┼─────────────────────────────────┘
          │                     │
          │                     v
          │           ┌──────────────────┐
          │           │ Filesystem       │
          │           │ Validator        │
          │           └──────────────────┘
          │                     │
          │                     v
          │           ┌──────────────────┐
          │           │ Shell            │
          │           │ Validator        │
          └──────────>│                  │
                      └──────────────────┘
```

---

*This plan addresses the critical gaps identified in `docs/audit-determinism.md` with a pragmatic, incremental approach focused on evidence collection and validation gates.*
