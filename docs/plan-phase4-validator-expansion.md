# Phase 4: Validator Expansion Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Enhance validator coverage and enforce evidence requirements for deterministic task completion.

**Architecture:** Build on existing validator infrastructure (`internal/validator/`) to add strict evidence enforcement, enhance WebValidator and MemoryValidator coverage, and fix validator integration gaps.

**Tech Stack:** Go 1.24, SQLite for state persistence, existing validator interfaces.

---

## Current State Analysis

### What Exists
- `WebValidator` (`internal/validator/web.go`) - validates `EvidenceAPIResponse`
- `MemoryValidator` (`internal/validator/memory.go`) - validates `EvidenceDatabaseRow`
- Both validators registered in `ValidatorManager` (`manager.go:29-34`)
- Evidence enforcement exists in `StepValidator.Validate()` (`interface.go:71-74`)

### What's Missing (Phase 4 Gaps)
1. **No `ValidateEvidence` method in WebValidator** - only `Validate` method exists
2. **No `ValidateEvidence` method in MemoryValidator** - only `Validate` method exists
3. **Unknown evidence types pass through** - `interface.go:92-94` returns `Valid: true` for unknown types
4. **No validation for web_search results** - only web_fetch (APIResponse) covered
5. **Evidence enforcement not wired** - Phase 1 broken pipeline means validators never see evidence

### Files to Modify
- `internal/validator/web.go` - Add `ValidateEvidence` method, enhance for web_search
- `internal/validator/memory.go` - Add `ValidateEvidence` method
- `internal/validator/interface.go` - Fix unknown evidence type pass-through
- `internal/validator/manager.go` - Ensure all memory tool variants registered

---

## Implementation Tasks

### Task 1: Enhance WebValidator

**Files:**
- Modify: `internal/validator/web.go`
- Test: `internal/validator/web_test.go` (create if missing)

- [ ] **Step 1: Add ValidateEvidence method to WebValidator**

Add the missing `ValidateEvidence` method that the `StepValidator` expects:

```go
// ValidateEvidence validates a single piece of web evidence.
func (v *WebValidator) ValidateEvidence(ctx context.Context, ev models.Evidence) ValidationResult {
    var result ValidationResult

    switch ev.Type {
    case models.EvidenceAPIResponse:
        if err := v.validateAPIResponse(ev.Subject, ev.Value); err != nil {
            result.Errors = append(result.Errors, err.Error())
        }
    default:
        result.Valid = false
        result.Errors = append(result.Errors, fmt.Sprintf("unexpected evidence type for web validator: %s", ev.Type))
    }

    result.Valid = len(result.Errors) == 0
    return result
}
```

- [ ] **Step 2: Run tests to verify WebValidator works**

Run: `go test ./internal/validator/... -v -run Web`

Expected: Tests pass or no tests exist (create in next step)

- [ ] **Step 3: Create unit tests for WebValidator**

```go
package validator

import (
    "context"
    "testing"

    "github.com/caimlas/meept/pkg/models"
)

func TestWebValidator_ValidateEvidence_APIResponse_Success(t *testing.T) {
    v := NewWebValidator()
    ev := models.NewEvidence(
        models.EvidenceAPIResponse,
        "https://api.example.com/data",
        "status=200,size=1234",
        "web_fetch",
    )

    result := v.ValidateEvidence(context.Background(), ev)
    if !result.Valid {
        t.Errorf("expected valid, got errors: %v", result.Errors)
    }
}

func TestWebValidator_ValidateEvidence_APIResponse_Failure(t *testing.T) {
    v := NewWebValidator()
    ev := models.NewEvidence(
        models.EvidenceAPIResponse,
        "https://api.example.com/data",
        "status=404,size=0",
        "web_fetch",
    )

    result := v.ValidateEvidence(context.Background(), ev)
    if result.Valid {
        t.Error("expected invalid for 404 status")
    }
}

func TestWebValidator_ValidateEvidence_WrongType(t *testing.T) {
    v := NewWebValidator()
    ev := models.NewEvidence(
        models.EvidenceFileExists,
        "/tmp/test.txt",
        "size=100",
        "file_read",
    )

    result := v.ValidateEvidence(context.Background(), ev)
    if result.Valid {
        t.Error("expected invalid for wrong evidence type")
    }
}
```

- [ ] **Step 4: Run tests and commit**

Run: `go test ./internal/validator/... -v`

```bash
git add internal/validator/web.go internal/validator/web_test.go
git commit -m "feat: add ValidateEvidence to WebValidator with tests"
```

---

### Task 2: Enhance MemoryValidator

**Files:**
- Modify: `internal/validator/memory.go`
- Test: `internal/validator/memory_test.go` (create if missing)

- [ ] **Step 1: Add ValidateEvidence method to MemoryValidator**

```go
// ValidateEvidence validates a single piece of memory evidence.
func (m *MemoryValidator) ValidateEvidence(ctx context.Context, ev models.Evidence) ValidationResult {
    var result ValidationResult

    switch ev.Type {
    case models.EvidenceDatabaseRow:
        if err := m.validateDatabaseRow(ev.Subject, ev.Value); err != nil {
            result.Errors = append(result.Errors, err.Error())
        }
    default:
        result.Valid = false
        result.Errors = append(result.Errors, fmt.Sprintf("unexpected evidence type for memory validator: %s", ev.Type))
    }

    result.Valid = len(result.Errors) == 0
    return result
}
```

- [ ] **Step 2: Run tests to verify MemoryValidator works**

Run: `go test ./internal/validator/... -v -run Memory`

- [ ] **Step 3: Create unit tests for MemoryValidator**

```go
package validator

import (
    "context"
    "testing"

    "github.com/caimlas/meept/pkg/models"
)

func TestMemoryValidator_ValidateEvidence_DBRow_Success(t *testing.T) {
    m := NewMemoryValidator()
    ev := models.NewEvidence(
        models.EvidenceDatabaseRow,
        "session_context",
        `{"rows_affected": 1, "found": true}`,
        "memory_search",
    )

    result := m.ValidateEvidence(context.Background(), ev)
    if !result.Valid {
        t.Errorf("expected valid, got errors: %v", result.Errors)
    }
}

func TestMemoryValidator_ValidateEvidence_DBRow_InvalidJSON(t *testing.T) {
    m := NewMemoryValidator()
    ev := models.NewEvidence(
        models.EvidenceDatabaseRow,
        "session_context",
        "not-json",
        "memory_search",
    )

    result := m.ValidateEvidence(context.Background(), ev)
    // Should handle gracefully - JSON parse fails but fallback accepts string
    // Test documents current behavior
}

func TestMemoryValidator_ValidateEvidence_WrongType(t *testing.T) {
    m := NewMemoryValidator()
    ev := models.NewEvidence(
        models.EvidenceFileExists,
        "/tmp/test.txt",
        "size=100",
        "file_read",
    )

    result := m.ValidateEvidence(context.Background(), ev)
    if result.Valid {
        t.Error("expected invalid for wrong evidence type")
    }
}
```

- [ ] **Step 4: Run tests and commit**

```bash
git add internal/validator/memory.go internal/validator/memory_test.go
git commit -m "feat: add ValidateEvidence to MemoryValidator with tests"
```

---

### Task 3: Fix Unknown Evidence Type Pass-Through

**Files:**
- Modify: `internal/validator/interface.go:80-95`
- Test: Add test for unknown evidence handling

- [ ] **Step 1: Change unknown evidence handling to fail instead of pass**

Current code (line 92-94):
```go
default:
    // Unknown evidence type - log and pass through
    return ValidationResult{Valid: true}
```

Change to:
```go
default:
    // Unknown evidence type - fail validation
    // This prevents unvalidated evidence from passing silently
    return ValidationResult{
        Valid: false,
        Errors: []string{fmt.Sprintf("unknown evidence type: %s", ev.Type)},
    }
```

- [ ] **Step 2: Add logging for unknown evidence types**

Add import `"log/slog"` if not present, then:

```go
// validateEvidence validates a single piece of evidence.
func (v *StepValidator) validateEvidence(ctx context.Context, ev models.Evidence) ValidationResult {
    // Route to appropriate validator based on evidence type
    switch ev.Type {
    case models.EvidenceFileExists, models.EvidenceFileHash:
        return v.fsValidator.ValidateEvidence(ctx, ev)
    case models.EvidenceProcessExit, models.EvidenceShellOutput:
        return v.shellValidator.ValidateEvidence(ctx, ev)
    case models.EvidenceAPIResponse:
        return v.webValidator.ValidateEvidence(ctx, ev)
    case models.EvidenceDatabaseRow:
        return v.memoryValidator.ValidateEvidence(ctx, ev)
    default:
        // Unknown evidence type - log and fail
        slog.Warn("Unknown evidence type during validation", "type", ev.Type, "subject", ev.Subject)
        return ValidationResult{
            Valid: false,
            Errors: []string{fmt.Sprintf("unknown evidence type: %s", ev.Type)},
        }
    }
}
```

- [ ] **Step 3: Add test for unknown evidence type handling**

Add to `internal/validator/interface_test.go` or create it:

```go
func TestStepValidator_ValidateEvidence_UnknownType(t *testing.T) {
    v := NewStepValidator()
    ev := models.Evidence{
        Type: "unknown_type",
        Subject: "test",
        Value: "test",
    }

    result := v.validateEvidence(context.Background(), ev)
    if result.Valid {
        t.Error("expected invalid for unknown evidence type")
    }
    if len(result.Errors) == 0 {
        t.Error("expected error message for unknown type")
    }
}
```

- [ ] **Step 4: Run tests and commit**

```bash
git add internal/validator/interface.go internal/validator/interface_test.go
git commit -m "fix: fail validation for unknown evidence types instead of pass-through"
```

---

### Task 4: Register All Memory Tool Variants

**Files:**
- Modify: `internal/validator/manager.go`

- [ ] **Step 1: Verify all memory tool variants are registered**

Check current registration (manager.go:19-37):
```go
func NewValidatorManager() *ValidatorManager {
    return &ValidatorManager{
        validators: map[string]Validator{
            // ... existing ...
            "memory_search":      NewMemoryValidator(),
            "memory_get_context": NewMemoryValidator(),
            "memory_store":       NewMemoryValidator(),
            "memory_delete":      NewMemoryValidator(),
        },
        // ...
    }
}
```

- [ ] **Step 2: Check for any missing memory tool variants**

Grep for memory tools: `grep -r "memory_" internal/tools/`

If any tools found that aren't registered, add them:

```go
"memory_list":         NewMemoryValidator(),  // if exists
"memory_clear":        NewMemoryValidator(),  // if exists
```

- [ ] **Step 3: Run tests and commit**

```bash
git add internal/validator/manager.go
git commit -m "chore: ensure all memory tool variants are registered"
```

---

### Task 5: Integration Test - Evidence Enforcement

**Files:**
- Create: `internal/validator/integration_test.go`

- [ ] **Step 1: Create integration test for full validator pipeline**

```go
package validator

import (
    "context"
    "testing"

    "github.com/caimlas/meept/internal/task"
    "github.com/caimlas/meept/pkg/models"
)

// TestValidatorManager_EvidenceEnforcement tests that evidence
// requirements are enforced across all validator types.
func TestValidatorManager_EvidenceEnforcement(t *testing.T) {
    mgr := NewValidatorManager()

    tests := []struct {
        name      string
        toolHint  string
        evidence  []models.Evidence
        claims    []string
        wantValid bool
    }{
        {
            name:      "web_fetch with valid evidence",
            toolHint:  "web_fetch",
            evidence:  []models.Evidence{models.NewEvidence(models.EvidenceAPIResponse, "http://example.com", "status=200", "web_fetch")},
            claims:    []string{"fetched data from http://example.com"},
            wantValid: true,
        },
        {
            name:      "web_fetch with no evidence",
            toolHint:  "web_fetch",
            evidence:  nil,
            claims:    []string{"fetched data from http://example.com"},
            wantValid: false,
        },
        {
            name:      "memory_search with valid evidence",
            toolHint:  "memory_search",
            evidence:  []models.Evidence{models.NewEvidence(models.EvidenceDatabaseRow, "context", `{"found": true}`, "memory_search")},
            claims:    []string{"retrieved context from memory"},
            wantValid: true,
        },
        {
            name:      "unknown tool hint passes through",
            toolHint:  "unknown_tool",
            evidence:  nil,
            claims:    []string{"did something"},
            wantValid: true, // No validator registered, passes through
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            step := &task.TaskStep{
                ToolHint: tt.toolHint,
                Evidence: tt.evidence,
                Claims:   tt.claims,
            }

            err := mgr.ValidateStep(context.Background(), step)
            gotValid := err == nil

            if gotValid != tt.wantValid {
                t.Errorf("ValidateStep() valid=%v, want %v, err=%v", gotValid, tt.wantValid, err)
            }
        })
    }
}
```

- [ ] **Step 2: Run integration test**

Run: `go test ./internal/validator/... -v -run Integration`

- [ ] **Step 3: Commit**

```bash
git add internal/validator/integration_test.go
git commit -m "test: add integration test for evidence enforcement pipeline"
```

---

## Verification

After implementation:

1. **Run all validator tests:**
   ```bash
   go test ./internal/validator/... -v
   ```

2. **Verify coverage:**
   - WebValidator has `ValidateEvidence` ✓
   - MemoryValidator has `ValidateEvidence` ✓
   - Unknown evidence types fail instead of pass ✓
   - All memory tool variants registered ✓
   - Integration test passes ✓

3. **Full test suite:**
   ```bash
   go test ./... -v
   ```

---

## Notes

**Phase 4 Dependency on Phase 1:**

Phase 4 validator enhancements will work correctly once Phase 1 (evidence flow pipeline) is fixed. The validators are designed to receive evidence via `TaskStep.Evidence`, but the current pipeline loses evidence in:
- `tools/registry.go:Execute()` → wraps result losing evidence
- `agent/executor.go` → `ExecutionResult` has no Evidence field
- `agent/tactical.go:OnJobCompleted()` → doesn't propagate evidence to step

Until Phase 1 is fixed, Phase 4 validators exist but won't be triggered with real evidence.
